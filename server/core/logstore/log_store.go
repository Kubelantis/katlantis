// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

// Package logstore persists job output (terraform and workflow hook logs)
// beyond the in-memory live view, so a job's log can still be replayed after
// a restart.
//
// Persistence is independent of whatever the in-memory buffer keeps for live
// tailing: every line is handed to the store as it is produced, so bounding
// the live view never bounds what is persisted.
package logstore

import "regexp"

// Pull identifies the pull request a job ran for. Stored logs are grouped by
// it so they can be deleted together when the pull request closes.
type Pull struct {
	RepoFullName string
	Num          int
}

// LogStore persists job output.
//
// Write and Complete are called from the single goroutine that fans job
// output out to live viewers, so they must stay cheap.
type LogStore interface {
	// Write records one output line for jobID. Lines for a job arrive in
	// order.
	Write(pull Pull, jobID, line string)
	// Complete marks jobID as finished; no further Write calls follow for it.
	Complete(jobID string)
	// Exists reports whether any persisted output exists for jobID.
	Exists(jobID string) (bool, error)
	// Replay calls fn with every persisted line for jobID, in order, until fn
	// returns false.
	Replay(jobID string, fn func(line string) bool) error
	// DeletePull removes the persisted output of every job of pull.
	DeletePull(pull Pull) error
	// Close finishes any in-progress writes.
	Close() error
}

// jobIDPattern matches the UUIDs Atlantis generates for project jobs and
// workflow hooks. Job IDs reach Exists and Replay straight from the request
// URL, so anything that could escape or widen a storage path is rejected.
var jobIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,127}$`)

// ValidJobID reports whether jobID is safe to use in a storage path. An
// invalid job ID is never persisted, so it never has output to replay.
func ValidJobID(jobID string) bool {
	return jobIDPattern.MatchString(jobID)
}

// NoopLogStore persists nothing: job output lives only in the output
// handler's memory. Used when log streaming itself is disabled.
type NoopLogStore struct{}

func (NoopLogStore) Write(_ Pull, _, _ string) {}

func (NoopLogStore) Complete(_ string) {}

func (NoopLogStore) Exists(_ string) (bool, error) { return false, nil }

func (NoopLogStore) Replay(_ string, _ func(string) bool) error { return nil }

func (NoopLogStore) DeletePull(_ Pull) error { return nil }

func (NoopLogStore) Close() error { return nil }
