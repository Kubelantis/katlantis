package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PullStatus is the plan/apply state of every project in a pull request.
// One object exists per pull request; it is deleted when the pull is closed.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=aps,categories=atlantis
// +kubebuilder:printcolumn:name="Repo",type=string,JSONPath=`.spec.pull.baseRepo.fullName`
// +kubebuilder:printcolumn:name="Pull",type=integer,JSONPath=`.spec.pull.num`
// +kubebuilder:printcolumn:name="Head",type=string,JSONPath=`.spec.pull.headCommit`,priority=1
// +kubebuilder:printcolumn:name="Projects",type=integer,JSONPath=`.spec.projectCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type PullStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PullStatusSpec `json:"spec"`
}

// PullStatusSpec mirrors models.PullStatus.
type PullStatusSpec struct {
	Pull PullRequest `json:"pull"`
	// ProjectCount is len(projects), kept for kubectl output.
	ProjectCount int             `json:"projectCount"`
	Projects     []ProjectStatus `json:"projects,omitempty"`
}

// PullRequest mirrors models.PullRequest. The clone URL and body are
// intentionally not stored: the clone URL can embed VCS credentials, and the
// body is unbounded while objects are limited in size.
type PullRequest struct {
	Num                      int    `json:"num"`
	HeadCommit               string `json:"headCommit"`
	URL                      string `json:"url,omitempty"`
	HeadBranch               string `json:"headBranch,omitempty"`
	BaseBranch               string `json:"baseBranch,omitempty"`
	HardenedNonPRRefCheckout bool   `json:"hardenedNonPRRefCheckout,omitempty"`
	Author                   string `json:"author,omitempty"`
	// +kubebuilder:validation:Enum=open;closed
	State    string `json:"state"`
	BaseRepo Repo   `json:"baseRepo"`
}

// Repo mirrors models.Repo without credentials.
type Repo struct {
	FullName          string `json:"fullName"`
	Owner             string `json:"owner,omitempty"`
	Name              string `json:"name,omitempty"`
	SanitizedCloneURL string `json:"sanitizedCloneURL,omitempty"`
	VCSHostname       string `json:"vcsHostname"`
	VCSHostType       int    `json:"vcsHostType"`
}

// ProjectStatus mirrors models.ProjectStatus.
type ProjectStatus struct {
	Workspace   string `json:"workspace"`
	RepoRelDir  string `json:"repoRelDir"`
	ProjectName string `json:"projectName,omitempty"`
	// +kubebuilder:validation:Enum=plan_errored;planned;planned_no_changes;apply_errored;applied;plan_discarded;policy_check_errored;policy_check_passed
	Status       string            `json:"status"`
	PolicyStatus []PolicySetStatus `json:"policyStatus,omitempty"`
	PlanRisk     *PlanRisk         `json:"planRisk,omitempty"`
}

// PolicySetStatus mirrors models.PolicySetStatus.
type PolicySetStatus struct {
	PolicySetName   string              `json:"policySetName"`
	Passed          bool                `json:"passed"`
	Approvals       []PolicySetApproval `json:"approvals,omitempty"`
	Hashes          []string            `json:"hashes,omitempty"`
	PolicyItemRegex string              `json:"policyItemRegex,omitempty"`
}

// PolicySetApproval mirrors models.PolicySetApproval.
type PolicySetApproval struct {
	Approver string   `json:"approver"`
	Hashes   []string `json:"hashes,omitempty"`
}

// PlanRisk mirrors models.PlanRisk.
type PlanRisk struct {
	// +kubebuilder:validation:Enum=low;medium;high;critical;unknown
	Tier     string `json:"tier"`
	Creates  int    `json:"creates"`
	Updates  int    `json:"updates"`
	Deletes  int    `json:"deletes"`
	Replaces int    `json:"replaces"`
	// BlastRadius is a decimal string (e.g. "1.42"); CRDs avoid floats.
	BlastRadius string            `json:"blastRadius,omitempty"`
	Findings    []PlanRiskFinding `json:"findings,omitempty"`
	Model       string            `json:"model,omitempty"`
	Error       string            `json:"error,omitempty"`
	AssessedAt  metav1.Time       `json:"assessedAt"`
}

// PlanRiskFinding mirrors models.PlanRiskFinding.
type PlanRiskFinding struct {
	Address     string `json:"address"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
	Probability string `json:"probability,omitempty"`
}

// PullStatusList is a list of PullStatus.
//
// +kubebuilder:object:root=true
type PullStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PullStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PullStatus{}, &PullStatusList{})
}
