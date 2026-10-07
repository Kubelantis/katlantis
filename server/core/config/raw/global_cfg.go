// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package raw

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation"
	"github.com/runatlantis/atlantis/server/core/config/valid"
)

// GlobalCfg is the raw schema for server-side repo config.
type GlobalCfg struct {
	Repos          []Repo         `yaml:"repos" json:"repos"`
	Workflows      Removed        `yaml:"workflows,omitempty" json:"workflows,omitempty"`
	PolicySets     PolicySets     `yaml:"policies" json:"policies"`
	Metrics        Metrics        `yaml:"metrics" json:"metrics"`
	TeamAuthz      TeamAuthz      `yaml:"team_authz" json:"team_authz"`
	ExternalStores ExternalStores `yaml:"external_stores" json:"external_stores"`
}

// ExternalStores is the raw schema for external storage backends.
type ExternalStores struct {
	PlanStore PlanStoreConfig `yaml:"plan_store" json:"plan_store"`
	LogStore  LogStoreConfig  `yaml:"log_store" json:"log_store"`
}

// PlanStoreConfig is the raw schema for plan storage configuration.
type PlanStoreConfig struct {
	Type string        `yaml:"type" json:"type"`
	S3   S3StoreConfig `yaml:"s3" json:"s3"`
}

// LogStoreConfig is the raw schema for job log archive configuration.
type LogStoreConfig struct {
	Type string        `yaml:"type" json:"type"`
	S3   S3StoreConfig `yaml:"s3" json:"s3"`
}

// S3StoreConfig is the raw schema for an S3 (or S3-compatible) store.
type S3StoreConfig struct {
	Bucket         string `yaml:"bucket" json:"bucket"`
	Region         string `yaml:"region" json:"region"`
	Prefix         string `yaml:"prefix" json:"prefix"`
	Endpoint       string `yaml:"endpoint" json:"endpoint"`
	ForcePathStyle bool   `yaml:"force_path_style" json:"force_path_style"`
	Profile        string `yaml:"profile" json:"profile"`
	// ServerSideEncryption is AES256, aws:kms or aws:kms:dsse; empty uses the
	// bucket's default encryption.
	ServerSideEncryption string `yaml:"server_side_encryption" json:"server_side_encryption"`
	// KMSKeyID selects the KMS key for aws:kms and aws:kms:dsse.
	KMSKeyID string `yaml:"kms_key_id" json:"kms_key_id"`
}

// Validate validates the external stores.
func (e ExternalStores) Validate() error {
	if err := validateStore("plan_store", e.PlanStore.Type, e.PlanStore.S3); err != nil {
		return err
	}
	return validateStore("log_store", e.LogStore.Type, e.LogStore.S3)
}

// Validate validates the plan store configuration.
func (p PlanStoreConfig) Validate() error {
	return validateStore("plan_store", p.Type, p.S3)
}

func validateStore(name, typ string, s3 S3StoreConfig) error {
	if typ == "" {
		return nil
	}
	if typ != "s3" {
		return fmt.Errorf("unsupported %s type %q: only 's3' is supported", strings.ReplaceAll(name, "_", " "), typ)
	}
	if s3.Bucket == "" {
		return fmt.Errorf("external_stores.%s.s3.bucket is required when type is 's3'", name)
	}
	if s3.Region == "" {
		return fmt.Errorf("external_stores.%s.s3.region is required when type is 's3'", name)
	}
	switch s3.ServerSideEncryption {
	case "", "AES256", "aws:kms", "aws:kms:dsse":
	default:
		return fmt.Errorf("external_stores.%s.s3.server_side_encryption %q must be one of AES256, aws:kms, aws:kms:dsse", name, s3.ServerSideEncryption)
	}
	if s3.KMSKeyID != "" && !strings.HasPrefix(s3.ServerSideEncryption, "aws:kms") {
		return fmt.Errorf("external_stores.%s.s3.kms_key_id requires server_side_encryption aws:kms or aws:kms:dsse", name)
	}
	return nil
}

// ToValid converts to the validated form.
func (e ExternalStores) ToValid() valid.ExternalStores {
	return valid.ExternalStores{
		PlanStore: valid.PlanStoreConfig{Type: e.PlanStore.Type, S3: e.PlanStore.S3.toValid()},
		LogStore:  valid.LogStoreConfig{Type: e.LogStore.Type, S3: e.LogStore.S3.toValid()},
	}
}

func (s S3StoreConfig) toValid() valid.S3StoreConfig {
	return valid.S3StoreConfig{
		Bucket:               s.Bucket,
		Region:               s.Region,
		Prefix:               s.Prefix,
		Endpoint:             s.Endpoint,
		ForcePathStyle:       s.ForcePathStyle,
		Profile:              s.Profile,
		ServerSideEncryption: s.ServerSideEncryption,
		KMSKeyID:             s.KMSKeyID,
	}
}

// Repo is the raw schema for repos in the server-side repo config.
type Repo struct {
	ID                        string         `yaml:"id" json:"id"`
	Branch                    string         `yaml:"branch" json:"branch"`
	RepoConfigFile            string         `yaml:"repo_config_file" json:"repo_config_file"`
	PlanRequirements          []string       `yaml:"plan_requirements" json:"plan_requirements"`
	ApplyRequirements         []string       `yaml:"apply_requirements" json:"apply_requirements"`
	ImportRequirements        []string       `yaml:"import_requirements" json:"import_requirements"`
	PreWorkflowHooks          []WorkflowHook `yaml:"pre_workflow_hooks" json:"pre_workflow_hooks"`
	Workflow                  Removed        `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	PostWorkflowHooks         []WorkflowHook `yaml:"post_workflow_hooks" json:"post_workflow_hooks"`
	AllowedWorkflows          Removed        `yaml:"allowed_workflows,omitempty" json:"allowed_workflows,omitempty"`
	AllowedOverrides          []string       `yaml:"allowed_overrides" json:"allowed_overrides"`
	AllowCustomWorkflows      Removed        `yaml:"allow_custom_workflows,omitempty" json:"allow_custom_workflows,omitempty"`
	DeleteSourceBranchOnMerge *bool          `yaml:"delete_source_branch_on_merge,omitempty" json:"delete_source_branch_on_merge,omitempty"`
	RepoLocking               *bool          `yaml:"repo_locking,omitempty" json:"repo_locking,omitempty"`
	RepoLocks                 *RepoLocks     `yaml:"repo_locks,omitempty" json:"repo_locks,omitempty"`
	PolicyCheck               *bool          `yaml:"policy_check,omitempty" json:"policy_check,omitempty"`
	CustomPolicyCheck         Removed        `yaml:"custom_policy_check,omitempty" json:"custom_policy_check,omitempty"`
	AutoDiscover              *AutoDiscover  `yaml:"autodiscover,omitempty" json:"autodiscover,omitempty"`
	SilencePRComments         []string       `yaml:"silence_pr_comments,omitempty" json:"silence_pr_comments,omitempty"`
	Inputs                    *Inputs        `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Tool                      *string        `yaml:"tool,omitempty" json:"tool,omitempty"`
	TerraformDistribution     *string        `yaml:"terraform_distribution,omitempty" json:"terraform_distribution,omitempty"`
}

func (g GlobalCfg) Validate() error {
	if err := removedKeys(map[string]Removed{"workflows": g.Workflows}); err != nil {
		return err
	}
	err := validation.ValidateStruct(&g,
		validation.Field(&g.Repos),
		validation.Field(&g.Metrics),
	)
	if err != nil {
		return err
	}

	if err := g.ExternalStores.Validate(); err != nil {
		return err
	}

	// Validate supported SilencePRComments values.
	for _, repo := range g.Repos {
		if repo.SilencePRComments == nil {
			continue
		}
		for _, silenceStage := range repo.SilencePRComments {
			if !slices.Contains(valid.AllowedSilencePRComments, silenceStage) {
				return fmt.Errorf(
					"server-side repo config '%s' key value of '%s' is not supported, supported values are [%s]",
					valid.SilencePRCommentsKey,
					silenceStage,
					strings.Join(valid.AllowedSilencePRComments, ", "),
				)
			}
		}
	}

	return nil
}

func (g GlobalCfg) ToValid(defaultCfg valid.GlobalCfg) valid.GlobalCfg {

	// assumes: globalcfg is always initialized with one repo .*
	globalPlanReqs := defaultCfg.Repos[0].PlanRequirements
	applyReqs := defaultCfg.Repos[0].ApplyRequirements
	var globalApplyReqs []string
	for _, req := range applyReqs {
		for _, nonOverridableReq := range valid.NonOverridableApplyReqs {
			if req == nonOverridableReq {
				globalApplyReqs = append(globalApplyReqs, req)
			}
		}
	}
	globalImportReqs := defaultCfg.Repos[0].ImportRequirements

	var repos []valid.Repo
	for _, r := range g.Repos {
		repos = append(repos, r.ToValid(globalPlanReqs, globalApplyReqs, globalImportReqs))
	}
	repos = append(defaultCfg.Repos, repos...)

	return valid.GlobalCfg{
		Repos:          repos,
		PolicySets:     g.PolicySets.ToValid(),
		Metrics:        g.Metrics.ToValid(),
		TeamAuthz:      g.TeamAuthz.ToValid(),
		ExternalStores: g.ExternalStores.ToValid(),
	}
}

// HasRegexID returns true if r is configured with a regex id instead of an
// exact match id.
func (r Repo) HasRegexID() bool {
	return strings.HasPrefix(r.ID, "/") && strings.HasSuffix(r.ID, "/")
}

// HasRegexBranch returns true if a branch regex was set.
func (r Repo) HasRegexBranch() bool {
	return strings.HasPrefix(r.Branch, "/") && strings.HasSuffix(r.Branch, "/")
}

func (r Repo) Validate() error {
	idValid := func(value any) error {
		id := value.(string)
		if !r.HasRegexID() {
			return nil
		}
		_, err := regexp.Compile(id[1 : len(id)-1])
		if err != nil {
			return fmt.Errorf("parsing: %s: %w", id, err)
		}
		return nil
	}

	branchValid := func(value any) error {
		branch := value.(string)
		if branch == "" {
			return nil
		}
		if !strings.HasPrefix(branch, "/") || !strings.HasSuffix(branch, "/") {
			return errors.New("regex must begin and end with a slash '/'")
		}
		withoutSlashes := branch[1 : len(branch)-1]
		_, err := regexp.Compile(withoutSlashes)
		if err != nil {
			return fmt.Errorf("parsing: %s: %w", branch, err)
		}
		return nil
	}

	repoConfigFileValid := func(value any) error {
		repoConfigFile := value.(string)
		if repoConfigFile == "" {
			return nil
		}
		if strings.HasPrefix(repoConfigFile, "/") {
			return errors.New("must not starts with a slash '/'")
		}
		if strings.Contains(repoConfigFile, "../") || strings.Contains(repoConfigFile, "..\\") {
			return errors.New("must not contains parent directory path like '../'")
		}
		return nil
	}

	overridesValid := func(value any) error {
		overrides := value.([]string)
		for _, o := range overrides {
			if o == "workflow" || o == "custom_policy_check" {
				return fmt.Errorf("%q can no longer be overridden: %s; %s", o, removedReplacement[o], MigrateHint)
			}
			if !slices.Contains(valid.OverridableKeys, o) {
				return fmt.Errorf("%q is not a valid override, only %s are supported", o, quotedList(valid.OverridableKeys))
			}
		}
		return nil
	}

	deleteSourceBranchOnMergeValid := func(value any) error {
		//TOBE IMPLEMENTED
		return nil
	}

	autoDiscoverValid := func(value any) error {
		autoDiscover := value.(*AutoDiscover)
		if autoDiscover != nil {
			return autoDiscover.Validate()
		}
		return nil
	}

	repoLocksValid := func(value any) error {
		repoLocks := value.(*RepoLocks)
		if repoLocks != nil {
			return repoLocks.Validate()
		}
		return nil
	}

	if err := removedKeys(map[string]Removed{
		"workflow":               r.Workflow,
		"allowed_workflows":      r.AllowedWorkflows,
		"allow_custom_workflows": r.AllowCustomWorkflows,
		"custom_policy_check":    r.CustomPolicyCheck,
	}); err != nil {
		return err
	}
	return validation.ValidateStruct(&r,
		validation.Field(&r.ID, validation.Required, validation.By(idValid)),
		validation.Field(&r.Branch, validation.By(branchValid)),
		validation.Field(&r.RepoConfigFile, validation.By(repoConfigFileValid)),
		validation.Field(&r.AllowedOverrides, validation.By(overridesValid)),
		validation.Field(&r.PlanRequirements, validation.By(validPlanReq)),
		validation.Field(&r.ApplyRequirements, validation.By(validApplyReq)),
		validation.Field(&r.ImportRequirements, validation.By(validImportReq)),
		validation.Field(&r.DeleteSourceBranchOnMerge, validation.By(deleteSourceBranchOnMergeValid)),
		validation.Field(&r.AutoDiscover, validation.By(autoDiscoverValid)),
		validation.Field(&r.RepoLocks, validation.By(repoLocksValid)),
		validation.Field(&r.Inputs),
		validation.Field(&r.Tool, validation.By(toolValid)),
		validation.Field(&r.TerraformDistribution, validation.By(validDistribution)),
	)
}

func (r Repo) ToValid(globalPlanReqs []string, globalApplyReqs []string, globalImportReqs []string) valid.Repo {
	var id string
	var idRegex *regexp.Regexp
	if r.HasRegexID() {
		withoutSlashes := r.ID[1 : len(r.ID)-1]
		// Safe to use MustCompile because we test it in Validate().
		idRegex = regexp.MustCompile(withoutSlashes)
	} else {
		id = r.ID
	}

	var branchRegex *regexp.Regexp
	if r.HasRegexBranch() {
		withoutSlashes := r.Branch[1 : len(r.Branch)-1]
		// Safe to use MustCompile because we test it in Validate().
		branchRegex = regexp.MustCompile(withoutSlashes)
	}

	var preWorkflowHooks []*valid.WorkflowHook
	if len(r.PreWorkflowHooks) > 0 {
		for _, hook := range r.PreWorkflowHooks {
			preWorkflowHooks = append(preWorkflowHooks, hook.ToValid())
		}
	}

	var postWorkflowHooks []*valid.WorkflowHook
	if len(r.PostWorkflowHooks) > 0 {
		for _, hook := range r.PostWorkflowHooks {
			postWorkflowHooks = append(postWorkflowHooks, hook.ToValid())
		}
	}

	var mergedPlanReqs []string
	mergedPlanReqs = append(mergedPlanReqs, r.PlanRequirements...)
	var mergedApplyReqs []string
	mergedApplyReqs = append(mergedApplyReqs, r.ApplyRequirements...)
	var mergedImportReqs []string
	mergedImportReqs = append(mergedImportReqs, r.ImportRequirements...)

	// only add global reqs if they don't exist already.
OuterGlobalPlanReqs:
	for _, globalReq := range globalPlanReqs {
		for _, currReq := range r.PlanRequirements {
			if globalReq == currReq {
				continue OuterGlobalPlanReqs
			}
		}

		// dont add policy_check step if repo have it explicitly disabled
		if globalReq == valid.PoliciesPassedCommandReq && r.PolicyCheck != nil && !*r.PolicyCheck {
			continue
		}
		mergedPlanReqs = append(mergedPlanReqs, globalReq)
	}
OuterGlobalApplyReqs:
	for _, globalReq := range globalApplyReqs {
		for _, currReq := range r.ApplyRequirements {
			if globalReq == currReq {
				continue OuterGlobalApplyReqs
			}
		}

		// dont add policy_check step if repo have it explicitly disabled
		if globalReq == valid.PoliciesPassedCommandReq && r.PolicyCheck != nil && !*r.PolicyCheck {
			continue
		}
		mergedApplyReqs = append(mergedApplyReqs, globalReq)
	}
OuterGlobalImportReqs:
	for _, globalReq := range globalImportReqs {
		for _, currReq := range r.ImportRequirements {
			if globalReq == currReq {
				continue OuterGlobalImportReqs
			}
		}

		// dont add policy_check step if repo have it explicitly disabled
		if globalReq == valid.PoliciesPassedCommandReq && r.PolicyCheck != nil && !*r.PolicyCheck {
			continue
		}
		mergedImportReqs = append(mergedImportReqs, globalReq)
	}

	var autoDiscover *valid.AutoDiscover
	if r.AutoDiscover != nil {
		autoDiscover = r.AutoDiscover.ToValid()
	}

	var repoLocks *valid.RepoLocks
	if r.RepoLocks != nil {
		repoLocks = r.RepoLocks.ToValid()
	}

	return valid.Repo{
		ID:                        id,
		IDRegex:                   idRegex,
		BranchRegex:               branchRegex,
		RepoConfigFile:            r.RepoConfigFile,
		PlanRequirements:          mergedPlanReqs,
		ApplyRequirements:         mergedApplyReqs,
		ImportRequirements:        mergedImportReqs,
		PreWorkflowHooks:          preWorkflowHooks,
		PostWorkflowHooks:         postWorkflowHooks,
		AllowedOverrides:          r.AllowedOverrides,
		DeleteSourceBranchOnMerge: r.DeleteSourceBranchOnMerge,
		RepoLocking:               r.RepoLocking,
		RepoLocks:                 repoLocks,
		PolicyCheck:               r.PolicyCheck,
		AutoDiscover:              autoDiscover,
		SilencePRComments:         r.SilencePRComments,
		Inputs:                    r.Inputs.ToValid(),
		Tool:                      r.Tool,
		TerraformDistribution:     r.TerraformDistribution,
	}
}

// quotedList renders keys as "a", "b", and "c".
func quotedList(keys []string) string {
	q := make([]string, len(keys))
	for i, k := range keys {
		q[i] = fmt.Sprintf("%q", k)
	}
	if len(q) < 2 {
		return strings.Join(q, "")
	}
	return strings.Join(q[:len(q)-1], ", ") + ", and " + q[len(q)-1]
}
