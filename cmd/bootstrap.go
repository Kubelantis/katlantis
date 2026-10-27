// Copyright 2017 HootSuite Media Inc.
// SPDX-License-Identifier: Apache-2.0
// Modified hereafter by contributors to runatlantis/atlantis.

package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// TestdriveCmd used to start a guided tour that ran Atlantis on this machine.
// This server needs a Kubernetes API, so the command now only says how to
// try Atlantis instead.
type TestdriveCmd struct{}

// errTestdrive explains why testdrive no longer runs.
var errTestdrive = errors.New("testdrive is not supported: this Atlantis keeps locks and pull status in Kubernetes and cannot run without a Kubernetes API; install it on a cluster with the Helm chart in deploy/helm/atlantis (see the repository README)")

// Init returns the runnable cobra command.
func (b *TestdriveCmd) Init() *cobra.Command {
	return &cobra.Command{
		Use:    "testdrive",
		Short:  "Not supported: Atlantis needs Kubernetes",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(os.Stderr, "\033[31mError: %s\033[39m\n\n", errTestdrive)
			return errTestdrive
		},
		SilenceErrors: true,
	}
}
