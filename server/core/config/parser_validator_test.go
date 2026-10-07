// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/runatlantis/atlantis/server/core/config"
	"github.com/runatlantis/atlantis/server/core/config/raw"
	"github.com/runatlantis/atlantis/server/core/config/valid"
	. "github.com/runatlantis/atlantis/testing"
)

var globalCfgArgs = valid.GlobalCfgArgs{
	AllowAllRepoSettings: true,
}

var globalCfg = valid.NewGlobalCfgFromArgs(globalCfgArgs)

func TestHasRepoCfg_DirDoesNotExist(t *testing.T) {
	r := config.ParserValidator{}
	exists, err := r.HasRepoCfg("/not/exist", "unused.yaml")
	Ok(t, err)
	Equals(t, false, exists)
}

func TestHasRepoCfg_FileDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	r := config.ParserValidator{}
	exists, err := r.HasRepoCfg(tmpDir, "not-exist.yaml")
	Ok(t, err)
	Equals(t, false, exists)
}

func TestHasRepoCfg_InvalidFileExtension(t *testing.T) {
	tmpDir := t.TempDir()
	repoConfigFile := "atlantis.yml"
	_, err := os.Create(filepath.Join(tmpDir, repoConfigFile))
	Ok(t, err)

	r := config.ParserValidator{}
	_, err = r.HasRepoCfg(tmpDir, repoConfigFile)
	ErrContains(t, "found \"atlantis.yml\" as config file; rename using the .yaml extension", err)
}

func TestParseRepoCfg_DirDoesNotExist(t *testing.T) {
	r := config.ParserValidator{}
	_, err := r.ParseRepoCfg("/not/exist", globalCfg, "", "")
	Assert(t, errors.Is(err, fs.ErrNotExist), "exp not exist err")
}

func TestParseRepoCfg_FileDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	r := config.ParserValidator{}
	_, err := r.ParseRepoCfg(tmpDir, globalCfg, "", "")
	Assert(t, errors.Is(err, fs.ErrNotExist), "exp not exist err")
}

func TestParseRepoCfg_BadPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "atlantis.yaml"), nil, 0000)
	Ok(t, err)

	r := config.ParserValidator{}
	_, err = r.ParseRepoCfg(tmpDir, globalCfg, "", "")
	ErrContains(t, "unable to read atlantis.yaml file: ", err)
}

// Test both ParseRepoCfg and ParseGlobalCfg when given in valid YAML.
// We only have a few cases here because we assume the YAML library to be
// well tested. See https://github.com/go-yaml/yaml/blob/v2/decode_test.go#L810.
func TestParseCfgs_InvalidYAML(t *testing.T) {
	cases := []struct {
		description string
		input       []byte
		expErr      string
	}{
		{
			"random characters",
			[]byte("slkjds"),
			"yaml: construct errors: line 1: cannot construct !!str `slkjds` into",
		},
		{
			"just a colon",
			[]byte(":"),
			"go-yaml load error in parser (while parsing a block mapping) at L1.C1: did not find expected key",
		},
		{
			"invalid merge key from fuzzing",
			[]byte("? [foo]\n: bar\n<<: {}\nversion: 3\n"),
			"go-yaml load error in constructor at L1.C3: runtime error: hash of unhashable type []interface {}",
		},
	}

	tmpDir := t.TempDir()

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			confPath := filepath.Join(tmpDir, "atlantis.yaml")
			err := os.WriteFile(confPath, c.input, 0600)
			Ok(t, err)
			r := config.ParserValidator{}
			_, err = r.ParseRepoCfg(tmpDir, globalCfg, "", "")
			ErrContains(t, c.expErr, err)
			globalCfgArgs := valid.GlobalCfgArgs{}
			_, err = r.ParseGlobalCfg(confPath, valid.NewGlobalCfgFromArgs(globalCfgArgs))
			ErrContains(t, c.expErr, err)
		})
	}
}

func TestParseRepoCfg(t *testing.T) {
	tfVersion, _ := version.NewVersion("v0.11.0")
	cases := []struct {
		description string
		input       string
		expErr      string
		exp         valid.RepoCfg
	}{
		// Version key.
		{
			description: "no version",
			input: `
projects:
- dir: "."
`,
			expErr: "version: is required. If you've just upgraded Atlantis you need to rewrite your atlantis.yaml for version 3. See www.runatlantis.io/docs/upgrading-atlantis-yaml.html.",
		},
		{
			description: "unsupported version",
			input: `
version: 0
projects:
- dir: "."
`,
			expErr: "version: only versions 2 and 3 are supported.",
		},
		{
			description: "empty version",
			input: `
version:
projects:
- dir: "."
`,
			expErr: "version: is required. If you've just upgraded Atlantis you need to rewrite your atlantis.yaml for version 3. See www.runatlantis.io/docs/upgrading-atlantis-yaml.html.",
		},
		{
			description: "version 2",
			input: `
version: 2
projects:
- dir: .
`,
			exp: valid.RepoCfg{
				Version: 2,
				Projects: []valid.Project{
					{
						Dir:       ".",
						Workspace: "default",
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      true,
						},
					},
				},
			},
		},

		// Projects key.
		{
			description: "empty projects list",
			input: `
version: 3
projects:`,
			exp: valid.RepoCfg{
				Version:  3,
				Projects: nil,
			},
		},
		{
			description: "project dir not set",
			input: `
version: 3
projects:
- {}`,
			expErr: "projects: (0: (dir: cannot be blank.).).",
		},
		{
			description: "project dir set",
			input: `
version: 3
projects:
- dir: .`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:              ".",
						Workspace:        "default",
						TerraformVersion: nil,
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      true,
						},
						ApplyRequirements: nil,
					},
				},
			},
		},
		{
			description: "timestamp-like strings are preserved",
			input: `
version: 3
projects:
- dir: 2026-06-26
  name: 2026-06-27
  autoplan:
    when_modified:
    - 2026-06-28`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:       "2026-06-26",
						Workspace: "default",
						Name:      String("2026-06-27"),
						Autoplan: valid.Autoplan{
							WhenModified: []string{"2026-06-28"},
							Enabled:      true,
						},
					},
				},
			},
		},
		{
			description: "autoplan should be enabled by default",
			input: `
version: 3
projects:
- dir: "."
`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:       ".",
						Workspace: "default",
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      true,
						},
					},
				},
			},
		},
		{
			description: "autoplan should be enabled if only when_modified set",
			input: `
version: 3
projects:
- dir: "."
  autoplan:
    when_modified: ["**/*.tf*"]
`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:       ".",
						Workspace: "default",
						Autoplan: valid.Autoplan{
							WhenModified: []string{"**/*.tf*"},
							Enabled:      true,
						},
					},
				},
			},
		},
		{
			description: "project fields set except autoplan",
			input: `
version: 3
projects:
- dir: .
  workspace: myworkspace
  terraform_version: v0.11.0
  apply_requirements: [approved]`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:              ".",
						Workspace:        "myworkspace",
						TerraformVersion: tfVersion,
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      true,
						},
						ApplyRequirements: []string{"approved"},
					},
				},
			},
		},
		{
			description: "project field with autoplan",
			input: `
version: 3
projects:
- dir: .
  workspace: myworkspace
  terraform_version: v0.11.0
  apply_requirements: [approved]
  autoplan:
    enabled: false`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:              ".",
						Workspace:        "myworkspace",
						TerraformVersion: tfVersion,
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      false,
						},
						ApplyRequirements: []string{"approved"},
					},
				},
			},
		},
		{
			description: "project field with mergeable apply requirement",
			input: `
version: 3
projects:
- dir: .
  workspace: myworkspace
  terraform_version: v0.11.0
  apply_requirements: [mergeable]
  autoplan:
    enabled: false`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:              ".",
						Workspace:        "myworkspace",
						TerraformVersion: tfVersion,
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      false,
						},
						ApplyRequirements: []string{"mergeable"},
					},
				},
			},
		},
		{
			description: "project field with undiverged apply requirement",
			input: `
version: 3
projects:
- dir: .
  workspace: myworkspace
  terraform_version: v0.11.0
  apply_requirements: [undiverged]
  autoplan:
    enabled: false`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:              ".",
						Workspace:        "myworkspace",
						TerraformVersion: tfVersion,
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      false,
						},
						ApplyRequirements: []string{"undiverged"},
					},
				},
			},
		},
		{
			description: "project field with mergeable and approved apply requirements",
			input: `
version: 3
projects:
- dir: .
  workspace: myworkspace
  terraform_version: v0.11.0
  apply_requirements: [mergeable, approved]
  autoplan:
    enabled: false`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:              ".",
						Workspace:        "myworkspace",
						TerraformVersion: tfVersion,
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      false,
						},
						ApplyRequirements: []string{"mergeable", "approved"},
					},
				},
			},
		},
		{
			description: "project field with undiverged and approved apply requirements",
			input: `
version: 3
projects:
- dir: .
  workspace: myworkspace
  terraform_version: v0.11.0
  apply_requirements: [undiverged, approved]
  autoplan:
    enabled: false`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:              ".",
						Workspace:        "myworkspace",
						TerraformVersion: tfVersion,
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      false,
						},
						ApplyRequirements: []string{"undiverged", "approved"},
					},
				},
			},
		},
		{
			description: "project field with undiverged and mergeable apply requirements",
			input: `
version: 3
projects:
- dir: .
  workspace: myworkspace
  terraform_version: v0.11.0
  apply_requirements: [undiverged, mergeable]
  autoplan:
    enabled: false`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:              ".",
						Workspace:        "myworkspace",
						TerraformVersion: tfVersion,
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      false,
						},
						ApplyRequirements: []string{"undiverged", "mergeable"},
					},
				},
			},
		},
		{
			description: "project field with undiverged, mergeable and approved apply requirements",
			input: `
version: 3
projects:
- dir: .
  workspace: myworkspace
  terraform_version: v0.11.0
  apply_requirements: [undiverged, mergeable, approved]
  autoplan:
    enabled: false`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:              ".",
						Workspace:        "myworkspace",
						TerraformVersion: tfVersion,
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      false,
						},
						ApplyRequirements: []string{"undiverged", "mergeable", "approved"},
					},
				},
			},
		},
		{
			description: "project field with terraform_distribution set to opentofu",
			input: `
version: 3
projects:
- dir: .
  workspace: myworkspace
  terraform_distribution: opentofu
`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Dir:                   ".",
						Workspace:             "myworkspace",
						TerraformDistribution: String("opentofu"),
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      true,
						},
					},
				},
			},
		},
		{
			description: "project dir with ..",
			input: `
version: 3
projects:
- dir: ..`,
			expErr: "projects: (0: (dir: cannot contain '..'.).).",
		},

		// Project must have dir set.
		{
			description: "project with no config",
			input: `
version: 3
projects:
- {}`,
			expErr: "projects: (0: (dir: cannot be blank.).).",
		},
		{
			description: "project with no config at index 1",
			input: `
version: 3
projects:
- dir: "."
- {}`,
			expErr: "projects: (1: (dir: cannot be blank.).).",
		},
		{
			description: "project with unknown key",
			input: `
version: 3
projects:
- unknown: value`,
			expErr: "yaml: construct errors: line 4: field unknown not found in type raw.Project",
		},
		{
			description: "two projects with same dir/workspace without names",
			input: `
version: 3
projects:
- dir: .
  workspace: workspace
- dir: .
  workspace: workspace`,
			expErr: "there are two or more projects with dir: \".\" workspace: \"workspace\" that are not all named; they must have a 'name' key so they can be targeted for apply's separately",
		},
		{
			description: "two projects with same dir/workspace only one with name",
			input: `
version: 3
projects:
- name: myname
  dir: .
  workspace: workspace
- dir: .
  workspace: workspace`,
			expErr: "there are two or more projects with dir: \".\" workspace: \"workspace\" that are not all named; they must have a 'name' key so they can be targeted for apply's separately",
		},
		{
			description: "two projects with same dir/workspace both with same name",
			input: `
version: 3
projects:
- name: myname
  dir: .
  workspace: workspace
- name: myname
  dir: .
  workspace: workspace`,
			expErr: "found two or more projects with name \"myname\"; project names must be unique",
		},
		{
			description: "two projects with same dir/workspace with different names",
			input: `
version: 3
projects:
- name: myname
  dir: .
  workspace: workspace
- name: myname2
  dir: .
  workspace: workspace`,
			exp: valid.RepoCfg{
				Version: 3,
				Projects: []valid.Project{
					{
						Name:      String("myname"),
						Dir:       ".",
						Workspace: "workspace",
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      true,
						},
					},
					{
						Name:      String("myname2"),
						Dir:       ".",
						Workspace: "workspace",
						Autoplan: valid.Autoplan{
							WhenModified: raw.DefaultAutoPlanWhenModified(),
							Enabled:      true,
						},
					},
				},
			},
		},
	}

	tmpDir := t.TempDir()

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			err := os.WriteFile(filepath.Join(tmpDir, "atlantis.yaml"), []byte(c.input), 0600)
			Ok(t, err)

			r := config.ParserValidator{}
			act, err := r.ParseRepoCfg(tmpDir, globalCfg, "", "")
			if c.expErr != "" {
				ErrEquals(t, c.expErr, err)
				return
			}
			Ok(t, err)
			Equals(t, c.exp, act)
		})
	}
}

// Test that we fail if the global validation fails. We test global validation
// more completely in GlobalCfg.ValidateRepoCfg().
func TestParseRepoCfg_GlobalValidation(t *testing.T) {
	tmpDir := t.TempDir()

	repoCfg := `
version: 3
projects:
- dir: .
  apply_requirements: [approved]`
	err := os.WriteFile(filepath.Join(tmpDir, "atlantis.yaml"), []byte(repoCfg), 0600)
	Ok(t, err)

	r := config.ParserValidator{}
	globalCfgArgs := valid.GlobalCfgArgs{}

	_, err = r.ParseRepoCfg(tmpDir, valid.NewGlobalCfgFromArgs(globalCfgArgs), "repo_id", "branch")
	ErrEquals(t, "repo config not allowed to set 'apply_requirements' key: server-side config needs 'allowed_overrides: [apply_requirements]'", err)
}

func TestParseGlobalCfg_NotExist(t *testing.T) {
	r := config.ParserValidator{}
	globalCfgArgs := valid.GlobalCfgArgs{}
	_, err := r.ParseGlobalCfg("/not/exist", valid.NewGlobalCfgFromArgs(globalCfgArgs))
	ErrEquals(t, "unable to read /not/exist file: open /not/exist: no such file or directory", err)
}

func TestParseGlobalCfg(t *testing.T) {
	globalCfgArgs := valid.GlobalCfgArgs{}

	defaultCfg := valid.NewGlobalCfgFromArgs(globalCfgArgs)
	preWorkflowHook := &valid.WorkflowHook{
		StepName:   "run",
		RunCommand: "custom workflow command",
	}
	preWorkflowHooks := []*valid.WorkflowHook{preWorkflowHook}

	postWorkflowHook := &valid.WorkflowHook{
		StepName:   "run",
		RunCommand: "custom workflow command",
	}
	postWorkflowHooks := []*valid.WorkflowHook{postWorkflowHook}

	conftestVersion, _ := version.NewVersion("v1.0.0")

	cases := map[string]struct {
		input  string
		expErr string
		exp    valid.GlobalCfg
	}{
		"empty file": {
			input:  "",
			expErr: "file <tmp> was empty",
		},
		"invalid fields": {
			input:  "invalid: key",
			expErr: "yaml: construct errors: line 1: field invalid not found in type raw.GlobalCfg",
		},
		"no id specified": {
			input: `repos:
- apply_requirements: []`,
			expErr: "repos: (0: (id: cannot be blank.).).",
		},
		"invalid id regex": {
			input: `repos:
- id: /?/`,
			expErr: "repos: (0: (id: parsing: /?/: error parsing regexp: missing argument to repetition operator: `?`.).).",
		},
		"invalid branch regex": {
			input: `repos:
- id: /.*/
  branch: /?/`,
			expErr: "repos: (0: (branch: parsing: /?/: error parsing regexp: missing argument to repetition operator: `?`.).).",
		},
		"invalid repo_config_file which starts with a slash": {
			input: `repos:
- id: /.*/
  repo_config_file: /etc/passwd`,
			expErr: "repos: (0: (repo_config_file: must not starts with a slash '/'.).).",
		},
		"invalid repo_config_file which contains parent directory path": {
			input: `repos:
- id: /.*/
  repo_config_file: ../../etc/passwd`,
			expErr: "repos: (0: (repo_config_file: must not contains parent directory path like '../'.).).",
		},
		"invalid allowed_override": {
			input: `repos:
- id: /.*/
  allowed_overrides: [invalid]`,
			expErr: "repos: (0: (allowed_overrides: \"invalid\" is not a valid override, only \"plan_requirements\", \"apply_requirements\", \"import_requirements\", \"delete_source_branch_on_merge\", \"repo_locking\", \"repo_locks\", \"policy_check\", \"silence_pr_comments\", \"inputs\", and \"tool\" are supported.).).",
		},
		"invalid plan_requirement": {
			input: `repos:
- id: /.*/
  plan_requirements: [invalid]`,
			expErr: "repos: (0: (plan_requirements: \"invalid\" is not a valid plan_requirement, only \"approved\", \"mergeable\" and \"undiverged\" are supported.).).",
		},
		"invalid apply_requirement": {
			input: `repos:
- id: /.*/
  apply_requirements: [invalid]`,
			expErr: "repos: (0: (apply_requirements: \"invalid\" is not a valid apply_requirement, only \"approved\", \"mergeable\" and \"undiverged\" are supported.).).",
		},
		"invalid import_requirement": {
			input: `repos:
- id: /.*/
  import_requirements: [invalid]`,
			expErr: "repos: (0: (import_requirements: \"invalid\" is not a valid import_requirement, only \"approved\", \"mergeable\" and \"undiverged\" are supported.).).",
		},
		"invalid silence_pr_comments": {
			input: `repos:
- id: /.*/
  silence_pr_comments: [invalid]`,
			expErr: "server-side repo config 'silence_pr_comments' key value of 'invalid' is not supported, supported values are [plan, apply]",
		},
		"disable autodiscover": {
			input: `repos:
- id: /.*/
  autodiscover:
    mode: disabled`,
			exp: valid.GlobalCfg{
				Repos: []valid.Repo{
					defaultCfg.Repos[0],
					{
						IDRegex:      regexp.MustCompile(".*"),
						AutoDiscover: &valid.AutoDiscover{Mode: valid.AutoDiscoverDisabledMode},
					},
				},
				TeamAuthz: valid.TeamAuthz{
					Args: make([]string, 0),
				},
			},
		},
		"disable repo locks": {
			input: `repos:
- id: /.*/
  repo_locks:
    mode: disabled`,
			exp: valid.GlobalCfg{
				Repos: []valid.Repo{
					defaultCfg.Repos[0],
					{
						IDRegex:   regexp.MustCompile(".*"),
						RepoLocks: &valid.RepoLocks{Mode: valid.RepoLocksDisabledMode},
					},
				},
				TeamAuthz: valid.TeamAuthz{
					Args: make([]string, 0),
				},
			},
		},
		"empty repos": {
			input: `repos: []`,
			exp:   defaultCfg,
		},
		"all keys specified": {
			input: `
repos:
- id: github.com/owner/repo
  repo_config_file: "path/to/atlantis.yaml"
  apply_requirements: [approved, mergeable]
  pre_workflow_hooks:
    - run: custom workflow command
  post_workflow_hooks:
    - run: custom workflow command
  allowed_overrides: [plan_requirements, apply_requirements, import_requirements, delete_source_branch_on_merge]
  policy_check: true
  autodiscover:
    mode: enabled
  repo_locks:
    mode: on_apply
- id: /.*/
  branch: /(master|main)/
  pre_workflow_hooks:
    - run: custom workflow command
  post_workflow_hooks:
    - run: custom workflow command
  policy_check: false
  autodiscover:
    mode: disabled
  repo_locks:
    mode: disabled
policies:
  conftest_version: v1.0.0
  policy_sets:
    - name: good-policy
      path: rel/path/to/policy
      source: local
`,
			exp: valid.GlobalCfg{
				Repos: []valid.Repo{
					defaultCfg.Repos[0],
					{
						ID:                "github.com/owner/repo",
						RepoConfigFile:    "path/to/atlantis.yaml",
						ApplyRequirements: []string{"approved", "mergeable"},
						PreWorkflowHooks:  preWorkflowHooks,
						PostWorkflowHooks: postWorkflowHooks,
						AllowedOverrides:  []string{"plan_requirements", "apply_requirements", "import_requirements", "delete_source_branch_on_merge"},
						PolicyCheck:       Bool(true),
						AutoDiscover:      &valid.AutoDiscover{Mode: valid.AutoDiscoverEnabledMode},
						RepoLocks:         &valid.RepoLocks{Mode: valid.RepoLocksOnApplyMode},
					},
					{
						IDRegex:           regexp.MustCompile(".*"),
						BranchRegex:       regexp.MustCompile("(master|main)"),
						PreWorkflowHooks:  preWorkflowHooks,
						PostWorkflowHooks: postWorkflowHooks,
						PolicyCheck:       Bool(false),
						AutoDiscover:      &valid.AutoDiscover{Mode: valid.AutoDiscoverDisabledMode},
						RepoLocks:         &valid.RepoLocks{Mode: valid.RepoLocksDisabledMode},
					},
				},
				PolicySets: valid.PolicySets{
					Version:         conftestVersion,
					ApproveCount:    1,
					PolicyItemRegex: "(?s).+",
					PolicySets: []valid.PolicySet{
						{
							Name:            "good-policy",
							Path:            "rel/path/to/policy",
							Source:          valid.LocalPolicySet,
							ApproveCount:    1,
							PolicyItemRegex: "(?s).+",
						},
					},
				},
				TeamAuthz: valid.TeamAuthz{
					Args: make([]string, 0),
				},
			},
		},
		"id regex with trailing slash": {
			input: `
repos:
- id: /github.com//
`,
			exp: valid.GlobalCfg{
				Repos: []valid.Repo{
					defaultCfg.Repos[0],
					{
						IDRegex: regexp.MustCompile("github.com/"),
					},
				},
				TeamAuthz: valid.TeamAuthz{
					Args: make([]string, 0),
				},
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := config.ParserValidator{}
			tmp := t.TempDir()
			path := filepath.Join(tmp, "conf.yaml")
			Ok(t, os.WriteFile(path, []byte(c.input), 0600))

			globalCfgArgs := valid.GlobalCfgArgs{
				PolicyCheckEnabled: false,
			}

			act, err := r.ParseGlobalCfg(path, valid.NewGlobalCfgFromArgs(globalCfgArgs))

			if c.expErr != "" {
				expErr := strings.ReplaceAll(c.expErr, "<tmp>", path)
				ErrEquals(t, expErr, err)
				return
			}
			Ok(t, err)

			if !act.PolicySets.HasPolicies() {
				c.exp.PolicySets = act.PolicySets
			}

			Equals(t, c.exp, act)
			// Have to hand-compare regexes because Equals doesn't do it.
			for i, actRepo := range act.Repos {
				expRepo := c.exp.Repos[i]
				if expRepo.IDRegex != nil {
					Assert(t, expRepo.IDRegex.String() == actRepo.IDRegex.String(),
						"%q != %q for repos[%d]", expRepo.IDRegex.String(), actRepo.IDRegex.String(), i)
				}
				if expRepo.BranchRegex != nil {
					Assert(t, expRepo.BranchRegex.String() == actRepo.BranchRegex.String(),
						"%q != %q for repos[%d]", expRepo.BranchRegex.String(), actRepo.BranchRegex.String(), i)
				}
			}
		})
	}
}

// Test that if we pass in JSON strings everything should parse fine.
func TestParserValidator_ParseGlobalCfgJSON(t *testing.T) {
	conftestVersion, _ := version.NewVersion("v1.0.0")

	cases := map[string]struct {
		json   string
		exp    valid.GlobalCfg
		expErr string
	}{
		"empty string": {
			json:   "",
			expErr: "unexpected end of JSON input",
		},
		"empty object": {
			json: "{}",
			exp:  valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{}),
		},
		"setting all keys": {
			json: `
{
  "repos": [
    {
      "id": "/.*/",
      "apply_requirements": ["mergeable", "approved"],
      "allowed_overrides": ["inputs", "apply_requirements"],
      "autodiscover": {
        "mode": "enabled"
      },
      "repo_locks": {
        "mode": "on_apply"
      }
    },
    {
      "id": "github.com/owner/repo"
    }
  ],
  "policies": {
    "conftest_version": "v1.0.0",
    "policy_sets": [
      {
        "name": "good-policy",
        "source": "local",
        "path": "rel/path/to/policy"
      }
    ]
  }
}
`,
			exp: valid.GlobalCfg{
				Repos: []valid.Repo{
					valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{}).Repos[0],
					{
						IDRegex:           regexp.MustCompile(".*"),
						ApplyRequirements: []string{"mergeable", "approved"},
						AllowedOverrides:  []string{"inputs", "apply_requirements"},
						AutoDiscover:      &valid.AutoDiscover{Mode: valid.AutoDiscoverEnabledMode},
						RepoLocks:         &valid.RepoLocks{Mode: valid.RepoLocksOnApplyMode},
					},
					{
						ID:                "github.com/owner/repo",
						IDRegex:           nil,
						ApplyRequirements: nil,
						AllowedOverrides:  nil,
						AutoDiscover:      nil,
						RepoLocks:         nil,
					},
				},
				PolicySets: valid.PolicySets{
					Version:         conftestVersion,
					ApproveCount:    1,
					PolicyItemRegex: "(?s).+",
					PolicySets: []valid.PolicySet{
						{
							Name:            "good-policy",
							Path:            "rel/path/to/policy",
							Source:          valid.LocalPolicySet,
							ApproveCount:    1,
							PolicyItemRegex: "(?s).+",
						},
					},
				},
				TeamAuthz: valid.TeamAuthz{
					Args: make([]string, 0),
				},
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			pv := &config.ParserValidator{}
			globalCfgArgs := valid.GlobalCfgArgs{}
			cfg, err := pv.ParseGlobalCfgJSON(c.json, valid.NewGlobalCfgFromArgs(globalCfgArgs))
			if c.expErr != "" {
				ErrEquals(t, c.expErr, err)
				return
			}
			Ok(t, err)

			if !cfg.PolicySets.HasPolicies() {
				c.exp.PolicySets = cfg.PolicySets
			}

			Equals(t, c.exp, cfg)
		})
	}
}

// String is a helper routine that allocates a new string value
// to store v and returns a pointer to it.
func String(v string) *string { return &v }

// Bool is a helper routine that allocates a new bool value
// to store v and returns a pointer to it.
func Bool(v bool) *bool { return &v }

// Test that ContainsGlobPattern correctly identifies glob patterns.
func TestContainsGlobPattern(t *testing.T) {
	cases := []struct {
		input    string
		expected bool
	}{
		{".", false},
		{"dir/subdir", false},
		{"dir-name", false},
		{"dir_name", false},
		{"*", true},
		{"**", true},
		{"dir/*", true},
		{"dir/**", true},
		{"**/subdir", true},
		{"dir/*/subdir", true},
		{"dir/**/subdir", true},
		{"?", true},
		{"dir/?", true},
		{"[abc]", true},
		{"dir/[abc]", true},
		{"modules/*/", true},
		{"environments/**/terraform", true},
	}

	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			result := raw.ContainsGlobPattern(c.input)
			Equals(t, c.expected, result)
		})
	}
}

// Test that ValidateGlobPattern correctly validates glob patterns.
func TestValidateGlobPattern(t *testing.T) {
	cases := []struct {
		input  string
		expErr bool
	}{
		{"*", false},
		{"**", false},
		{"dir/*", false},
		{"dir/**", false},
		{"**/subdir", false},
		{"dir/*/subdir", false},
		{"dir/**/subdir", false},
		{"?", false},
		{"[abc]", false},
		{"[a-z]", false},
		{"modules/*/", false},
		{"environments/**/terraform", false},
		// Invalid patterns
		{"[", true},
		{"[abc", true},
	}

	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			err := raw.ValidateGlobPattern(c.input)
			if c.expErr {
				Assert(t, err != nil, "expected error for pattern %q", c.input)
			} else {
				Ok(t, err)
			}
		})
	}
}

// Test glob pattern expansion in ParseRepoCfg.
func TestParseRepoCfg_GlobExpansion(t *testing.T) {
	// Create a temp directory with the following structure:
	// repo/
	//   atlantis.yaml
	//   modules/
	//     module-a/
	//       main.tf
	//     module-b/
	//       main.tf
	//     module-c/          (no .tf files - should be excluded)
	//       readme.md
	//   environments/
	//     dev/
	//       main.tf
	//     prod/
	//       main.tf

	tmpDir := t.TempDir()

	// Create directory structure
	dirs := []string{
		"modules/module-a",
		"modules/module-b",
		"modules/module-c",
		"environments/dev",
		"environments/prod",
	}
	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755)
		Ok(t, err)
	}

	// Create .tf files in terraform directories
	tfDirs := []string{
		"modules/module-a",
		"modules/module-b",
		"environments/dev",
		"environments/prod",
	}
	for _, dir := range tfDirs {
		err := os.WriteFile(filepath.Join(tmpDir, dir, "main.tf"), []byte("# terraform"), 0600)
		Ok(t, err)
	}

	// Create non-tf file in module-c
	err := os.WriteFile(filepath.Join(tmpDir, "modules/module-c/readme.md"), []byte("# readme"), 0600)
	Ok(t, err)

	cases := []struct {
		description string
		input       string
		expDirs     []string // Expected project directories after expansion
		expErr      string
	}{
		{
			description: "single glob pattern",
			input: `
version: 3
projects:
- dir: "modules/*"
`,
			expDirs: []string{"modules/module-a", "modules/module-b"},
		},
		{
			description: "double star glob pattern",
			input: `
version: 3
projects:
- dir: "environments/**"
`,
			expDirs: []string{"environments/dev", "environments/prod"},
		},
		{
			description: "mixed glob and non-glob projects",
			input: `
version: 3
projects:
- dir: "."
- dir: "modules/*"
`,
			expDirs: []string{".", "modules/module-a", "modules/module-b"},
		},
		{
			description: "glob with settings preserved",
			input: `
version: 3
projects:
- dir: "modules/*"
  workspace: staging
  apply_requirements: [approved]
`,
			expDirs: []string{"modules/module-a", "modules/module-b"},
		},
		{
			description: "no glob - backward compatibility",
			input: `
version: 3
projects:
- dir: "modules/module-a"
`,
			expDirs: []string{"modules/module-a"},
		},
		{
			description: "invalid glob pattern",
			input: `
version: 3
projects:
- dir: "[invalid"
`,
			expErr: "syntax error in pattern",
		},
	}

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			err := os.WriteFile(filepath.Join(tmpDir, "atlantis.yaml"), []byte(c.input), 0600)
			Ok(t, err)

			r := config.ParserValidator{}
			cfg, err := r.ParseRepoCfg(tmpDir, globalCfg, "", "")
			if c.expErr != "" {
				Assert(t, err != nil, "expected error")
				Assert(t, strings.Contains(err.Error(), c.expErr), "error %q should contain %q", err.Error(), c.expErr)
				return
			}
			Ok(t, err)

			// Extract directories from the parsed config
			var actualDirs []string
			for _, p := range cfg.Projects {
				actualDirs = append(actualDirs, p.Dir)
			}

			// Sort both slices for comparison
			Equals(t, len(c.expDirs), len(actualDirs))
			for _, expDir := range c.expDirs {
				found := slices.Contains(actualDirs, expDir)
				Assert(t, found, "expected dir %q not found in actual dirs %v", expDir, actualDirs)
			}
		})
	}
}

// Test that glob expansion preserves project settings.
func TestParseRepoCfg_GlobExpansionPreservesSettings(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory structure
	dirs := []string{"modules/mod-a", "modules/mod-b"}
	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755)
		Ok(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, dir, "main.tf"), []byte("# tf"), 0600)
		Ok(t, err)
	}

	input := `
version: 3
projects:
- dir: "modules/*"
  workspace: staging
  terraform_version: v1.0.0
  apply_requirements: [approved, mergeable]
  autoplan:
    enabled: false
    when_modified: ["*.tf"]
`
	err := os.WriteFile(filepath.Join(tmpDir, "atlantis.yaml"), []byte(input), 0600)
	Ok(t, err)

	r := config.ParserValidator{}
	cfg, err := r.ParseRepoCfg(tmpDir, globalCfg, "", "")
	Ok(t, err)

	// Verify we got 2 projects
	Equals(t, 2, len(cfg.Projects))

	// Verify each project has the correct settings
	for _, p := range cfg.Projects {
		Equals(t, "staging", p.Workspace)
		Assert(t, p.TerraformVersion != nil, "TerraformVersion should not be nil")
		Equals(t, "1.0.0", p.TerraformVersion.String())
		Equals(t, []string{"approved", "mergeable"}, p.ApplyRequirements)
		Equals(t, false, p.Autoplan.Enabled)
		Equals(t, []string{"*.tf"}, p.Autoplan.WhenModified)
	}
}

// Test that glob expansion does not copy project names.
func TestParseRepoCfg_GlobExpansionNoNameCopy(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory structure
	dirs := []string{"modules/mod-a", "modules/mod-b"}
	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755)
		Ok(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, dir, "main.tf"), []byte("# tf"), 0600)
		Ok(t, err)
	}

	input := `
version: 3
projects:
- name: my-project
  dir: "modules/*"
`
	err := os.WriteFile(filepath.Join(tmpDir, "atlantis.yaml"), []byte(input), 0600)
	Ok(t, err)

	r := config.ParserValidator{}
	cfg, err := r.ParseRepoCfg(tmpDir, globalCfg, "", "")
	Ok(t, err)

	// Verify we got 2 projects and none have names (since name is not copied for expanded projects)
	Equals(t, 2, len(cfg.Projects))
	for _, p := range cfg.Projects {
		Assert(t, p.Name == nil, "expanded projects should not have names")
	}
}
