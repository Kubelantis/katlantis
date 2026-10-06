package events_test

import (
	"fmt"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	. "github.com/petergtz/pegomock/v4"

	"github.com/runatlantis/atlantis/server"
	"github.com/runatlantis/atlantis/server/events/models"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

// TestGitHubWorkflowWithCdktn runs a real CDK Terrain app with no
// atlantis.yaml: Atlantis installs its dependencies, synthesizes it, plans
// each stack in its synthesized directory with native inputs, and applies
// the stack the other depends on first.
func TestGitHubWorkflowWithCdktn(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	ensureRunning014(t)
	for _, bin := range []string{"cdktn", "npm"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("%s must be installed to run this test; see testing/Dockerfile", bin)
		}
	}
	RegisterMockTestingT(t)
	userConfig = server.UserConfig{}

	ctrl, vcsClient, githubGetter, atlantisWorkspace := setupE2E(t, "cdktn", setupOption{})
	repoDir, headSHA := initializeRepo(t, "cdktn")
	atlantisWorkspace.TestingOverrideHeadCloneURL = fmt.Sprintf("file://%s", repoDir)
	When(githubGetter.GetPullRequest(Any[logging.SimpleLogging](), Any[models.Repo](), Any[int]())).ThenReturn(GitHubPullRequestParsed(headSHA), nil)
	When(vcsClient.GetModifiedFiles(Any[logging.SimpleLogging](), Any[models.Repo](), Any[models.PullRequest]())).ThenReturn([]string{"main.js"}, nil)

	w := httptest.NewRecorder()
	ctrl.Post(w, GitHubPullRequestOpenedEvent(t, headSHA))
	ResponseContains(t, w, 200, "Processing...")

	w = httptest.NewRecorder()
	ctrl.Post(w, GitHubCommentEvent(t, "atlantis apply"))
	ResponseContains(t, w, 200, "Processing...")

	_, _, _, replies, _ := vcsClient.VerifyWasCalled(AtLeast(2)).CreateComment(
		Any[logging.SimpleLogging](), Any[models.Repo](), Any[int](), Any[string](), Any[string]()).GetAllCapturedArguments()

	plan := replies[0]
	for _, want := range []string{"project: `vpc`", "project: `app`", `+ name = "vpc-prod"`, `+ name = "app-prod"`} {
		Assert(t, strings.Contains(plan, want), "plan is missing %q:\n%s", want, plan)
	}
	Assert(t, strings.Index(plan, "project: `vpc`") < strings.Index(plan, "project: `app`"), "the dependency must plan first:\n%s", plan)

	apply := replies[1]
	Equals(t, 2, strings.Count(apply, "Apply complete!"))
	Assert(t, strings.Index(apply, "project: `vpc`") < strings.Index(apply, "project: `app`"), "the dependency must apply first:\n%s", apply)
}
