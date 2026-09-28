package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	. "github.com/petergtz/pegomock/v4"

	"github.com/runatlantis/atlantis/server"
	"github.com/runatlantis/atlantis/server/core/planrisk"
	"github.com/runatlantis/atlantis/server/events/models"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

// recordingEvaluator stands in for TypeSafe: it records the state it was sent
// and rates every plan's blast radius as broad.
type recordingEvaluator struct {
	mu     sync.Mutex
	states []string
}

func (r *recordingEvaluator) Evaluate(_ context.Context, state any, qs map[string]planrisk.Question) (*planrisk.Response, error) {
	b, _ := json.Marshal(state)
	r.mu.Lock()
	r.states = append(r.states, string(b))
	r.mu.Unlock()
	resp := &planrisk.Response{Model: "jev-test", Answers: map[string]planrisk.Answer{}}
	for id := range qs {
		if id == "blast_radius" {
			resp.Answers[id] = planrisk.Answer{Type: "score", Score: 2}
		} else {
			resp.Answers[id] = planrisk.Answer{Type: "noul", Noul: 0.01}
		}
	}
	return resp, nil
}

// TestGitHubWorkflowWithPlanRisk runs real terraform: autoplan must assess
// the plan from `terraform show -json`, render the risk, persist it, and the
// apply of the unapproved high-risk plan must be refused.
func TestGitHubWorkflowWithPlanRisk(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	ensureRunning014(t)
	RegisterMockTestingT(t)
	userConfig = server.UserConfig{}

	eval := &recordingEvaluator{}
	ctrl, vcsClient, githubGetter, atlantisWorkspace := setupE2E(t, "simple", setupOption{
		planRiskAssessor:      &planrisk.Assessor{Evaluator: eval, FailureTier: models.PlanRiskHigh},
		planRiskMaxUnapproved: models.PlanRiskMedium,
	})
	repoDir, headSHA := initializeRepo(t, "simple")
	atlantisWorkspace.TestingOverrideHeadCloneURL = fmt.Sprintf("file://%s", repoDir)
	When(githubGetter.GetPullRequest(Any[logging.SimpleLogging](), Any[models.Repo](), Any[int]())).ThenReturn(GitHubPullRequestParsed(headSHA), nil)
	When(vcsClient.GetModifiedFiles(Any[logging.SimpleLogging](), Any[models.Repo](), Any[models.PullRequest]())).ThenReturn([]string{"main.tf"}, nil)

	w := httptest.NewRecorder()
	ctrl.Post(w, GitHubPullRequestOpenedEvent(t, headSHA))
	ResponseContains(t, w, 200, "Processing...")

	w = httptest.NewRecorder()
	ctrl.Post(w, GitHubCommentEvent(t, "atlantis apply"))
	ResponseContains(t, w, 200, "Processing...")

	_, _, _, replies, _ := vcsClient.VerifyWasCalled(AtLeast(2)).CreateComment(
		Any[logging.SimpleLogging](), Any[models.Repo](), Any[int](), Any[string](), Any[string]()).GetAllCapturedArguments()

	Equals(t, 1, len(eval.states))
	Assert(t, strings.Contains(eval.states[0], `"address":"null_resource.simple[0]","type":"null_resource","action":"create"`), "state missing parsed change: %s", eval.states[0])
	Assert(t, strings.Contains(eval.states[0], `"directory":"."`), "state missing project: %s", eval.states[0])

	plan := replies[0]
	Assert(t, strings.Contains(plan, "**Plan risk:** :orange_circle: **high** (3 to create, 0 to update, 0 to delete, 0 to replace)"), "plan comment missing risk:\n%s", plan)

	apply := replies[1]
	Assert(t, strings.Contains(apply, "Plan risk is **high**"), "apply was not gated:\n%s", apply)
	Assert(t, !strings.Contains(apply, "Apply complete!"), "gated apply must not run:\n%s", apply)
}
