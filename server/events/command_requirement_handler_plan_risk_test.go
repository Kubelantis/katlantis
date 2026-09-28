package events_test

import (
	"testing"

	"github.com/runatlantis/atlantis/server/events"
	"github.com/runatlantis/atlantis/server/events/command"
	"github.com/runatlantis/atlantis/server/events/models"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

func TestValidateApplyProject_PlanRisk(t *testing.T) {
	ctxFor := func(risk *models.PlanRisk, approved bool) command.ProjectContext {
		return command.ProjectContext{
			Log:             logging.NewNoopLogger(t),
			Pull:            models.PullRequest{Num: 1},
			ProjectPlanRisk: risk,
			PullReqStatus:   models.PullReqStatus{ApprovalStatus: models.ApprovalStatus{IsApproved: approved}},
		}
	}
	high := &models.PlanRisk{Tier: models.PlanRiskHigh, Findings: []models.PlanRiskFinding{{Address: "aws_db_instance.main", Reason: "may destroy stored data"}}}
	low := &models.PlanRisk{Tier: models.PlanRiskLow}
	unknown := &models.PlanRisk{Tier: models.PlanRiskUnknown, Error: "timeout"}

	cases := []struct {
		name     string
		max      models.PlanRiskTier
		ctx      command.ProjectContext
		wantFail string
	}{
		{"gate disabled", "", ctxFor(high, false), ""},
		{"low plan passes", models.PlanRiskLow, ctxFor(low, false), ""},
		{"high plan needs approval", models.PlanRiskLow, ctxFor(high, false),
			"Plan risk is **high** (`aws_db_instance.main` may destroy stored data). The pull request must be approved before running apply; plans rated low or lower can be applied without approval."},
		{"approved high plan passes", models.PlanRiskLow, ctxFor(high, true), ""},
		{"threshold allows high", models.PlanRiskHigh, ctxFor(high, false), ""},
		{"unknown ranks as high", models.PlanRiskMedium, ctxFor(unknown, false),
			"Plan risk is **unknown** (assessment failed). The pull request must be approved before running apply; plans rated medium or lower can be applied without approval."},
		{"unassessed plan must be replanned", models.PlanRiskCritical, ctxFor(nil, true), "The plan's risk has not been assessed. Run plan again before running apply."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &events.DefaultCommandRequirementHandler{PlanRiskMaxUnapproved: c.max}
			failure, err := h.ValidateApplyProject(t.TempDir(), c.ctx)
			Ok(t, err)
			Equals(t, c.wantFail, failure)
		})
	}
}
