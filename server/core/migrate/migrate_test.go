package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/runatlantis/atlantis/server/core/config"
	"github.com/runatlantis/atlantis/server/core/config/valid"
	"github.com/runatlantis/atlantis/server/core/migrate"
	"github.com/runatlantis/atlantis/server/core/typesafe"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

// fixtures holds copies of e2e fixtures as they were before migration.
const fixtures = "testdata"

func read(t *testing.T, path string) []byte {
	b, err := os.ReadFile(filepath.Join(fixtures, path))
	Ok(t, err)
	return b
}

func notesWith(notes []migrate.Note, a migrate.Action) []migrate.Note {
	var out []migrate.Note
	for _, n := range notes {
		if n.Action == a {
			out = append(out, n)
		}
	}
	return out
}

// The tfvars fixture's two workflows become project inputs that a real
// Atlantis parser accepts, with the rm -rf .terraform workaround dropped.
func TestMigrateRepoConfigTfvarsFixture(t *testing.T) {
	r, err := migrate.MigrateRepoConfig("atlantis.yaml", read(t, "tfvars-yaml/atlantis.yaml"), nil)
	Ok(t, err)
	out := string(r.Output)
	Assert(t, !strings.Contains(out, "workflows:") && !strings.Contains(out, "workflow:"), "workflows must be gone:\n%s", out)
	for _, want := range []string{"var_files: [default.tfvars]", "backend_config: [default.backend.tfvars]", "var_files: [staging.tfvars]", "backend_config: [staging.backend.tfvars]"} {
		Assert(t, strings.Contains(out, want), "missing %q:\n%s", want, out)
	}
	dropped := notesWith(r.Notes, migrate.Dropped)
	Assert(t, len(dropped) >= 3, "expected the cleanup and echo steps to be dropped: %+v", dropped)
	Equals(t, 0, len(notesWith(r.Notes, migrate.Removed)))
	Equals(t, 0, len(notesWith(r.Notes, migrate.Review)))

	// The result loads with the real parser and compiles to the same arguments.
	dir := t.TempDir()
	Ok(t, os.WriteFile(filepath.Join(dir, "atlantis.yaml"), r.Output, 0o600))
	global := valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{AllowAllRepoSettings: true})
	pv := &cfg.ParserValidator{}
	repoCfg, err := pv.ParseRepoCfg(dir, global, "github.com/org/repo", "")
	Ok(t, err)
	staging := global.MergeProjectCfg(logging.NewNoopLogger(t), "github.com/org/repo", repoCfg.Projects[1], repoCfg)
	var args []string
	for _, s := range staging.Workflow.Plan.Steps {
		args = append(args, s.StepName+":"+strings.Join(s.ExtraArgs, " "))
	}
	Equals(t, []string{"init:-reconfigure -backend-config=staging.backend.tfvars", "plan:-var-file=staging.tfvars"}, args)
}

// Server-side workflows and override keys are migrated; hooks are kept.
func TestMigrateServerConfigFixture(t *testing.T) {
	r, err := migrate.MigrateServerConfig("repos.yaml", read(t, "server-side-cfg/repos.yaml"))
	Ok(t, err)
	out := string(r.Output)
	for _, gone := range []string{"workflows:", "workflow: custom"} {
		Assert(t, !strings.Contains(out, gone), "%q must be gone:\n%s", gone, out)
	}
	for _, kept := range []string{"pre_workflow_hooks", "post_workflow_hooks"} {
		Assert(t, strings.Contains(out, kept), "%q must be kept:\n%s", kept, out)
	}
	Assert(t, strings.Contains(out, "allowed_overrides: [inputs]"), "override not converted:\n%s", out)
	Equals(t, 0, len(notesWith(r.Notes, migrate.Removed)))
	pv := &cfg.ParserValidator{}
	_, err = pv.ParseGlobalCfg(writeTemp(t, r.Output), valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{}))
	Ok(t, err)
}

func writeTemp(t *testing.T, b []byte) string {
	p := filepath.Join(t.TempDir(), "repos.yaml")
	Ok(t, os.WriteFile(p, b, 0o600))
	return p
}

func TestMigrateServerDefaultWorkflowBecomesCatchAll(t *testing.T) {
	src := `# keep me
repos:
- id: github.com/org/special
  apply_requirements: [approved]
workflows:
  default:
    plan:
      steps:
      - env:
          name: TF_AWS_DEFAULT_TAGS_repository
          command: 'echo "github.com/${BASE_REPO_OWNER}/${BASE_REPO_NAME}"'
      - init
      - plan:
          extra_args: ["-var", "region=eu-west-1", "-lock-timeout=5m"]
`
	r, err := migrate.MigrateServerConfig("repos.yaml", []byte(src))
	Ok(t, err)
	out := string(r.Output)
	Assert(t, strings.Contains(out, "# keep me"), "comments must survive:\n%s", out)
	Assert(t, strings.Index(out, "id: /.*/") < strings.Index(out, "github.com/org/special"), "catch-all must come first:\n%s", out)
	for _, want := range []string{`TF_AWS_DEFAULT_TAGS_repository: "github.com/${BASE_REPO_OWNER}/${BASE_REPO_NAME}"`, "region: eu-west-1", "plan: [-lock-timeout=5m]"} {
		Assert(t, strings.Contains(out, want), "missing %q:\n%s", want, out)
	}
	_, err = (&cfg.ParserValidator{}).ParseGlobalCfg(writeTemp(t, r.Output), valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{}))
	Ok(t, err)
}

func TestRunStepsBecomeBuiltinsOrAreReported(t *testing.T) {
	src := `workflows:
  w:
    plan:
      steps:
      - run: terraform workspace select $WORKSPACE
      - run: terraform init -backend-config=prod.hcl
      - run: terraform plan -input=false -refresh -out $PLANFILE
      - run: infracost breakdown --path . | tee cost.txt
      - multienv: ./secrets.sh
    apply:
      steps:
      - run: terraform apply $PLANFILE
    policy_check:
      steps:
      - show
      - run: checkov -f $SHOWFILE
`
	r, err := migrate.MigrateServerConfig("repos.yaml", []byte(src))
	Ok(t, err)
	in := r.Workflows["w"]
	Equals(t, []string{"prod.hcl"}, in.BackendConfig)
	Equals(t, []string{"-refresh"}, in.ExtraArgs["plan"])
	removed := notesWith(r.Notes, migrate.Removed)
	var cmds []string
	for _, n := range removed {
		cmds = append(cmds, n.Command)
	}
	Equals(t, []string{"infracost breakdown --path . | tee cost.txt", "./secrets.sh", "checkov -f $SHOWFILE"}, cmds)
	// The policy stage lost its policy_check step: flagged for review.
	var review []string
	for _, n := range notesWith(r.Notes, migrate.Review) {
		review = append(review, n.Detail)
	}
	Assert(t, strings.Contains(strings.Join(review, "\n"), "native policy_check runs show, policy_check; this workflow ran show"), "missing stage review: %v", review)
}

type fakeEval struct{ label string }

func (f fakeEval) Evaluate(_ context.Context, state any, _ map[string]typesafe.Question) (*typesafe.Response, error) {
	return &typesafe.Response{Answers: map[string]typesafe.Answer{"category": {Choice: f.label, Confidence: 0.9}}}, nil
}

func TestLabelerAnnotatesOnlyRemovedCommands(t *testing.T) {
	notes := []migrate.Note{
		{Action: migrate.Removed, Command: "infracost breakdown"},
		{Action: migrate.Dropped, Command: "echo hi"},
	}
	Ok(t, (&migrate.JevLabeler{Evaluator: fakeEval{"cost_estimation"}, MinConfidence: 0.8}).Label(context.Background(), notes))
	Equals(t, "cost_estimation", notes[0].Label)
	Assert(t, strings.Contains(notes[0].Suggestion, "Infracost"), "suggestion: %s", notes[0].Suggestion)
	Equals(t, "", notes[1].Label)
	report := migrate.Report(notes)
	Assert(t, strings.Contains(report, "| repos") || strings.Contains(report, "cost_estimation"), "report:\n%s", report)
}

func TestTerragruntRunStepsBecomeToolTerragrunt(t *testing.T) {
	src := `repos:
- id: /.*/
  workflow: tg
  allowed_overrides: [workflow]
workflows:
  tg:
    plan:
      steps:
      - run: terragrunt workspace select $WORKSPACE
      - run: terragrunt init --non-interactive -upgrade
      - run: terragrunt run -- plan -input=false -var-file=prod.tfvars -out $PLANFILE
    apply:
      steps:
      - run: terragrunt apply $PLANFILE
`
	r, err := migrate.MigrateServerConfig("repos.yaml", []byte(src))
	Ok(t, err)
	in := r.Workflows["tg"]
	Equals(t, "terragrunt", in.Tool)
	Equals(t, []string{"prod.tfvars"}, in.VarFiles)
	Equals(t, []string{"-upgrade"}, in.ExtraArgs["init"])
	Equals(t, 0, len(notesWith(r.Notes, migrate.Removed)))
	Equals(t, 0, len(notesWith(r.Notes, migrate.Review)))

	out := string(r.Output)
	for _, want := range []string{"tool: terragrunt", "allowed_overrides: [inputs, tool]", "var_files: [prod.tfvars]"} {
		Assert(t, strings.Contains(out, want), "output is missing %q:\n%s", want, out)
	}
	_, err = (&cfg.ParserValidator{}).ParseGlobalCfg(writeTemp(t, r.Output), valid.NewGlobalCfgFromArgs(valid.GlobalCfgArgs{}))
	Ok(t, err)
}

func TestTerragruntWithPlainBuiltinsNeedsReview(t *testing.T) {
	src := `workflows:
  tg:
    plan:
      steps:
      - init
      - run: terragrunt plan -out $PLANFILE
`
	r, err := migrate.MigrateServerConfig("repos.yaml", []byte(src))
	Ok(t, err)
	Equals(t, "terragrunt", r.Workflows["tg"].Tool)
	var review []string
	for _, n := range notesWith(r.Notes, migrate.Review) {
		review = append(review, n.Detail)
	}
	Assert(t, strings.Contains(strings.Join(review, "\n"), "now run through terragrunt too"), "missing review note: %v", review)
}
