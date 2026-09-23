// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

// Package logstore persists job output (terraform and workflow hook logs)
// beyond the in-memory live view, so a job's log can still be replayed after
// a restart or from a replica that did not run it.
//
// Persistence is independent of whatever the in-memory buffer keeps for live
// tailing: every line is handed to the store as it is produced, so bounding
// the live view never bounds what is persisted.
package logstore

// LogStore persists job output.
//
// Write and Complete are called from the single goroutine that fans job
// output out to live viewers, so they must never block on I/O; an
// implementation that talks to a remote backend buffers and flushes in the
// background.
type LogStore interface {
	// Write records one output line for jobID. Lines for a job arrive in
	// order.
	Write(jobID, line string)
	// Complete marks jobID as finished; no further Write calls follow for it.
	// Everything written for it is guaranteed to be flushed: retried in the
	// background, and flushed synchronously by Close.
	Complete(jobID string)
	// Exists reports whether any persisted output exists for jobID.
	Exists(jobID string) (bool, error)
	// Replay calls fn with every persisted line for jobID, in order, until fn
	// returns false.
	Replay(jobID string, fn func(line string) bool) error
	// Close flushes all pending output and stops background work.
	Close() error
}

// LocalLogStore keeps job output in memory only, which is the behavior
// without an external store: nothing is persisted, so nothing can be replayed
// once the job has left the output handler's memory.
type LocalLogStore struct{}

func (LocalLogStore) Write(_, _ string) {}

func (LocalLogStore) Complete(_ string) {}

func (LocalLogStore) Exists(_ string) (bool, error) { return false, nil }

func (LocalLogStore) Replay(_ string, _ func(string) bool) error { return nil }

func (LocalLogStore) Close() error { return nil }
