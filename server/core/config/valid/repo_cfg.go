// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

// Package valid contains the structs representing the atlantis.yaml config
// after it's been parsed and validated.
package valid

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	version "github.com/hashicorp/go-version"
)

// RepoCfg is the atlantis.yaml config after it's been parsed and validated.
type RepoCfg struct {
	// Version is the version of the atlantis YAML file.
	Version                   int
	Projects                  []Project
	PolicySets                PolicySets
	Automerge                 *bool
	AutoDiscover              *AutoDiscover
	ParallelApply             *bool
	ParallelPlan              *bool
	ParallelPolicyCheck       *bool
	DeleteSourceBranchOnMerge *bool
	RepoLocks                 *RepoLocks
	EmojiReaction             string
	AllowedRegexpPrefixes     []string
	AbortOnExecutionOrderFail bool
	SilencePRComments         []string
}

func (r RepoCfg) FindProjectsByDirWorkspace(repoRelDir string, workspace string) []Project {
	var ps []Project
	for _, p := range r.Projects {
		if p.Dir == repoRelDir && p.Workspace == workspace {
			ps = append(ps, p)
		}
	}
	return ps
}

// FindProjectsByDir returns all projects that are in dir.
func (r RepoCfg) FindProjectsByDir(dir string) []Project {
	var ps []Project
	for _, p := range r.Projects {
		if p.Dir == dir {
			ps = append(ps, p)
		}
	}
	return ps
}

// FindProjectsByDirPattern returns all projects whose dir matches the glob pattern.
// Supports patterns like "modules/*", "environments/**", etc.
func (r RepoCfg) FindProjectsByDirPattern(pattern string) []Project {
	var ps []Project
	for _, p := range r.Projects {
		if matched, _ := doublestar.Match(pattern, p.Dir); matched {
			ps = append(ps, p)
		}
	}
	return ps
}

// FindProjectsByDirPatternWorkspace returns all projects whose dir matches the
// glob pattern and workspace matches exactly.
func (r RepoCfg) FindProjectsByDirPatternWorkspace(pattern string, workspace string) []Project {
	var ps []Project
	for _, p := range r.Projects {
		if matched, _ := doublestar.Match(pattern, p.Dir); matched && p.Workspace == workspace {
			ps = append(ps, p)
		}
	}
	return ps
}

// ContainsDirGlobPattern returns true if the string contains glob pattern characters.
func ContainsDirGlobPattern(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func (r RepoCfg) FindProjectByName(name string) *Project {
	for _, p := range r.Projects {
		if p.Name != nil && *p.Name == name {
			return &p
		}
	}
	return nil
}

func (r RepoCfg) FindProjectsByExactName(name string) []Project {
	var ps []Project
	for _, p := range r.Projects {
		if p.Name != nil && *p.Name == name {
			ps = append(ps, p)
		}
	}
	return ps
}

// FindProjectsByName returns all projects that match with name.
func (r RepoCfg) FindProjectsByName(name string) []Project {
	var ps []Project
	sanitizedName := "^" + name + "$"
	for _, p := range r.Projects {
		if p.Name != nil {
			if match, _ := regexp.MatchString(sanitizedName, *p.Name); match {
				ps = append(ps, p)
			}
		}
	}
	// If we found more than one project then we need to make sure that the regex is allowed.
	if len(ps) > 1 && !isRegexAllowed(name, r.AllowedRegexpPrefixes) {
		log.Printf("Found more than one project for regex %q. This regex is not on the allow list.", name)
		return nil
	}
	return ps
}

func isRegexAllowed(name string, allowedRegexpPrefixes []string) bool {
	if len(allowedRegexpPrefixes) == 0 {
		return true
	}
	for _, allowedRegexPrefix := range allowedRegexpPrefixes {
		if strings.HasPrefix(name, allowedRegexPrefix) {
			return true
		}
	}
	return false
}

// AutoDiscoverEnabled returns a final true/false decision for whether
// AutoDiscover is enabled for a repo. The caller must resolve precedence
// before passing autoDiscoverMode. This method does not read r.AutoDiscover.
func (r RepoCfg) AutoDiscoverEnabled(autoDiscoverMode AutoDiscoverMode) bool {
	if autoDiscoverMode == AutoDiscoverAutoMode {
		// AutoDiscover is enabled by default when no projects are defined
		return len(r.Projects) == 0
	}

	return autoDiscoverMode == AutoDiscoverEnabledMode
}

// validateWorkspaceAllowed returns an error if repoCfg defines projects in
// repoRelDir but none of them use workspace. We want this to be an error
// because if users have gone to the trouble of defining projects in repoRelDir
// then it's likely that if we're running a command for a workspace that isn't
// defined then they probably just typed the workspace name wrong.
func (r RepoCfg) ValidateWorkspaceAllowed(repoRelDir string, workspace string) error {
	projects := r.FindProjectsByDir(repoRelDir)

	// If that directory doesn't have any projects configured then we don't
	// enforce workspace names.
	if len(projects) == 0 {
		return nil
	}

	var configuredSpaces []string
	for _, p := range projects {
		if p.Workspace == workspace {
			return nil
		}
		configuredSpaces = append(configuredSpaces, p.Workspace)
	}

	return fmt.Errorf(
		"running commands in workspace %q is not allowed because this"+
			" directory is only configured for the following workspaces: %s",
		workspace,
		strings.Join(configuredSpaces, ", "),
	)
}

type Project struct {
	Dir                       string
	BranchRegex               *regexp.Regexp
	Workspace                 string
	Name                      *string
	TerraformDistribution     *string
	TerraformVersion          *version.Version
	Autoplan                  Autoplan
	PlanRequirements          []string
	ApplyRequirements         []string
	ImportRequirements        []string
	DependsOn                 []string
	DeleteSourceBranchOnMerge *bool
	RepoLocking               *bool
	RepoLocks                 *RepoLocks
	ExecutionOrderGroup       int
	PolicyCheck               *bool
	SilencePRComments         []string
	// Inputs override the server-side inputs when allowed.
	Inputs *Inputs
	// Tool overrides the server-side tool when allowed.
	Tool *string
	// Stack is the CDK Terrain stack the project plans, for tool cdktn.
	Stack string
}

// GetName returns the name of the project or an empty string if there is no
// project name.
func (p Project) GetName() string {
	if p.Name != nil {
		return *p.Name
	}
	return ""
}

type Autoplan struct {
	WhenModified []string
	Enabled      bool
}

type Stage struct {
	Steps []Step
}

// CommandShell sets up the shell for command execution
type CommandShell struct {
	Shell     string
	ShellArgs []string
}

func (s CommandShell) String() string {
	return fmt.Sprintf("%s %s", s.Shell, strings.Join(s.ShellArgs, " "))
}

// Step is one built-in step of the native workflow, such as init or plan.
type Step struct {
	StepName  string
	ExtraArgs []string
}

type Workflow struct {
	Name        string
	Apply       Stage
	Plan        Stage
	PolicyCheck Stage
	Import      Stage
	StateRm     Stage
}
