// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package valid

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	version "github.com/hashicorp/go-version"
	"github.com/runatlantis/atlantis/server/logging"
)

const MergeableCommandReq = "mergeable"
const ApprovedCommandReq = "approved"
const UnDivergedCommandReq = "undiverged"
const PoliciesPassedCommandReq = "policies_passed"
const PlanRequirementsKey = "plan_requirements"
const ApplyRequirementsKey = "apply_requirements"
const ImportRequirementsKey = "import_requirements"
const AllowedOverridesKey = "allowed_overrides"
const DefaultWorkflowName = "default"
const DeleteSourceBranchOnMergeKey = "delete_source_branch_on_merge"
const RepoLockingKey = "repo_locking"
const RepoLocksKey = "repo_locks"
const PolicyCheckKey = "policy_check"
const AutoDiscoverKey = "autodiscover"
const SilencePRCommentsKey = "silence_pr_comments"

var AllowedSilencePRComments = []string{"plan", "apply"}

// OverridableKeys are the keys allowed_overrides accepts.
var OverridableKeys = []string{PlanRequirementsKey, ApplyRequirementsKey, ImportRequirementsKey, DeleteSourceBranchOnMergeKey, RepoLockingKey, RepoLocksKey, PolicyCheckKey, SilencePRCommentsKey, InputsKey, ToolKey}

// DefaultAtlantisFile is the default name of the config file for each repo.
const DefaultAtlantisFile = "atlantis.yaml"

// NonOverridableApplyReqs will get applied across all "repos" in the server side config.
// If repo config is allowed overrides, they can override this.
// TODO: Make this more customizable, not everyone wants this rigid workflow
// maybe something along the lines of defining overridable/non-overridable apply
// requirements in the config and removing the flag to enable policy checking.
var NonOverridableApplyReqs = []string{PoliciesPassedCommandReq}

// GlobalCfg is the final parsed version of server-side repo config.
type GlobalCfg struct {
	Repos          []Repo
	PolicySets     PolicySets
	Metrics        Metrics
	TeamAuthz      TeamAuthz
	ExternalStores ExternalStores
}

// ExternalStores holds configuration for external storage backends.
type ExternalStores struct {
	PlanStore PlanStoreConfig
	LogStore  LogStoreConfig
}

// LogStoreConfig holds the type and backend-specific config for the job log
// archive.
type LogStoreConfig struct {
	Type string
	S3   S3StoreConfig
}

// PlanStoreConfig holds the type and backend-specific config for plan storage.
type PlanStoreConfig struct {
	Type string
	S3   S3StoreConfig
}

// S3StoreConfig holds configuration for an S3 (or S3-compatible) store.
type S3StoreConfig struct {
	Bucket               string
	Region               string
	Prefix               string
	Endpoint             string
	ForcePathStyle       bool
	Profile              string
	ServerSideEncryption string
	KMSKeyID             string
}

type Metrics struct {
	Statsd     *Statsd
	Prometheus *Prometheus
}

type Statsd struct {
	Port string
	Host string
}

type Prometheus struct {
	Endpoint string
}

// Repo is the final parsed version of server-side repo config.
type Repo struct {
	// ID is the exact match id of this config.
	// If IDRegex is set then this will be empty.
	ID string
	// IDRegex is the regex match for this config.
	// If ID is set then this will be nil.
	IDRegex                   *regexp.Regexp
	BranchRegex               *regexp.Regexp
	RepoConfigFile            string
	PlanRequirements          []string
	ApplyRequirements         []string
	ImportRequirements        []string
	PreWorkflowHooks          []*WorkflowHook
	PostWorkflowHooks         []*WorkflowHook
	AllowedOverrides          []string
	DeleteSourceBranchOnMerge *bool
	RepoLocking               *bool
	RepoLocks                 *RepoLocks
	PolicyCheck               *bool
	AutoDiscover              *AutoDiscover
	SilencePRComments         []string
	// Inputs are the default native inputs for matching repos.
	Inputs *Inputs
	// Tool is the default IaC tool for matching repos.
	Tool *string
	// TerraformDistribution is the default engine (terraform or opentofu)
	// for matching repos; a project's terraform_distribution wins.
	TerraformDistribution *string
}

type MergedProjectCfg struct {
	PlanRequirements   []string
	ApplyRequirements  []string
	ImportRequirements []string
	// Workflow is the native workflow with the project's inputs compiled
	// into its built-in steps.
	Workflow                  Workflow
	DependsOn                 []string
	RepoRelDir                string
	Workspace                 string
	Name                      string
	AutoplanEnabled           bool
	AutoplanWhenModified      []string
	AutoMergeDisabled         bool
	AutoMergeMethod           string
	TerraformDistribution     *string
	TerraformVersion          *version.Version
	RepoCfgVersion            int
	PolicySets                PolicySets
	DeleteSourceBranchOnMerge bool
	ExecutionOrderGroup       int
	RepoLocks                 RepoLocks
	PolicyCheck               bool
	SilencePRComments         []string
	// Env is set for every step of the project (from inputs).
	Env map[string]string
	// Tool runs the built-in steps; see Tools.
	Tool string
	// Stack is the CDK Terrain stack, for tool cdktn.
	Stack string
}

// WorkflowHook is a map of custom run commands to run before or after workflows.
type WorkflowHook struct {
	StepName        string
	RunCommand      string
	StepDescription string
	Shell           string
	ShellArgs       string
	Commands        string
}

// DefaultApplyStage is the Atlantis default apply stage.
var DefaultApplyStage = Stage{
	Steps: []Step{
		{
			StepName: "apply",
		},
	},
}

// DefaultPolicyCheckStage is the Atlantis default policy check stage.
var DefaultPolicyCheckStage = Stage{
	Steps: []Step{
		{
			StepName: "show",
		},
		{
			StepName: "policy_check",
		},
	},
}

// DefaultPlanStage is the Atlantis default plan stage.
var DefaultPlanStage = Stage{
	Steps: []Step{
		{
			StepName: "init",
		},
		{
			StepName: "plan",
		},
	},
}

// DefaultImportStage is the Atlantis default import stage.
var DefaultImportStage = Stage{
	Steps: []Step{
		{
			StepName: "init",
		},
		{
			StepName: "import",
		},
	},
}

// DefaultStateRmStage is the Atlantis default state_rm stage.
var DefaultStateRmStage = Stage{
	Steps: []Step{
		{
			StepName: "init",
		},
		{
			StepName: "state_rm",
		},
	},
}

type GlobalCfgArgs struct {
	RepoConfigFile string
	// No longer a user option as of https://github.com/runatlantis/atlantis/pull/3911,
	// but useful for tests to set to true to not require enumeration of allowed settings
	// on the repo side
	AllowAllRepoSettings bool
	PolicyCheckEnabled   bool
	PreWorkflowHooks     []*WorkflowHook
	PostWorkflowHooks    []*WorkflowHook
}

// NativeWorkflow returns the built-in steps every project runs. Native
// inputs compile into it; see Inputs.Apply.
func NativeWorkflow() Workflow {
	return Workflow{
		Name:        DefaultWorkflowName,
		Apply:       DefaultApplyStage,
		Plan:        DefaultPlanStage,
		PolicyCheck: DefaultPolicyCheckStage,
		Import:      DefaultImportStage,
		StateRm:     DefaultStateRmStage,
	}
}

func NewGlobalCfgFromArgs(args GlobalCfgArgs) GlobalCfg {
	// Must construct slices here instead of using a `var` declaration because
	// we treat nil slices differently.
	applyReqs := []string{}
	importReqs := []string{}
	planReqs := []string{}
	allowedOverrides := []string{}
	policyCheck := false
	if args.PolicyCheckEnabled {
		applyReqs = append(applyReqs, PoliciesPassedCommandReq)
		policyCheck = true
	}

	deleteSourceBranchOnMerge := false
	repoLocks := DefaultRepoLocks
	var silencePRComments []string
	if args.AllowAllRepoSettings {
		allowedOverrides = slices.Clone(OverridableKeys)
	}

	return GlobalCfg{
		Repos: []Repo{
			{
				IDRegex:                   regexp.MustCompile(".*"),
				BranchRegex:               regexp.MustCompile(".*"),
				RepoConfigFile:            args.RepoConfigFile,
				PlanRequirements:          planReqs,
				ApplyRequirements:         applyReqs,
				ImportRequirements:        importReqs,
				PreWorkflowHooks:          args.PreWorkflowHooks,
				PostWorkflowHooks:         args.PostWorkflowHooks,
				AllowedOverrides:          allowedOverrides,
				DeleteSourceBranchOnMerge: &deleteSourceBranchOnMerge,
				RepoLocks:                 &repoLocks,
				PolicyCheck:               &policyCheck,
				SilencePRComments:         silencePRComments,
			},
		},
		TeamAuthz: TeamAuthz{
			Args: make([]string, 0),
		},
	}
}

// IDMatches returns true if the repo ID otherID matches this config.
func (r Repo) IDMatches(otherID string) bool {
	if r.ID != "" {
		return r.ID == otherID
	}
	return r.IDRegex.MatchString(otherID)
}

// BranchMatches returns true if the branch other matches a branch regex (if preset).
func (r Repo) BranchMatches(other string) bool {
	if r.BranchRegex == nil {
		return true
	}
	return r.BranchRegex.MatchString(other)
}

// IDString returns a string representation of this config.
func (r Repo) IDString() string {
	if r.ID != "" {
		return r.ID
	}
	return "/" + r.IDRegex.String() + "/"
}

// MergeProjectCfg merges proj and rCfg with the global config to return a
// final config. It assumes that all configs have been validated.
func (g GlobalCfg) MergeProjectCfg(log logging.SimpleLogging, repoID string, proj Project, rCfg RepoCfg) MergedProjectCfg {
	log.Debug("MergeProjectCfg started")
	planReqs, applyReqs, importReqs, allowedOverrides, deleteSourceBranchOnMerge, repoLocks, policyCheck, _, silencePRComments := g.getMatchingCfg(log, repoID)
	// If repos are allowed to override certain keys then override them.
	for _, key := range allowedOverrides {
		switch key {
		case PlanRequirementsKey:
			if proj.PlanRequirements != nil {
				log.Debug("overriding server-defined %s with repo settings: [%s]", PlanRequirementsKey, strings.Join(proj.PlanRequirements, ","))
				planReqs = proj.PlanRequirements
			}
		case ApplyRequirementsKey:
			if proj.ApplyRequirements != nil {
				log.Debug("overriding server-defined %s with repo settings: [%s]", ApplyRequirementsKey, strings.Join(proj.ApplyRequirements, ","))
				applyReqs = proj.ApplyRequirements

				// Preserve policies_passed req if policy check is enabled
				if policyCheck {
					applyReqs = append(applyReqs, PoliciesPassedCommandReq)
				}
			}
		case ImportRequirementsKey:
			if proj.ImportRequirements != nil {
				log.Debug("overriding server-defined %s with repo settings: [%s]", ImportRequirementsKey, strings.Join(proj.ImportRequirements, ","))
				importReqs = proj.ImportRequirements
			}
		case DeleteSourceBranchOnMergeKey:
			//We check whether the server configured value and repo-root level
			//config is different. If it is then we change to the more granular.
			if rCfg.DeleteSourceBranchOnMerge != nil && deleteSourceBranchOnMerge != *rCfg.DeleteSourceBranchOnMerge {
				log.Debug("overriding server-defined %s with repo settings: [%t]", DeleteSourceBranchOnMergeKey, *rCfg.DeleteSourceBranchOnMerge)
				deleteSourceBranchOnMerge = *rCfg.DeleteSourceBranchOnMerge
			}
			//Then we check whether the more granular project based config is
			//different. If it is then we set it.
			if proj.DeleteSourceBranchOnMerge != nil && deleteSourceBranchOnMerge != *proj.DeleteSourceBranchOnMerge {
				log.Debug("overriding repo-root-defined %s with repo settings: [%t]", DeleteSourceBranchOnMergeKey, *proj.DeleteSourceBranchOnMerge)
				deleteSourceBranchOnMerge = *proj.DeleteSourceBranchOnMerge
			}
			log.Debug("merged deleteSourceBranchOnMerge: [%t]", deleteSourceBranchOnMerge)
		case RepoLockingKey:
			if proj.RepoLocking != nil {
				log.Debug("overriding server-defined %s with repo settings: [%t]", RepoLockingKey, *proj.RepoLocking)
				if *proj.RepoLocking && repoLocks.Mode == RepoLocksDisabledMode {
					repoLocks.Mode = DefaultRepoLocksMode
				} else if !*proj.RepoLocking {
					repoLocks.Mode = RepoLocksDisabledMode
				}
			}
		case RepoLocksKey:
			//We check whether the server configured value and repo-root level
			//config is different. If it is then we change to the more granular.
			if rCfg.RepoLocks != nil && repoLocks.Mode != rCfg.RepoLocks.Mode {
				log.Debug("overriding server-defined %s with repo settings: [%#v]", RepoLocksKey, rCfg.RepoLocks)
				repoLocks = *rCfg.RepoLocks
			}
			//Then we check whether the more granular project based config is
			//different. If it is then we set it.
			if proj.RepoLocks != nil && repoLocks.Mode != proj.RepoLocks.Mode {
				log.Debug("overriding repo-root-defined %s with repo settings: [%#v]", RepoLocksKey, *proj.RepoLocks)
				repoLocks = *proj.RepoLocks
			}
			log.Debug("merged repoLocks: [%#v]", repoLocks)
		case PolicyCheckKey:
			if proj.PolicyCheck != nil {
				log.Debug("overriding server-defined %s with repo settings: [%t]", PolicyCheckKey, *proj.PolicyCheck)
				policyCheck = *proj.PolicyCheck
			}
		case SilencePRCommentsKey:
			if proj.SilencePRComments != nil {
				log.Debug("overriding repo-root-defined %s with repo settings: [%s]", SilencePRCommentsKey, strings.Join(proj.SilencePRComments, ","))
				silencePRComments = proj.SilencePRComments
			} else if rCfg.SilencePRComments != nil {
				log.Debug("overriding server-defined %s with repo settings: [%s]", SilencePRCommentsKey, strings.Join(rCfg.SilencePRComments, ","))
				silencePRComments = rCfg.SilencePRComments
			}
		}
		log.Debug("MergeProjectCfg completed")
	}

	// Native inputs: server-side defaults, replaced field by field by the
	// project's inputs when the server allows it. They compile into the
	// built-in steps, so no custom step is involved.
	tool := g.RepoTool(repoID)
	if slices.Contains(allowedOverrides, ToolKey) && proj.Tool != nil {
		log.Debug("overriding server-defined %s with repo settings: %s", ToolKey, *proj.Tool)
		tool = *proj.Tool
	}
	inputs := g.matchingInputs(repoID)
	if slices.Contains(allowedOverrides, InputsKey) && proj.Inputs != nil {
		log.Debug("overriding server-defined %s with repo settings", InputsKey)
		inputs = inputs.Override(proj.Inputs)
	}
	workflow := inputs.Apply(NativeWorkflow())

	log.Debug("final settings: %s: [%s], %s: [%s], %s: [%s], %s: %t, %s: %s, %s: %t, %s: %s, %s: [%s]",
		PlanRequirementsKey, strings.Join(planReqs, ","),
		ApplyRequirementsKey, strings.Join(applyReqs, ","),
		ImportRequirementsKey, strings.Join(importReqs, ","),
		DeleteSourceBranchOnMergeKey, deleteSourceBranchOnMerge,
		RepoLockingKey, repoLocks.Mode,
		PolicyCheckKey, policyCheck,
		ToolKey, tool,
		SilencePRCommentsKey, strings.Join(silencePRComments, ","),
	)

	return MergedProjectCfg{
		PlanRequirements:          planReqs,
		ApplyRequirements:         applyReqs,
		ImportRequirements:        importReqs,
		Workflow:                  workflow,
		RepoRelDir:                proj.Dir,
		Workspace:                 proj.Workspace,
		DependsOn:                 proj.DependsOn,
		Name:                      proj.GetName(),
		AutoplanEnabled:           proj.Autoplan.Enabled,
		AutoplanWhenModified:      proj.Autoplan.WhenModified,
		TerraformDistribution:     cmpPtr(proj.TerraformDistribution, g.repoDistribution(repoID)),
		TerraformVersion:          proj.TerraformVersion,
		RepoCfgVersion:            rCfg.Version,
		PolicySets:                g.PolicySets,
		DeleteSourceBranchOnMerge: deleteSourceBranchOnMerge,
		ExecutionOrderGroup:       proj.ExecutionOrderGroup,
		RepoLocks:                 repoLocks,
		PolicyCheck:               policyCheck,
		SilencePRComments:         silencePRComments,
		Env:                       inputs.Env,
		Tool:                      tool,
		Stack:                     proj.Stack,
	}
}

// DefaultProjCfg returns the default project config for all projects under the
// repo with id repoID. It is used when there is no repo config.
func (g GlobalCfg) DefaultProjCfg(log logging.SimpleLogging, repoID string, repoRelDir string, workspace string) MergedProjectCfg {
	log.Debug("building config based on server-side config")
	planReqs, applyReqs, importReqs, _, deleteSourceBranchOnMerge, repoLocks, policyCheck, _, silencePRComments := g.getMatchingCfg(log, repoID)
	tool := g.RepoTool(repoID)
	inputs := g.matchingInputs(repoID)
	workflow := inputs.Apply(NativeWorkflow())
	return MergedProjectCfg{
		PlanRequirements:          planReqs,
		ApplyRequirements:         applyReqs,
		ImportRequirements:        importReqs,
		Workflow:                  workflow,
		RepoRelDir:                repoRelDir,
		Workspace:                 workspace,
		Name:                      "",
		AutoplanEnabled:           DefaultAutoPlanEnabled,
		AutoplanWhenModified:      []string{},
		TerraformDistribution:     g.repoDistribution(repoID),
		TerraformVersion:          nil,
		PolicySets:                g.PolicySets,
		DeleteSourceBranchOnMerge: deleteSourceBranchOnMerge,
		RepoLocks:                 repoLocks,
		PolicyCheck:               policyCheck,
		SilencePRComments:         silencePRComments,
		Env:                       inputs.Env,
		Tool:                      tool,
	}
}

// repoDistribution returns the terraform_distribution of the last server-side
// repo entry that matches repoID and sets one, or nil for the server default.
func (g GlobalCfg) repoDistribution(repoID string) *string {
	var d *string
	for _, repo := range g.Repos {
		if repo.IDMatches(repoID) && repo.TerraformDistribution != nil {
			d = repo.TerraformDistribution
		}
	}
	return d
}

// cmpPtr returns the first non-nil pointer.
func cmpPtr[T any](ptrs ...*T) *T {
	for _, p := range ptrs {
		if p != nil {
			return p
		}
	}
	return nil
}

// RepoTool returns the tool of the last server-side repo entry that
// matches repoID and sets one, or ToolTerraform.
func (g GlobalCfg) RepoTool(repoID string) string {
	tool := ToolTerraform
	for _, repo := range g.Repos {
		if repo.IDMatches(repoID) && repo.Tool != nil {
			tool = *repo.Tool
		}
	}
	return tool
}

// matchingInputs returns the inputs of the last server-side repo entry that
// matches repoID and sets inputs, like the other repo settings.
func (g GlobalCfg) matchingInputs(repoID string) Inputs {
	var inputs Inputs
	for _, repo := range g.Repos {
		if repo.IDMatches(repoID) && repo.Inputs != nil {
			inputs = *repo.Inputs
		}
	}
	return inputs
}

// RepoAutoDiscoverCfg returns the inherited AutoDiscover config from matching
// server-side repo config for repoID. If no matching repo defines
// AutoDiscover, this function returns nil.
func (g GlobalCfg) RepoAutoDiscoverCfg(repoID string) *AutoDiscover {
	var autoDiscover *AutoDiscover
	for _, repo := range g.Repos {
		if repo.IDMatches(repoID) && repo.AutoDiscover != nil {
			autoDiscover = repo.AutoDiscover
		}
	}
	return autoDiscover
}

// ValidateRepoCfg validates that rCfg for repo with id repoID is valid based
// on our global config.
func (g GlobalCfg) ValidateRepoCfg(rCfg RepoCfg, repoID string) error {
	// Check allowed overrides.
	var allowedOverrides []string
	for _, repo := range g.Repos {
		if repo.IDMatches(repoID) {
			if repo.AllowedOverrides != nil {
				allowedOverrides = repo.AllowedOverrides
			}
		}
	}
	for _, p := range rCfg.Projects {
		if p.ApplyRequirements != nil && !slices.Contains(allowedOverrides, ApplyRequirementsKey) {
			return fmt.Errorf("repo config not allowed to set '%s' key: server-side config needs '%s: [%s]'", ApplyRequirementsKey, AllowedOverridesKey, ApplyRequirementsKey)
		}
		if p.PlanRequirements != nil && !slices.Contains(allowedOverrides, PlanRequirementsKey) {
			return fmt.Errorf("repo config not allowed to set '%s' key: server-side config needs '%s: [%s]'", PlanRequirementsKey, AllowedOverridesKey, PlanRequirementsKey)
		}
		if p.ImportRequirements != nil && !slices.Contains(allowedOverrides, ImportRequirementsKey) {
			return fmt.Errorf("repo config not allowed to set '%s' key: server-side config needs '%s: [%s]'", ImportRequirementsKey, AllowedOverridesKey, ImportRequirementsKey)
		}
		if p.DeleteSourceBranchOnMerge != nil && !slices.Contains(allowedOverrides, DeleteSourceBranchOnMergeKey) {
			return fmt.Errorf("repo config not allowed to set '%s' key: server-side config needs '%s: [%s]'", DeleteSourceBranchOnMergeKey, AllowedOverridesKey, DeleteSourceBranchOnMergeKey)
		}
		if p.RepoLocking != nil && !slices.Contains(allowedOverrides, RepoLockingKey) {
			return fmt.Errorf("repo config not allowed to set '%s' key: server-side config needs '%s: [%s]'", RepoLockingKey, AllowedOverridesKey, RepoLockingKey)
		}
		if p.RepoLocks != nil && !slices.Contains(allowedOverrides, RepoLocksKey) {
			return fmt.Errorf("repo config not allowed to set '%s' key: server-side config needs '%s: [%s]'", RepoLocksKey, AllowedOverridesKey, RepoLocksKey)
		}
		if p.Tool != nil && !slices.Contains(allowedOverrides, ToolKey) {
			return fmt.Errorf("repo config not allowed to set '%s' key: server-side config needs '%s: [%s]'", ToolKey, AllowedOverridesKey, ToolKey)
		}
		if p.Inputs != nil && !slices.Contains(allowedOverrides, InputsKey) {
			return fmt.Errorf("repo config not allowed to set '%s' key: server-side config needs '%s: [%s]'", InputsKey, AllowedOverridesKey, InputsKey)
		}
		if p.SilencePRComments != nil {
			if !slices.Contains(allowedOverrides, SilencePRCommentsKey) {
				return fmt.Errorf(
					"repo config not allowed to set '%s' key: server-side config needs '%s: [%s]'",
					SilencePRCommentsKey,
					AllowedOverridesKey,
					SilencePRCommentsKey,
				)
			}
			for _, silenceStage := range p.SilencePRComments {
				if !slices.Contains(AllowedSilencePRComments, silenceStage) {
					return fmt.Errorf(
						"repo config '%s' key value of '%s' is not supported, supported values are [%s]",
						SilencePRCommentsKey,
						silenceStage,
						strings.Join(AllowedSilencePRComments, ", "),
					)
				}
			}
		}
	}

	return nil
}

// getMatchingCfg returns the key settings for repoID.
func (g GlobalCfg) getMatchingCfg(log logging.SimpleLogging, repoID string) (planReqs []string, applyReqs []string, importReqs []string, allowedOverrides []string, deleteSourceBranchOnMerge bool, repoLocks RepoLocks, policyCheck bool, autoDiscover AutoDiscover, silencePRComments []string) {
	toLog := make(map[string]string)
	traceF := func(repoIdx int, repoID string, key string, val any) string {
		from := "default server config"
		if repoIdx > 0 {
			from = fmt.Sprintf("repos[%d], id: %s", repoIdx, repoID)
		}
		var valStr string
		switch v := val.(type) {
		case string:
			valStr = fmt.Sprintf("%q", v)
		case []string:
			valStr = fmt.Sprintf("[%s]", strings.Join(v, ","))
		case bool:
			valStr = fmt.Sprintf("%t", v)
		default:
			valStr = "this is a bug"
		}

		return fmt.Sprintf("setting %s: %s from %s", key, valStr, from)
	}

	// Can't use raw.DefaultAutoDiscoverMode() because of an import cycle. Should refactor to avoid that.
	autoDiscover = AutoDiscover{Mode: AutoDiscoverAutoMode}
	repoLocking := true
	repoLocks = DefaultRepoLocks

	for _, key := range []string{PlanRequirementsKey, ApplyRequirementsKey, ImportRequirementsKey, AllowedOverridesKey, DeleteSourceBranchOnMergeKey, RepoLockingKey, RepoLocksKey, PolicyCheckKey, SilencePRCommentsKey} {
		for i, repo := range g.Repos {
			if repo.IDMatches(repoID) {
				switch key {
				case PlanRequirementsKey:
					if repo.PlanRequirements != nil {
						toLog[PlanRequirementsKey] = traceF(i, repo.IDString(), PlanRequirementsKey, repo.PlanRequirements)
						planReqs = repo.PlanRequirements
					}
				case ApplyRequirementsKey:
					if repo.ApplyRequirements != nil {
						toLog[ApplyRequirementsKey] = traceF(i, repo.IDString(), ApplyRequirementsKey, repo.ApplyRequirements)
						applyReqs = repo.ApplyRequirements
					}
				case ImportRequirementsKey:
					if repo.ImportRequirements != nil {
						toLog[ImportRequirementsKey] = traceF(i, repo.IDString(), ImportRequirementsKey, repo.ImportRequirements)
						importReqs = repo.ImportRequirements
					}
				case AllowedOverridesKey:
					if repo.AllowedOverrides != nil {
						toLog[AllowedOverridesKey] = traceF(i, repo.IDString(), AllowedOverridesKey, repo.AllowedOverrides)
						allowedOverrides = repo.AllowedOverrides
					}
				case DeleteSourceBranchOnMergeKey:
					if repo.DeleteSourceBranchOnMerge != nil {
						toLog[DeleteSourceBranchOnMergeKey] = traceF(i, repo.IDString(), DeleteSourceBranchOnMergeKey, *repo.DeleteSourceBranchOnMerge)
						deleteSourceBranchOnMerge = *repo.DeleteSourceBranchOnMerge
					}
				case RepoLockingKey:
					if repo.RepoLocking != nil {
						toLog[RepoLockingKey] = traceF(i, repo.IDString(), RepoLockingKey, *repo.RepoLocking)
						repoLocking = *repo.RepoLocking
					}
				case RepoLocksKey:
					if repo.RepoLocks != nil {
						toLog[RepoLocksKey] = traceF(i, repo.IDString(), RepoLocksKey, repo.RepoLocks.Mode)
						repoLocks = *repo.RepoLocks
					}
				case PolicyCheckKey:
					if repo.PolicyCheck != nil {
						toLog[PolicyCheckKey] = traceF(i, repo.IDString(), PolicyCheckKey, *repo.PolicyCheck)
						policyCheck = *repo.PolicyCheck
					}
				case AutoDiscoverKey:
					if repo.AutoDiscover != nil {
						toLog[AutoDiscoverKey] = traceF(i, repo.IDString(), AutoDiscoverKey, repo.AutoDiscover.Mode)
						autoDiscover = *repo.AutoDiscover
					}
				case SilencePRCommentsKey:
					if repo.SilencePRComments != nil {
						toLog[SilencePRCommentsKey] = traceF(i, repo.IDString(), SilencePRCommentsKey, repo.SilencePRComments)
						silencePRComments = repo.SilencePRComments
					}
				}
			}
		}
	}
	for _, l := range toLog {
		log.Debug("%s", l)
	}
	// repoLocking is deprecated and enabled by default, disable repo locks if it is explicitly disabled
	if !repoLocking {
		repoLocks.Mode = RepoLocksDisabledMode
	}
	return
}

// MatchingRepo returns an instance of Repo which matches a given repoID.
// If multiple repos match, return the last one for consistency with getMatchingCfg.
func (g GlobalCfg) MatchingRepo(repoID string) *Repo {
	for _, repo := range slices.Backward(g.Repos) {
		if repo.IDMatches(repoID) {
			return &repo
		}
	}
	return nil
}

// RepoConfigFile returns a repository specific file path
// If not defined, return atlantis.yaml as default
func (g GlobalCfg) RepoConfigFile(repoID string) string {
	repo := g.MatchingRepo(repoID)
	if repo != nil && repo.RepoConfigFile != "" {
		return repo.RepoConfigFile
	}
	return DefaultAtlantisFile
}
