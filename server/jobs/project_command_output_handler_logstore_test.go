// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package jobs_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runatlantis/atlantis/server/core/logstore"
	"github.com/runatlantis/atlantis/server/jobs"
	"github.com/runatlantis/atlantis/server/logging"
)

// fakeLogStore records what the handler persists and replays it back, the
// way a real external store would.
type fakeLogStore struct {
	mu        sync.Mutex
	lines     map[string][]string
	pulls     map[string]logstore.Pull
	completed map[string]bool
	// events records Write/Complete calls in order, as "write:<line>" and
	// "complete".
	events map[string][]string
}

func newFakeLogStore() *fakeLogStore {
	return &fakeLogStore{lines: map[string][]string{}, pulls: map[string]logstore.Pull{}, completed: map[string]bool{}, events: map[string][]string{}}
}

func (f *fakeLogStore) Write(pull logstore.Pull, jobID, line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pulls[jobID] = pull
	f.lines[jobID] = append(f.lines[jobID], line)
	f.events[jobID] = append(f.events[jobID], "write:"+line)
}

func (f *fakeLogStore) Complete(jobID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed[jobID] = true
	f.events[jobID] = append(f.events[jobID], "complete")
}

func (f *fakeLogStore) Exists(jobID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.lines[jobID]
	return ok, nil
}

func (f *fakeLogStore) Replay(jobID string, fn func(string) bool) error {
	f.mu.Lock()
	lines := append([]string(nil), f.lines[jobID]...)
	f.mu.Unlock()
	for _, l := range lines {
		if !fn(l) {
			return nil
		}
	}
	return nil
}

func (f *fakeLogStore) DeletePull(pull logstore.Pull) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for jobID, p := range f.pulls {
		if p == pull {
			delete(f.lines, jobID)
			delete(f.pulls, jobID)
		}
	}
	return nil
}

func (f *fakeLogStore) Close() error { return nil }

func (f *fakeLogStore) isCompleted(jobID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.completed[jobID]
}

func (f *fakeLogStore) eventsFor(jobID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events[jobID]...)
}

func newHandlerWithStore(t *testing.T, store *fakeLogStore) jobs.ProjectCommandOutputHandler {
	t.Helper()
	ch := make(chan *jobs.ProjectCmdOutputLine)
	h := jobs.NewAsyncProjectCommandOutputHandler(ch, logging.NewNoopLogger(t), store)
	go h.Handle()
	t.Cleanup(func() { close(ch) })
	return h
}

func drain(t *testing.T, ch chan string) []string {
	t.Helper()
	var got []string
	timeout := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, line)
		case <-timeout:
			t.Fatalf("channel was never closed; received %v", got)
		}
	}
}

func TestOutputHandler_PersistsEveryLineThenCompletes(t *testing.T) {
	store := newFakeLogStore()
	h := newHandlerWithStore(t, store)
	ctx := createTestProjectCmdContext(t)

	h.Send(ctx, "line 1", false)
	h.Send(ctx, "line 2", false)
	h.Send(ctx, "line 3", false)
	h.Send(ctx, "", true)

	require.Eventually(t, func() bool { return store.isCompleted(ctx.JobID) }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"write:line 1", "write:line 2", "write:line 3", "complete"}, store.eventsFor(ctx.JobID))
}

// A job from before a restart is no longer in the handler's memory, but is
// still viewable from the store.
func TestOutputHandler_ReplaysJobFromBeforeRestart(t *testing.T) {
	store := newFakeLogStore()
	store.Write(logstore.Pull{RepoFullName: "owner/repo", Num: 1}, "job-before-restart", "Apply complete!")
	h := newHandlerWithStore(t, store)

	assert.True(t, h.IsKeyExists("job-before-restart"))
	ch := make(chan string, 10)
	go h.Register("job-before-restart", ch)
	assert.Equal(t, []string{"Apply complete!"}, drain(t, ch))
}

// Closing the pull request deletes its persisted logs too, including those
// of jobs from before a restart that are not in the handler's memory.
func TestOutputHandler_CleanUpDeletesPersistedLogs(t *testing.T) {
	store := newFakeLogStore()
	h := newHandlerWithStore(t, store)
	ctx := createTestProjectCmdContext(t)
	pull := logstore.Pull{RepoFullName: ctx.BaseRepo.FullName, Num: ctx.Pull.Num}
	store.Write(pull, "job-before-restart", "old output")
	store.Write(logstore.Pull{RepoFullName: "other/repo", Num: 7}, "other-pull-job", "keep me")

	h.Send(ctx, "Plan: 1 to add", false)
	h.Send(ctx, "", true)
	require.Eventually(t, func() bool { return store.isCompleted(ctx.JobID) }, 5*time.Second, 10*time.Millisecond)

	// As PullClosedExecutor does: once per project, then for the pull.
	h.CleanUp(jobs.PullInfo{
		PullNum:      ctx.Pull.Num,
		Repo:         ctx.BaseRepo.Name,
		RepoFullName: ctx.BaseRepo.FullName,
		ProjectName:  ctx.ProjectName,
		Path:         ctx.RepoRelDir,
		Workspace:    ctx.Workspace,
	})
	h.CleanUp(jobs.PullInfo{PullNum: ctx.Pull.Num, Repo: ctx.BaseRepo.Name, RepoFullName: ctx.BaseRepo.FullName})

	assert.False(t, h.IsKeyExists(ctx.JobID))
	assert.False(t, h.IsKeyExists("job-before-restart"))
	assert.True(t, h.IsKeyExists("other-pull-job"), "other pull requests' logs must be kept")
}

// Registering before a job's first line, with nothing persisted yet, must
// still tail the job live rather than replaying an empty log and closing.
func TestOutputHandler_RegisterBeforeFirstLineStillTailsLive(t *testing.T) {
	store := newFakeLogStore()
	h := newHandlerWithStore(t, store)
	ctx := createTestProjectCmdContext(t)

	ch := make(chan string, 10)
	h.Register(ctx.JobID, ch)

	h.Send(ctx, "live line", false)
	h.Send(ctx, "", true)
	assert.Equal(t, []string{"live line"}, drain(t, ch))
}

func TestOutputHandler_UnknownJobDoesNotExist(t *testing.T) {
	h := newHandlerWithStore(t, newFakeLogStore())
	assert.False(t, h.IsKeyExists("no-such-job"))
}
