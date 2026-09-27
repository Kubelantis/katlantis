// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package logstore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runatlantis/atlantis/server/core/logstore"
	"github.com/runatlantis/atlantis/server/logging"
)

const jobID = "5f0e2c4a-9b1d-4e7a-8c3f-1a2b3c4d5e6f"

var pull = logstore.Pull{RepoFullName: "runatlantis/atlantis", Num: 42}

// newFileStore returns a store whose timer never fires within a test, so
// output reaches disk only when the test flushes or a size kick fires.
func newFileStore(t *testing.T, root string) *logstore.FileLogStore {
	t.Helper()
	s, err := logstore.NewFileLogStore(root, time.Hour, logging.NewNoopLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func replayAll(t *testing.T, s logstore.LogStore, id string) []string {
	t.Helper()
	var lines []string
	require.NoError(t, s.Replay(id, func(line string) bool {
		lines = append(lines, line)
		return true
	}))
	return lines
}

func TestFileLogStore_PersistsAndReplaysInOrder(t *testing.T) {
	root := t.TempDir()
	s := newFileStore(t, root)

	s.Write(pull, jobID, "Initializing the backend...")
	s.Write(pull, jobID, "")
	s.Write(pull, jobID, "Plan: 1 to add, 0 to change, 0 to destroy.")
	s.Complete(jobID)
	s.Flush()

	data, err := os.ReadFile(filepath.Join(root, "runatlantis%2Fatlantis", "42", jobID+".log"))
	require.NoError(t, err)
	assert.Equal(t, "Initializing the backend...\n\nPlan: 1 to add, 0 to change, 0 to destroy.\n", string(data))

	exists, err := s.Exists(jobID)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, []string{"Initializing the backend...", "", "Plan: 1 to add, 0 to change, 0 to destroy."}, replayAll(t, s, jobID))
}

// The point of the store: a new instance on the same directory, as after a
// restart, replays what the previous one wrote, including a job cut short by
// the shutdown.
func TestFileLogStore_SurvivesRestart(t *testing.T) {
	root := t.TempDir()
	before := newFileStore(t, root)
	before.Write(pull, jobID, "Apply complete!")
	before.Complete(jobID)
	before.Write(pull, "interrupted-job", "Still running when the server stopped")
	require.NoError(t, before.Close())

	after := newFileStore(t, root)
	assert.Equal(t, []string{"Apply complete!"}, replayAll(t, after, jobID))
	assert.Equal(t, []string{"Still running when the server stopped"}, replayAll(t, after, "interrupted-job"))
}

func TestFileLogStore_DeletePullRemovesOnlyThatPull(t *testing.T) {
	root := t.TempDir()
	s := newFileStore(t, root)
	other := logstore.Pull{RepoFullName: "runatlantis/atlantis", Num: 43}

	s.Write(pull, jobID, "done")
	s.Complete(jobID)
	s.Flush()
	s.Write(pull, "still-open-job", "running") // never completed
	s.Write(other, "other-pull-job", "keep me")
	s.Complete("other-pull-job")
	s.Flush()

	require.NoError(t, s.DeletePull(pull))

	for _, id := range []string{jobID, "still-open-job"} {
		exists, err := s.Exists(id)
		require.NoError(t, err)
		assert.False(t, exists, id)
	}
	assert.Equal(t, []string{"keep me"}, replayAll(t, s, "other-pull-job"))
	assert.NoDirExists(t, filepath.Join(root, "runatlantis%2Fatlantis", "42"))

	require.NoError(t, s.DeletePull(other))
	assert.NoDirExists(t, filepath.Join(root, "runatlantis%2Fatlantis"), "an emptied repo directory is pruned")
	assert.DirExists(t, root)

	require.NoError(t, s.DeletePull(other), "deleting an already-deleted pull request is a no-op")
}

// Repo names with any number of segments (GitLab subgroups) map to a single
// directory, and nothing in a repo name can place files outside root.
func TestFileLogStore_RepoNamesStayOneSegmentUnderRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "job-logs")
	s := newFileStore(t, root)

	for i, name := range []string{"group/subgroup/project", "../../escape", `..\windows`} {
		p := logstore.Pull{RepoFullName: name, Num: i}
		id := "job-" + string(rune('a'+i))
		s.Write(p, id, "line")
		s.Complete(id)
		s.Flush()
		assert.Equal(t, []string{"line"}, replayAll(t, s, id), name)
	}

	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	require.Len(t, entries, 1, "nothing may be written outside root")
	repos, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Len(t, repos, 3)

	for _, name := range []string{"", ".", ".."} {
		assert.Error(t, s.DeletePull(logstore.Pull{RepoFullName: name, Num: 1}), "%q", name)
	}
}

// Job IDs reach Exists and Replay straight from the request URL, so anything
// that could climb out of, or widen, a lookup is refused.
func TestFileLogStore_RejectsUnsafeJobIDs(t *testing.T) {
	root := t.TempDir()
	s := newFileStore(t, root)
	s.Write(pull, jobID, "someone else's output")
	s.Complete(jobID)
	s.Flush()

	for _, id := range []string{"", "..", "../" + jobID, "*", "42/" + jobID, jobID + ".log", "-leading", "job id"} {
		s.Write(pull, id, "x")
		s.Complete(id)
		s.Flush()
		exists, err := s.Exists(id)
		require.NoError(t, err)
		assert.False(t, exists, "%q", id)
		assert.Empty(t, replayAll(t, s, id), "%q", id)
	}
	files, err := os.ReadDir(filepath.Join(root, "runatlantis%2Fatlantis", "42"))
	require.NoError(t, err)
	assert.Len(t, files, 1, "unsafe job IDs must never create files")
}

func TestFileLogStore_ReplayStopsWhenCallbackReturnsFalse(t *testing.T) {
	s := newFileStore(t, t.TempDir())
	for _, l := range []string{"a", "b", "c"} {
		s.Write(pull, jobID, l)
	}
	s.Complete(jobID)
	s.Flush()

	var got []string
	require.NoError(t, s.Replay(jobID, func(line string) bool {
		got = append(got, line)
		return len(got) < 2
	}))
	assert.Equal(t, []string{"a", "b"}, got)
}

func TestFileLogStore_UnknownJobHasNothingToReplay(t *testing.T) {
	s := newFileStore(t, t.TempDir())
	exists, err := s.Exists(jobID)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, replayAll(t, s, jobID))
}

func TestNoopLogStore_PersistsNothing(t *testing.T) {
	var s logstore.LogStore = logstore.NoopLogStore{}
	s.Write(pull, jobID, "line")
	s.Complete(jobID)

	exists, err := s.Exists(jobID)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, replayAll(t, s, jobID))
	require.NoError(t, s.DeletePull(pull))
	require.NoError(t, s.Close())
}

func logPath(root string) string {
	return filepath.Join(root, "runatlantis%2Fatlantis", "42", jobID+".log")
}

func readLog(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(logPath(root))
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

// Write only buffers: the goroutine that fans output out to live viewers must
// never wait on the filesystem, which may be a slow network mount.
func TestFileLogStore_WriteDoesNoIO(t *testing.T) {
	root := t.TempDir()
	s := newFileStore(t, root)

	s.Write(pull, jobID, "buffered")
	assert.NoFileExists(t, logPath(root))
	assert.NoDirExists(t, filepath.Join(root, "runatlantis%2Fatlantis"))

	s.Flush()
	assert.Equal(t, "buffered\n", readLog(t, root))
}

// Buffered output reaches disk within one flush interval without anything
// else prompting it. That interval is the most a crash can lose, and how far
// a viewer on another replica sharing the directory lags a running job.
func TestFileLogStore_FlushesPeriodically(t *testing.T) {
	root := t.TempDir()
	s, err := logstore.NewFileLogStore(root, 20*time.Millisecond, logging.NewNoopLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	s.Write(pull, jobID, "still running")
	require.Eventually(t, func() bool { return readLog(t, root) == "still running\n" }, 5*time.Second, 10*time.Millisecond)

	s.Write(pull, jobID, "more")
	require.Eventually(t, func() bool { return readLog(t, root) == "still running\nmore\n" }, 5*time.Second, 10*time.Millisecond,
		"later output is appended, not rewritten")
}

func TestFileLogStore_LargeOutputFlushesBeforeTheInterval(t *testing.T) {
	root := t.TempDir()
	s := newFileStore(t, root)

	line := strings.Repeat("x", 1023)
	for range 300 { // 300 KiB, past the size trigger
		s.Write(pull, jobID, line)
	}
	require.Eventually(t, func() bool { return readLog(t, root) != "" }, 5*time.Second, 10*time.Millisecond)
}

// Complete prompts the final flush without blocking its caller.
func TestFileLogStore_CompleteFlushesInTheBackground(t *testing.T) {
	root := t.TempDir()
	s := newFileStore(t, root)

	s.Write(pull, jobID, "Apply complete!")
	s.Complete(jobID)
	require.Eventually(t, func() bool { return readLog(t, root) == "Apply complete!\n" }, 5*time.Second, 10*time.Millisecond)
}

// A failed write keeps the output buffered and retries it on the next flush,
// together with anything written meanwhile.
func TestFileLogStore_RetriesFailedWritesWithoutLoss(t *testing.T) {
	root := t.TempDir()
	s := newFileStore(t, root)

	// A file where the repo directory should be makes every write fail.
	blocker := filepath.Join(root, "runatlantis%2Fatlantis")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	s.Write(pull, jobID, "first")
	s.Flush()
	s.Write(pull, jobID, "second")
	s.Flush()
	require.Error(t, s.Close(), "Close reports output it could not write")

	require.NoError(t, os.Remove(blocker))
	s.Complete(jobID) // after Close: flushes inline
	assert.Equal(t, "first\nsecond\n", readLog(t, root))
}

func TestFileLogStore_RecordsDroppedLinesWhileWritesFail(t *testing.T) {
	root := t.TempDir()
	s := newFileStore(t, root)
	blocker := filepath.Join(root, "runatlantis%2Fatlantis")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	line := strings.Repeat("y", 1<<16)
	for range 256 { // fills the 16 MiB cap exactly
		s.Write(pull, jobID, line[:len(line)-1])
	}
	s.Write(pull, jobID, "over the cap")
	s.Write(pull, jobID, "also over")
	s.Flush()

	require.NoError(t, os.Remove(blocker))
	s.Complete(jobID)
	s.Flush()

	lines := strings.Split(strings.TrimSuffix(readLog(t, root), "\n"), "\n")
	var markers []string
	for _, l := range lines {
		if strings.HasPrefix(l, "[atlantis:") {
			markers = append(markers, l)
		}
	}
	assert.Equal(t, []string{"[atlantis: 2 line(s) of output dropped while the job log could not be written]"}, markers)
	assert.Equal(t, markers[0], lines[len(lines)-1])
	assert.Len(t, lines, 257)
}

// Deleting a pull request's logs also drops output still buffered for it, so
// a later flush cannot recreate the deleted directory.
func TestFileLogStore_DeletePullIsNotUndoneByALaterFlush(t *testing.T) {
	root := t.TempDir()
	s := newFileStore(t, root)

	s.Write(pull, jobID, "flushed")
	s.Flush()
	s.Write(pull, jobID, "still buffered")
	require.NoError(t, s.DeletePull(pull))
	s.Flush()

	assert.NoDirExists(t, filepath.Join(root, "runatlantis%2Fatlantis"))
}
