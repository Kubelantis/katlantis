package logstore_test

import (
	"testing"
	"time"

	"github.com/runatlantis/atlantis/server/core/logstore"
	"github.com/runatlantis/atlantis/server/core/objstore"
	"github.com/runatlantis/atlantis/server/core/objstore/objstoretest"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

func replica(t *testing.T, fake *objstoretest.Fake) *logstore.ArchiveLogStore {
	local, err := logstore.NewFileLogStore(t.TempDir(), 10*time.Millisecond, logging.NewNoopLogger(t))
	Ok(t, err)
	a := logstore.NewArchiveLogStore(local, objstore.NewBucket(fake, objstore.Config{Bucket: "b", Prefix: "logs", ServerSideEncryption: "AES256"}), logging.NewNoopLogger(t))
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func lines(t *testing.T, s logstore.LogStore, jobID string) []string {
	var out []string
	Ok(t, s.Replay(jobID, func(l string) bool { out = append(out, l); return true }))
	return out
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met")
}

// A completed job written on one replica is served by another replica that
// has never seen it, straight from the archive.
func TestArchivedJobIsServedByAnotherReplica(t *testing.T) {
	fake := objstoretest.New()
	a, b := replica(t, fake), replica(t, fake)
	pull := logstore.Pull{RepoFullName: "org/repo", Num: 7}

	a.Write(pull, "job-1", "Initializing...")
	a.Write(pull, "job-1", "Plan: 1 to add")
	a.Complete("job-1")
	waitFor(t, func() bool { ok, _ := b.Exists("job-1"); return ok })

	Equals(t, []string{"Initializing...", "Plan: 1 to add"}, lines(t, b, "job-1"))
	Equals(t, []string{"Initializing...", "Plan: 1 to add"}, lines(t, a, "job-1"))
	for _, o := range fake.Objects {
		Equals(t, "AES256", string(o.Encryption))
	}

	ok, err := b.Exists("unknown-job")
	Ok(t, err)
	Assert(t, !ok, "unknown job must not exist")
	Equals(t, 0, len(lines(t, b, "unknown-job")))
}

func TestUnfinishedJobIsNotArchived(t *testing.T) {
	fake := objstoretest.New()
	a := replica(t, fake)
	a.Write(logstore.Pull{RepoFullName: "org/repo", Num: 1}, "job-running", "still going")
	time.Sleep(100 * time.Millisecond)
	Equals(t, 0, len(fake.Keys()))
}

func TestDeletePullRemovesArchivedLogs(t *testing.T) {
	fake := objstoretest.New()
	a, b := replica(t, fake), replica(t, fake)
	pull, other := logstore.Pull{RepoFullName: "org/repo", Num: 7}, logstore.Pull{RepoFullName: "org/repo", Num: 8}
	for _, job := range []string{"job-1", "job-2", "job-3"} {
		a.Write(pull, job, "x")
		a.Complete(job)
	}
	a.Write(other, "job-other", "y")
	a.Complete("job-other")
	waitFor(t, func() bool { return len(fake.Keys()) == 8 })

	Ok(t, b.DeletePull(pull)) // from a replica that never ran them
	Equals(t, []string{"logs/jobs/job-other", "logs/pulls/org%2Frepo/8/job-other.log"}, fake.Keys())
	ok, _ := b.Exists("job-1")
	Assert(t, !ok, "deleted job must be gone")
}

func TestCloseWaitsForUploads(t *testing.T) {
	fake := objstoretest.New()
	a := replica(t, fake)
	a.Write(logstore.Pull{RepoFullName: "org/repo", Num: 1}, "job-1", "done")
	a.Complete("job-1")
	Ok(t, a.Close())
	Equals(t, 2, len(fake.Keys()))
}
