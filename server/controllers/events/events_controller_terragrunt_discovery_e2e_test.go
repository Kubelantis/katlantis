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

// TestGitHubWorkflowWithTerragruntDiscovery runs real Terragrunt with no
// atlantis.yaml: Atlantis discovers the units, autoplans every unit that
// uses the changed module (including the one that only depends on it),
// skips units marked atlantis_skip, and applies dependencies first.
func TestGitHubWorkflowWithTerragruntDiscovery(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	ensureRunning014(t)
	if _, err := exec.LookPath("terragrunt"); err != nil {
		t.Fatal("terragrunt must be installed to run this test; see testing/Dockerfile")
	}
	RegisterMockTestingT(t)
	userConfig = server.UserConfig{}

	ctrl, vcsClient, githubGetter, atlantisWorkspace := setupE2E(t, "terragrunt-discovery", setupOption{})
	repoDir, headSHA := initializeRepo(t, "terragrunt-discovery")
	atlantisWorkspace.TestingOverrideHeadCloneURL = fmt.Sprintf("file://%s", repoDir)
	When(githubGetter.GetPullRequest(Any[logging.SimpleLogging](), Any[models.Repo](), Any[int]())).ThenReturn(GitHubPullRequestParsed(headSHA), nil)
	When(vcsClient.GetModifiedFiles(Any[logging.SimpleLogging](), Any[models.Repo](), Any[models.PullRequest]())).ThenReturn([]string{"modules/net/main.tf"}, nil)

	w := httptest.NewRecorder()
	ctrl.Post(w, GitHubPullRequestOpenedEvent(t, headSHA))
	ResponseContains(t, w, 200, "Processing...")

	w = httptest.NewRecorder()
	ctrl.Post(w, GitHubCommentEvent(t, "atlantis apply"))
	ResponseContains(t, w, 200, "Processing...")

	_, _, _, replies, _ := vcsClient.VerifyWasCalled(AtLeast(2)).CreateComment(
		Any[logging.SimpleLogging](), Any[models.Repo](), Any[int](), Any[string](), Any[string]()).GetAllCapturedArguments()

	plan := replies[0]
	for _, want := range []string{"dir: `live/vpc`", "dir: `live/app`", `"prod-vpc"`, `"prod-app-in-pending-vpc"`} {
		Assert(t, strings.Contains(plan, want), "plan is missing %q:\n%s", want, plan)
	}
	Assert(t, !strings.Contains(plan, "live/old"), "atlantis_skip unit was planned:\n%s", plan)
	Assert(t, strings.Index(plan, "dir: `live/vpc`") < strings.Index(plan, "dir: `live/app`"), "the dependency must plan first:\n%s", plan)

	apply := replies[1]
	Equals(t, 2, strings.Count(apply, "Apply complete!"))
	Assert(t, strings.Index(apply, "dir: `live/vpc`") < strings.Index(apply, "dir: `live/app`"), "the dependency must apply first:\n%s", apply)
}
