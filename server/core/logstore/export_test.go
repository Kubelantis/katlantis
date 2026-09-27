// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package logstore

// Flush runs one flush pass synchronously, so tests need not wait for the
// flush interval.
func (s *FileLogStore) Flush() { s.flushAll() }
