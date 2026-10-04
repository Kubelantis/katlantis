package events_test

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/petergtz/pegomock/v4"

	"github.com/runatlantis/atlantis/server"
	"github.com/runatlantis/atlantis/server/events/models"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

// TestGitHubWorkflowWithNativeInputs runs real terraform with server-side
// native inputs: a var file, a var, a backend config and a templated env
// var, all without a custom workflow.
func TestGitHubWorkflowWithNativeInputs(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	ensureRunning014(t)
	RegisterMockTestingT(t)
	userConfig = server.UserConfig{}

	ctrl, vcsClient, githubGetter, atlantisWorkspace := setupE2E(t, "native-inputs", setupOption{})
	repoDir, headSHA := initializeRepo(t, "native-inputs")
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
	for _, want := range []string{`+ env_name = "prod"`, `+ team     = "platform"`, `+ repo_tag = "atlantis-tests:."`} {
		Assert(t, strings.Contains(plan, want), "plan is missing %q:\n%s", want, plan)
	}
	Assert(t, strings.Contains(replies[1], "Apply complete!"), "apply failed:\n%s", replies[1])

	// The backend config reached init: state is in the configured file.
	clone := filepath.Join(atlantisWorkspace.DataDir, "repos", "runatlantis", "atlantis-tests", "2", "default")
	_, err := os.Stat(filepath.Join(clone, "custom.tfstate"))
	Ok(t, err)
	_, err = os.Stat(filepath.Join(clone, "terraform.tfstate"))
	Assert(t, os.IsNotExist(err), "state must not be in the default file")
}
