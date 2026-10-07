// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package raw_test

import (
	"testing"

	validation "github.com/go-ozzo/ozzo-validation"
	"github.com/runatlantis/atlantis/server/core/config/raw"
	"github.com/runatlantis/atlantis/server/core/config/valid"
	. "github.com/runatlantis/atlantis/testing"
)

func TestConfig_UnmarshalYAML(t *testing.T) {
	autoDiscoverEnabled := valid.AutoDiscoverEnabledMode
	repoLocksDisabled := valid.RepoLocksDisabledMode
	repoLocksOnApply := valid.RepoLocksOnApplyMode
	cases := []struct {
		description string
		input       string
		exp         raw.RepoCfg
		expErr      string
	}{
		{
			description: "no data",
			input:       "",
			exp: raw.RepoCfg{
				Version:  nil,
				Projects: nil,
			},
		},
		{
			description: "yaml nil",
			input:       "~",
			exp: raw.RepoCfg{
				Version:  nil,
				Projects: nil,
			},
		},
		{
			description: "invalid key",
			input:       "invalid: key",
			exp: raw.RepoCfg{
				Version:  nil,
				Projects: nil,
			},
			expErr: "yaml: construct errors: line 1: field invalid not found in type raw.RepoCfg",
		},
		{
			description: "version set to 2",
			input:       "version: 2",
			exp: raw.RepoCfg{
				Version:  Int(2),
				Projects: nil,
			},
		},
		{
			description: "version set to 3",
			input:       "version: 3",
			exp: raw.RepoCfg{
				Version:  Int(3),
				Projects: nil,
			},
		},
		{
			description: "projects key without value",
			input:       "projects:",
			exp: raw.RepoCfg{
				Version:  nil,
				Projects: nil,
			},
		},
		{
			description: "projects with a map",
			input:       "projects:\n  key: value",
			exp: raw.RepoCfg{
				Version:  nil,
				Projects: nil,
			},
			expErr: "yaml: construct errors: line 2: cannot construct !!map into []raw.Project",
		},
		{
			description: "projects with a scalar",
			input:       "projects: value",
			exp: raw.RepoCfg{
				Version:  nil,
				Projects: nil,
			},
			expErr: "yaml: construct errors: line 1: cannot construct !!str `value` into []raw.Project",
		},
		{
			description: "automerge not a boolean",
			input:       "version: 3\nautomerge: notabool",
			exp: raw.RepoCfg{
				Version:  nil,
				Projects: nil,
			},
			expErr: "yaml: construct errors: line 2: cannot construct !!str `notabool` into bool",
		},
		{
			description: "parallel apply not a boolean",
			input:       "version: 3\nparallel_apply: notabool",
			exp: raw.RepoCfg{
				Version:  nil,
				Projects: nil,
			},
			expErr: "yaml: construct errors: line 2: cannot construct !!str `notabool` into bool",
		},
		{
			description: "should use values if set",
			input: `
version: 3
automerge: true
autodiscover:
  mode: enabled
  ignore_paths:
  - foo/*
parallel_apply: true
parallel_plan: false
repo_locks:
  mode: on_apply
projects:
- dir: mydir
  workspace: myworkspace
  terraform_version: v0.11.0
  autoplan:
    enabled: false
    when_modified: []
  apply_requirements: [mergeable]
  repo_locks:
    mode: disabled
allowed_regexp_prefixes:
- dev/
- staging/`,
			exp: raw.RepoCfg{
				Version: Int(3),
				AutoDiscover: &raw.AutoDiscover{
					Mode:        &autoDiscoverEnabled,
					IgnorePaths: []string{"foo/*"},
				},
				Automerge:     Bool(true),
				ParallelApply: Bool(true),
				ParallelPlan:  Bool(false),
				RepoLocks:     &raw.RepoLocks{Mode: &repoLocksOnApply},
				Projects: []raw.Project{
					{
						Dir:              String("mydir"),
						Workspace:        String("myworkspace"),
						TerraformVersion: String("v0.11.0"),
						Autoplan: &raw.Autoplan{
							WhenModified: []string{},
							Enabled:      Bool(false),
						},
						ApplyRequirements: []string{"mergeable"},
						RepoLocks:         &raw.RepoLocks{Mode: &repoLocksDisabled},
					},
				},
				AllowedRegexpPrefixes: []string{"dev/", "staging/"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			var conf raw.RepoCfg
			err := unmarshalString(c.input, &conf)
			if c.expErr != "" {
				ErrEquals(t, c.expErr, err)
				return
			}
			Ok(t, err)
			Equals(t, c.exp, conf)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	cases := []struct {
		description string
		input       raw.RepoCfg
		expErr      string
	}{
		{
			description: "version not nil",
			input: raw.RepoCfg{
				Version: nil,
			},
			expErr: "version: is required. If you've just upgraded Atlantis you need to rewrite your atlantis.yaml for version 3. See www.runatlantis.io/docs/upgrading-atlantis-yaml.html.",
		},
		{
			description: "version not 2 or 3",
			input: raw.RepoCfg{
				Version: Int(1),
			},
			expErr: "version: only versions 2 and 3 are supported.",
		},
	}
	validation.ErrorTag = "yaml"
	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			err := c.input.Validate()
			if c.expErr == "" {
				Ok(t, err)
			} else {
				ErrEquals(t, c.expErr, err)
			}
		})
	}
}

func TestConfig_ToValid(t *testing.T) {
	autoDiscoverEnabled := valid.AutoDiscoverEnabledMode
	repoLocksOnApply := valid.RepoLocksOnApplyMode
	cases := []struct {
		description string
		input       raw.RepoCfg
		exp         valid.RepoCfg
	}{
		{
			description: "nothing set",
			input:       raw.RepoCfg{Version: Int(2)},
			exp: valid.RepoCfg{
				Version: 2,
			},
		},
		{
			description: "set to empty",
			input: raw.RepoCfg{
				Version:      Int(2),
				AutoDiscover: &raw.AutoDiscover{},
				Projects:     []raw.Project{},
				RepoLocks:    &raw.RepoLocks{},
			},
			exp: valid.RepoCfg{
				Version:      2,
				AutoDiscover: raw.DefaultAutoDiscover(),
				Projects:     nil,
				RepoLocks:    &valid.DefaultRepoLocks,
			},
		},
		{
			description: "automerge, parallel_apply, abort_on_execution_order_fail omitted",
			input: raw.RepoCfg{
				Version: Int(2),
			},
			exp: valid.RepoCfg{
				Version:                   2,
				Automerge:                 nil,
				ParallelApply:             nil,
				AbortOnExecutionOrderFail: false,
			},
		},
		{
			description: "automerge, parallel_apply, abort_on_execution_order_fail true",
			input: raw.RepoCfg{
				Version:                   Int(2),
				Automerge:                 Bool(true),
				ParallelApply:             Bool(true),
				AbortOnExecutionOrderFail: Bool(true),
			},
			exp: valid.RepoCfg{
				Version:                   2,
				Automerge:                 Bool(true),
				ParallelApply:             Bool(true),
				AbortOnExecutionOrderFail: true,
			},
		},
		{
			description: "automerge, parallel_apply, abort_on_execution_order_fail false",
			input: raw.RepoCfg{
				Version:                   Int(2),
				Automerge:                 Bool(false),
				ParallelApply:             Bool(false),
				AbortOnExecutionOrderFail: Bool(false),
			},
			exp: valid.RepoCfg{
				Version:                   2,
				Automerge:                 Bool(false),
				ParallelApply:             Bool(false),
				AbortOnExecutionOrderFail: false,
			},
		},
		{
			description: "autodiscover omitted",
			input: raw.RepoCfg{
				Version: Int(2),
			},
			exp: valid.RepoCfg{
				Version: 2,
			},
		},
		{
			description: "autodiscover included",
			input: raw.RepoCfg{
				Version:      Int(2),
				AutoDiscover: &raw.AutoDiscover{Mode: &autoDiscoverEnabled},
			},
			exp: valid.RepoCfg{
				Version: 2,
				AutoDiscover: &valid.AutoDiscover{
					Mode: valid.AutoDiscoverEnabledMode,
				},
			},
		},
		{
			description: "repo_locks omitted",
			input: raw.RepoCfg{
				Version: Int(2),
			},
			exp: valid.RepoCfg{
				Version: 2,
			},
		},
		{
			description: "repo_locks included",
			input: raw.RepoCfg{
				Version:   Int(2),
				RepoLocks: &raw.RepoLocks{Mode: &repoLocksOnApply},
			},
			exp: valid.RepoCfg{
				Version: 2,
				RepoLocks: &valid.RepoLocks{
					Mode: valid.RepoLocksOnApplyMode,
				},
			},
		},
		{
			description: "everything set",
			input: raw.RepoCfg{
				Version:       Int(2),
				Automerge:     Bool(true),
				ParallelApply: Bool(true),
				AutoDiscover: &raw.AutoDiscover{
					Mode: &autoDiscoverEnabled,
				},
				RepoLocks: &raw.RepoLocks{
					Mode: &repoLocksOnApply,
				},
				Projects: []raw.Project{
					{
						Dir: String("mydir"),
					},
				},
			},
			exp: valid.RepoCfg{
				Version:       2,
				Automerge:     Bool(true),
				ParallelApply: Bool(true),
				AutoDiscover: &valid.AutoDiscover{
					Mode: valid.AutoDiscoverEnabledMode,
				},
				RepoLocks: &valid.RepoLocks{
					Mode: valid.RepoLocksOnApplyMode,
				},
				Projects: []valid.Project{
					{
						Dir:       "mydir",
						Workspace: "default",
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      true,
						},
					},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			Equals(t, c.exp, c.input.ToValid())
		})
	}
}
