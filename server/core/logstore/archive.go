package logstore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/runatlantis/atlantis/server/core/objstore"
	"github.com/runatlantis/atlantis/server/logging"
)

// ArchiveLogStore keeps job logs on local disk while a job runs and copies
// each completed log to object storage, so that:
//   - any replica can serve a finished job's output without proxying to the
//     replica that ran it, and
//   - job output survives the loss of that replica or its volume.
//
// Live output still streams from the replica running the job.
//
// Object layout under the bucket prefix:
//
//	pulls/<escaped repo>/<pull>/<jobID>.log   the log
//	jobs/<jobID>                              pointer holding the log's key
//
// The pointer exists because lookups only know the job ID.
type ArchiveLogStore struct {
	local  *FileLogStore
	bucket *objstore.Bucket
	logger logging.SimpleLogging

	uploads sync.WaitGroup
	// retries bounds upload attempts; backoff doubles from backoff.
	retries int
	backoff time.Duration
}

var _ LogStore = (*ArchiveLogStore)(nil)

const pointerKeyMeta = "log-key"

// NewArchiveLogStore wraps local so completed logs are archived to bucket.
func NewArchiveLogStore(local *FileLogStore, bucket *objstore.Bucket, logger logging.SimpleLogging) *ArchiveLogStore {
	a := &ArchiveLogStore{local: local, bucket: bucket, logger: logger, retries: 5, backoff: time.Second}
	// onComplete runs with the local store's locks held; only start the upload.
	local.onComplete = func(pull Pull, jobID, path string) {
		a.uploads.Go(func() { a.upload(pull, jobID, path) })
	}
	return a
}

func (a *ArchiveLogStore) pullPrefix(pull Pull) string {
	return a.bucket.Key("pulls", url.PathEscape(pull.RepoFullName), strconv.Itoa(pull.Num)) + "/"
}

func (a *ArchiveLogStore) logKey(pull Pull, jobID string) string {
	return a.pullPrefix(pull) + jobID + ".log"
}

func (a *ArchiveLogStore) pointerKey(jobID string) string {
	return a.bucket.Key("jobs", jobID)
}

func (a *ArchiveLogStore) upload(pull Pull, jobID, path string) {
	var err error
	for attempt := range a.retries {
		if attempt > 0 {
			time.Sleep(a.backoff << (attempt - 1))
		}
		if err = a.uploadOnce(pull, jobID, path); err == nil || errors.Is(err, os.ErrNotExist) {
			return // done, or the pull was deleted meanwhile
		}
		a.logger.Warn("job log archive: uploading job %s (attempt %d): %s", jobID, attempt+1, err)
	}
	a.logger.Err("job log archive: giving up on job %s after %d attempts: %s", jobID, a.retries, err)
}

func (a *ArchiveLogStore) uploadOnce(pull Pull, jobID, path string) error {
	f, err := os.Open(path) // #nosec G304 -- path comes from the local store's own index.
	if err != nil {
		return err
	}
	defer f.Close() // nolint: errcheck
	ctx, cancel := context.WithTimeout(context.Background(), objstore.OpTimeout)
	defer cancel()
	key := a.logKey(pull, jobID)
	if err := a.bucket.Put(ctx, key, f, map[string]string{"repo": pull.RepoFullName, "pull": strconv.Itoa(pull.Num)}); err != nil {
		return err
	}
	// Written second, so a visible pointer always refers to a complete log.
	return a.bucket.Put(ctx, a.pointerKey(jobID), strings.NewReader(""), map[string]string{pointerKeyMeta: key})
}

// Write implements LogStore.
func (a *ArchiveLogStore) Write(pull Pull, jobID, line string) { a.local.Write(pull, jobID, line) }

// Complete implements LogStore. The upload starts once the local log is
// fully written.
func (a *ArchiveLogStore) Complete(jobID string) { a.local.Complete(jobID) }

// Exists implements LogStore.
func (a *ArchiveLogStore) Exists(jobID string) (bool, error) {
	if ok, err := a.local.Exists(jobID); ok || err != nil {
		return ok, err
	}
	if !ValidJobID(jobID) {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), objstore.OpTimeout)
	defer cancel()
	return a.bucket.Exists(ctx, a.pointerKey(jobID))
}

// Replay implements LogStore, preferring the local copy.
func (a *ArchiveLogStore) Replay(jobID string, fn func(line string) bool) error {
	if ok, err := a.local.Exists(jobID); err != nil || ok {
		if err != nil {
			return err
		}
		return a.local.Replay(jobID, fn)
	}
	if !ValidJobID(jobID) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), objstore.OpTimeout)
	defer cancel()
	ptr, err := a.bucket.Get(ctx, a.pointerKey(jobID))
	if objstore.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = ptr.Body.Close()
	key := ""
	for k, v := range ptr.Metadata {
		if strings.EqualFold(k, pointerKeyMeta) {
			key = v
		}
	}
	if key == "" {
		return fmt.Errorf("job log archive: pointer for job %s has no log key", jobID)
	}
	obj, err := a.bucket.Get(ctx, key)
	if objstore.IsNotFound(err) {
		return nil // deleted with its pull request in the meantime
	}
	if err != nil {
		return err
	}
	defer obj.Body.Close() // nolint: errcheck
	return replayLines(obj.Body, fn)
}

func replayLines(r io.Reader, fn func(string) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		if !fn(sc.Text()) {
			return nil
		}
	}
	return sc.Err()
}

// DeletePull implements LogStore. Local and archived logs are both removed;
// all failures are returned.
func (a *ArchiveLogStore) DeletePull(pull Pull) error {
	errs := []error{a.local.DeletePull(pull)}
	ctx := context.Background()
	prefix := a.pullPrefix(pull)
	for key, err := range a.bucket.List(ctx, prefix) {
		if err != nil {
			errs = append(errs, err)
			break
		}
		jobID := strings.TrimSuffix(strings.TrimPrefix(key, prefix), ".log")
		opCtx, cancel := context.WithTimeout(ctx, objstore.OpTimeout)
		// Pointer first, so a job is never visible without its log.
		errs = append(errs, a.bucket.Delete(opCtx, a.pointerKey(jobID)), a.bucket.Delete(opCtx, key))
		cancel()
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("deleting job logs for %s#%d: %w", pull.RepoFullName, pull.Num, err)
	}
	return nil
}

// Close implements LogStore: it flushes local logs, then waits up to 30s for
// in-flight uploads.
func (a *ArchiveLogStore) Close() error {
	err := a.local.Close()
	done := make(chan struct{})
	go func() { a.uploads.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		a.logger.Warn("job log archive: shutting down with uploads still in progress")
	}
	return err
}
