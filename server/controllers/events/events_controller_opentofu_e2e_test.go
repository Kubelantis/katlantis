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

// TestGitHubWorkflowWithOpenTofu plans and applies with real OpenTofu, chosen
// by the server-side terraform_distribution on a server whose default is
// Terraform, with no required_version: Atlantis must use the tofu binary's
// version, not the Terraform default. Each fixture keeps its resource in a
// .tofu file, which only OpenTofu reads.
func TestGitHubWorkflowWithOpenTofu(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	ensureRunning014(t)
	cases := []struct {
		repo     string
		modified string
		bins     []string
		want     []string
	}{
		{"opentofu", "main.tofu", []string{"tofu"}, []string{"OpenTofu will perform", `+ input  = "opentofu"`}},
		{"terragrunt-opentofu", "main.tofu", []string{"tofu", "terragrunt"}, []string{"OpenTofu will perform", `+ input  = "opentofu"`}},
		{"cdktn-opentofu", "main.js", []string{"tofu", "cdktn", "npm"}, []string{"OpenTofu", `+ engine = "net-on-opentofu"`}},
	}
	for _, c := range cases {
		t.Run(c.repo, func(t *testing.T) {
			for _, bin := range c.bins {
				if _, err := exec.LookPath(bin); err != nil {
					t.Fatalf("%s must be installed to run this test; see testing/Dockerfile", bin)
				}
			}
			RegisterMockTestingT(t)
			userConfig = server.UserConfig{}

			ctrl, vcsClient, githubGetter, atlantisWorkspace := setupE2E(t, c.repo, setupOption{})
			repoDir, headSHA := initializeRepo(t, c.repo)
			atlantisWorkspace.TestingOverrideHeadCloneURL = fmt.Sprintf("file://%s", repoDir)
			When(githubGetter.GetPullRequest(Any[logging.SimpleLogging](), Any[models.Repo](), Any[int]())).ThenReturn(GitHubPullRequestParsed(headSHA), nil)
			When(vcsClient.GetModifiedFiles(Any[logging.SimpleLogging](), Any[models.Repo](), Any[models.PullRequest]())).ThenReturn([]string{c.modified}, nil)

			w := httptest.NewRecorder()
			ctrl.Post(w, GitHubPullRequestOpenedEvent(t, headSHA))
			ResponseContains(t, w, 200, "Processing...")

			w = httptest.NewRecorder()
			ctrl.Post(w, GitHubCommentEvent(t, "atlantis apply"))
			ResponseContains(t, w, 200, "Processing...")

			_, _, _, replies, _ := vcsClient.VerifyWasCalled(AtLeast(2)).CreateComment(
				Any[logging.SimpleLogging](), Any[models.Repo](), Any[int](), Any[string](), Any[string]()).GetAllCapturedArguments()
			plan := replies[0]
			for _, want := range c.want {
				Assert(t, strings.Contains(plan, want), "plan is missing %q:\n%s", want, plan)
			}
			Assert(t, strings.Contains(replies[1], "Apply complete!"), "apply failed:\n%s", replies[1])
		})
	}
}
