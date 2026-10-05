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

// TestGitHubWorkflowWithTerragrunt runs real Terragrunt through the built-in
// steps: inputs from terragrunt.hcl and from native inputs both reach the
// plan, and apply uses the saved plan file.
func TestGitHubWorkflowWithTerragrunt(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	ensureRunning014(t)
	if _, err := exec.LookPath("terragrunt"); err != nil {
		t.Fatal("terragrunt must be installed to run this test; see testing/Dockerfile")
	}
	RegisterMockTestingT(t)
	userConfig = server.UserConfig{}

	ctrl, vcsClient, githubGetter, atlantisWorkspace := setupE2E(t, "terragrunt", setupOption{})
	repoDir, headSHA := initializeRepo(t, "terragrunt")
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

	plan := replies[0]
	for _, want := range []string{`+ env_name = "prod"`, `+ team     = "platform"`, "Plan: 1 to add"} {
		Assert(t, strings.Contains(plan, want), "plan is missing %q:\n%s", want, plan)
	}
	// Terragrunt's own log lines must not leak into the comment.
	Assert(t, !strings.Contains(plan, "STDOUT terraform"), "plan has Terragrunt log prefixes:\n%s", plan)
	Assert(t, strings.Contains(replies[1], "Apply complete!"), "apply failed:\n%s", replies[1])
}
