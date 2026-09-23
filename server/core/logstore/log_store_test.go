// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package logstore_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runatlantis/atlantis/server/core/logstore"
)

func TestLocalLogStore_PersistsNothing(t *testing.T) {
	var s logstore.LogStore = logstore.LocalLogStore{}
	s.Write("job", "line")
	s.Complete("job")

	exists, err := s.Exists("job")
	require.NoError(t, err)
	assert.False(t, exists)
	require.NoError(t, s.Replay("job", func(string) bool {
		t.Fatal("LocalLogStore must not replay anything")
		return false
	}))
	require.NoError(t, s.Close())
}
