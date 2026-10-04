package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/runatlantis/atlantis/testing"
)

func TestMigrateWorkflowsWritesFilesAndReport(t *testing.T) {
	t.Setenv("ATLANTIS_TYPESAFE_API_KEY", "")
	t.Setenv("TYPESAFE_API_KEY", "")
	dir := t.TempDir()
	repos := filepath.Join(dir, "repos.yaml")
	Ok(t, os.WriteFile(repos, []byte(`repos:
- id: /.*/
  workflow: custom
  post_workflow_hooks:
  - run: infracost breakdown --path .
workflows:
  custom:
    plan:
      steps:
      - init
      - plan:
          extra_args: [-var-file=prod.tfvars]
`), 0o600))
	report := filepath.Join(dir, "report.md")

	var stdout, stderr bytes.Buffer
	c := (&MigrateWorkflowsCmd{Stdout: &stdout, Stderr: &stderr}).Init()
	c.SetArgs([]string{"--repos-yaml", repos, "--write", "--report", report})
	Ok(t, c.Execute())

	migrated, err := os.ReadFile(repos)
	Ok(t, err)
	Assert(t, strings.Contains(string(migrated), "var_files: [prod.tfvars]"), "not migrated:\n%s", migrated)
	_, err = os.Stat(repos + ".orig")
	Ok(t, err)
	r, err := os.ReadFile(report)
	Ok(t, err)
	Assert(t, strings.Contains(string(r), "infracost breakdown --path ."), "report:\n%s", r)

	// --strict fails when behaviour was removed.
	Ok(t, os.Rename(repos+".orig", repos))
	c = (&MigrateWorkflowsCmd{Stdout: &stdout, Stderr: &stderr}).Init()
	c.SetArgs([]string{"--repos-yaml", repos, "--strict"})
	ErrContains(t, "removed or needs review", c.Execute())
}

func TestMigrateWorkflowsNeedsAFile(t *testing.T) {
	c := (&MigrateWorkflowsCmd{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Init()
	c.SetArgs(nil)
	ErrContains(t, "pass --repos-yaml, --atlantis-yaml, or both", c.Execute())
}
