package models

import "time"

// PlanRiskTier is the policy outcome of a plan risk assessment, ordered from
// least to most restrictive.
type PlanRiskTier string

const (
	// PlanRiskLow plans may be applied without extra review.
	PlanRiskLow PlanRiskTier = "low"
	// PlanRiskMedium plans require the pull request to be approved.
	PlanRiskMedium PlanRiskTier = "medium"
	// PlanRiskHigh plans require approval and are highlighted to reviewers.
	PlanRiskHigh PlanRiskTier = "high"
	// PlanRiskCritical plans require approval and an explicit risk acknowledgement.
	PlanRiskCritical PlanRiskTier = "critical"
	// PlanRiskUnknown means the assessment could not be completed.
	PlanRiskUnknown PlanRiskTier = "unknown"
)

// Rank orders tiers so policy code can compare them. Unknown ranks with high:
// an assessment that failed must not be more permissive than a risky one.
func (t PlanRiskTier) Rank() int {
	switch t {
	case PlanRiskLow:
		return 0
	case PlanRiskMedium:
		return 1
	case PlanRiskHigh, PlanRiskUnknown:
		return 2
	case PlanRiskCritical:
		return 3
	}
	return 2
}

// PlanRiskFinding explains why a resource change contributed to the tier.
type PlanRiskFinding struct {
	Address string
	Action  string
	Reason  string
	// Probability is the model probability behind the finding, if any.
	Probability float64 `json:",omitempty"`
}

// PlanRisk is the persisted result of assessing a plan. It is stored with the
// project status so apply requirements can evaluate it in a later request.
type PlanRisk struct {
	Tier PlanRiskTier
	// Counts from terraform show -json, computed in code.
	Creates  int
	Updates  int
	Deletes  int
	Replaces int
	// BlastRadius is the model's expected blast radius level (0-3).
	BlastRadius float64
	Findings    []PlanRiskFinding `json:",omitempty"`
	// Model is the versioned model ID that produced the judgments.
	Model string `json:",omitempty"`
	// Error is set when the assessment failed and the tier fell back to the
	// configured failure tier.
	Error      string `json:",omitempty"`
	AssessedAt time.Time
}
