package events_test

import (
	"strings"
	"testing"

	"github.com/runatlantis/atlantis/server/events"
	"github.com/runatlantis/atlantis/server/events/command"
	"github.com/runatlantis/atlantis/server/events/models"
	"github.com/runatlantis/atlantis/server/i18n"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

func TestRenderPlanRisk(t *testing.T) {
	ctx := &command.Context{
		Log:  logging.NewNoopLogger(t).WithHistory(),
		Pull: models.PullRequest{BaseRepo: models.Repo{VCSHost: models.VCSHost{Type: models.Github}}},
	}
	result := func(risk *models.PlanRisk) command.Result {
		return command.Result{ProjectResults: []command.ProjectResult{{
			Command:    command.Plan,
			RepoRelDir: "path",
			Workspace:  "default",
			ProjectCommandOutput: command.ProjectCommandOutput{PlanSuccess: &models.PlanSuccess{
				TerraformOutput: "Plan: 0 to add, 0 to change, 1 to destroy.",
				ApplyCmd:        "atlantis apply -d path",
				RePlanCmd:       "atlantis plan -d path",
				Risk:            risk,
			}},
		}}}
	}
	render := func(lang string, risk *models.PlanRisk) string {
		r := events.NewMarkdownRenderer(false, false, false, false, false, false, "", "atlantis", false, false, i18n.TranslatorConfig{LanguageCode: lang})
		return r.Render(ctx, result(risk), &events.CommentCommand{Name: command.Plan})
	}

	risk := &models.PlanRisk{
		Tier: models.PlanRiskHigh, Deletes: 1,
		Findings: []models.PlanRiskFinding{{Address: "aws_db_instance.main", Action: "delete", Reason: "may destroy stored data"}},
	}
	out := render("en", risk)
	Assert(t, strings.Contains(out, "**Plan risk:** :orange_circle: **high** (0 to create, 0 to update, 1 to delete, 0 to replace)"), "missing summary:\n%s", out)
	Assert(t, strings.Contains(out, "* `aws_db_instance.main` may destroy stored data"), "missing finding:\n%s", out)
	Assert(t, strings.Index(out, "Plan risk") < strings.Index(out, "To **apply**"), "risk must come before apply instructions:\n%s", out)

	failed := render("en", &models.PlanRisk{Tier: models.PlanRiskHigh, Error: "TypeSafe returned 503"})
	Assert(t, strings.Contains(failed, "Risk could not be assessed (TypeSafe returned 503)"), "missing failure:\n%s", failed)

	es := render("es", risk)
	Assert(t, strings.Contains(es, "**Riesgo del plan:**"), "missing Spanish summary:\n%s", es)

	Assert(t, !strings.Contains(render("en", nil), "Plan risk"), "no risk section when disabled")
}
