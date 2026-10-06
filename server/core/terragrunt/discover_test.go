package terragrunt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/moby/patternmatcher"

	"github.com/runatlantis/atlantis/server/core/config/valid"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

// writeRepo writes files (path -> content) under a new directory.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, c := range files {
		full := filepath.Join(dir, p)
		Ok(t, os.MkdirAll(filepath.Dir(full), 0o755))
		Ok(t, os.WriteFile(full, []byte(c), 0o600))
	}
	return dir
}

// triggers reports whether a change to file autoplans project p, the way
// the project finder matches when_modified.
func triggers(t *testing.T, p valid.Project, file string) bool {
	t.Helper()
	var pats []string
	for _, wm := range p.Autoplan.WhenModified {
		if wm != "" && wm[0] == '!' {
			pats = append(pats, "!"+filepath.Join(p.Dir, wm[1:]))
		} else {
			pats = append(pats, filepath.Join(p.Dir, wm))
		}
	}
	pm, err := patternmatcher.New(pats)
	Ok(t, err)
	ok, err := pm.MatchesOrParentMatches(file)
	Ok(t, err)
	return ok
}

func byDir(ps []valid.Project) map[string]valid.Project {
	m := map[string]valid.Project{}
	for _, p := range ps {
		m[p.Dir] = p
	}
	return m
}

var layout = map[string]string{
	"root.hcl":                      "inputs = {}\n",
	"live/prod/env.hcl":             "locals { name = \"prod\" }\n",
	"live/_envcommon/vpc.hcl":       "",
	"modules/vpc/main.tf":           "",
	"modules/app/main.tf":           "",
	"live/prod/vpc/terragrunt.hcl":  "",
	"live/prod/vpc/cidr.txt":        "",
	"live/prod/app/terragrunt.hcl":  "locals {\n  env = get_env(\"X\")\n  extra_atlantis_dependencies = [\"../../../policies/*.json\"]\n  atlantis_terraform_version = \"1.9.0\"\n}\n",
	"live/stage/vpc/terragrunt.hcl": "locals { atlantis_autoplan = false }\n",
	"live/stage/old/terragrunt.hcl": "locals { atlantis_skip = true }\n",
}

var layoutUnits = []Unit{
	{Type: "unit", Path: "live/prod/app", Include: map[string]string{"root": "root.hcl"}, Dependencies: []string{"live/prod/vpc"},
		Reading: []string{"live/prod/env.hcl", "modules/app/main.tf", "root.hcl"}},
	{Type: "unit", Path: "live/prod/vpc", Include: map[string]string{"root": "root.hcl", "envcommon": "live/_envcommon/vpc.hcl"},
		Reading: []string{"live/_envcommon/vpc.hcl", "live/prod/env.hcl", "modules/vpc/main.tf", "root.hcl"}},
	{Type: "unit", Path: "live/stage/vpc", Reading: []string{"modules/vpc/main.tf"}},
	{Type: "unit", Path: "live/stage/old"},
}

func TestProjects(t *testing.T) {
	repo := writeRepo(t, layout)
	ps, err := Projects(repo, layoutUnits)
	Ok(t, err)
	got := byDir(ps)
	Equals(t, 3, len(ps))

	app, vpc, stage := got["live/prod/app"], got["live/prod/vpc"], got["live/stage/vpc"]
	_, skipped := got["live/stage/old"]
	Assert(t, !skipped, "atlantis_skip unit must be left out")

	// Dependencies plan and apply first.
	Equals(t, 0, vpc.ExecutionOrderGroup)
	Equals(t, 1, app.ExecutionOrderGroup)

	// Settings from locals.
	Equals(t, "1.9.0", app.TerraformVersion.String())
	Assert(t, app.Autoplan.Enabled, "autoplan defaults to on")
	Assert(t, !stage.Autoplan.Enabled, "atlantis_autoplan = false turns it off")

	cases := []struct {
		file      string
		app, vpc  bool
		stageUnit bool
	}{
		{"live/prod/app/terragrunt.hcl", true, false, false},
		{"live/prod/vpc/cidr.txt", true, true, false},  // own file; app depends on vpc
		{"live/_envcommon/vpc.hcl", true, true, false}, // included by vpc
		{"modules/vpc/main.tf", true, true, true},      // module source
		{"modules/vpc/outputs.tf", true, true, true},   // new file in a module source
		{"modules/app/main.tf", true, false, false},    //
		{"live/prod/env.hcl", true, true, false},       // read_terragrunt_config
		{"root.hcl", true, true, false},                // parent config
		{"policies/tags.json", true, false, false},     // extra_atlantis_dependencies
		{"live/stage/old/terragrunt.hcl", false, false, false},
		{"README.md", false, false, false},
	}
	for _, c := range cases {
		Equals(t, c.app, triggers(t, app, c.file))
		Equals(t, c.vpc, triggers(t, vpc, c.file))
		Equals(t, c.stageUnit, triggers(t, stage, c.file))
	}
}

func TestProjectsDropParentsAndNestedUnits(t *testing.T) {
	repo := writeRepo(t, map[string]string{
		"terragrunt.hcl":          "",
		"a/terragrunt.hcl":        "",
		"a/nested/terragrunt.hcl": "",
		"b/terragrunt.hcl":        "",
	})
	ps, err := Projects(repo, []Unit{
		{Type: "unit", Path: "."},
		{Type: "unit", Path: "a", Include: map[string]string{"root": "terragrunt.hcl"}},
		{Type: "unit", Path: "a/nested"},
		{Type: "unit", Path: "b", Include: map[string]string{"root": "terragrunt.hcl"}, Dependencies: []string{"a/nested"}},
		{Type: "stack", Path: "s"},
	})
	Ok(t, err)
	got := byDir(ps)
	_, root := got["."]
	Assert(t, !root, "an included parent config is not a project")
	Equals(t, 3, len(ps))

	// a/nested's files belong to a/nested, not a.
	Assert(t, !triggers(t, got["a"], "a/nested/main.tf"), "nested unit files must not autoplan the parent")
	Assert(t, triggers(t, got["a"], "a/main.tf"), "own files autoplan")
	Assert(t, triggers(t, got["a/nested"], "a/nested/main.tf"), "own files autoplan")
	Assert(t, triggers(t, got["b"], "a/nested/main.tf"), "dependency files autoplan")
}

func TestProjectsRejectInvalidLocals(t *testing.T) {
	repo := writeRepo(t, map[string]string{"u/terragrunt.hcl": "locals { atlantis_skip = get_env(\"SKIP\") }\n"})
	_, err := Projects(repo, []Unit{{Type: "unit", Path: "u"}})
	ErrContains(t, "u/terragrunt.hcl: local atlantis_skip must be a literal value", err)

	repo = writeRepo(t, map[string]string{"u/terragrunt.hcl": "locals { atlantis_autoplan = \"yes please\" }\n"})
	_, err = Projects(repo, []Unit{{Type: "unit", Path: "u"}})
	ErrContains(t, "local atlantis_autoplan must be a bool", err)

	repo = writeRepo(t, map[string]string{"u/terragrunt.hcl": "locals { extra_atlantis_dependencies = [\"../../outside\"] }\n"})
	_, err = Projects(repo, []Unit{{Type: "unit", Path: "u"}})
	ErrContains(t, "outside the repository", err)
}

func TestDiscoverCachesPerCommit(t *testing.T) {
	repo := writeRepo(t, layout)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "x"}} {
		Ok(t, exec.Command("git", append([]string{"-C", repo}, args...)...).Run())
	}
	calls := 0
	d := &Discoverer{find: func(context.Context, string) ([]Unit, error) { calls++; return layoutUnits, nil }}
	for range 3 {
		ps, err := d.Discover(logging.NewNoopLogger(t), repo)
		Ok(t, err)
		Equals(t, 3, len(ps))
	}
	Equals(t, 1, calls)
}

// TestDiscoverWithTerragrunt runs the real terragrunt find.
func TestDiscoverWithTerragrunt(t *testing.T) {
	if _, err := exec.LookPath("terragrunt"); err != nil {
		t.Skip("terragrunt is not installed")
	}
	repo := writeRepo(t, map[string]string{
		"root.hcl":                     "locals { env = read_terragrunt_config(find_in_parent_folders(\"env.hcl\")) }\n",
		"live/prod/env.hcl":            "locals { name = \"prod\" }\n",
		"modules/vpc/main.tf":          "",
		"live/prod/vpc/terragrunt.hcl": "include \"root\" { path = find_in_parent_folders(\"root.hcl\") }\nterraform { source = \"${get_repo_root()}/modules/vpc\" }\n",
		"live/prod/app/terragrunt.hcl": "include \"root\" { path = find_in_parent_folders(\"root.hcl\") }\ndependency \"vpc\" { config_path = \"../vpc\" }\n",
	})
	Ok(t, exec.Command("git", "-C", repo, "init", "-q").Run())
	ps, err := NewDiscoverer().Discover(logging.NewNoopLogger(t), repo)
	Ok(t, err)
	got := byDir(ps)
	Equals(t, 2, len(ps))
	Equals(t, 1, got["live/prod/app"].ExecutionOrderGroup)
	Assert(t, triggers(t, got["live/prod/app"], "modules/vpc/main.tf"), "app autoplans when its dependency's module changes")
	Assert(t, triggers(t, got["live/prod/vpc"], "live/prod/env.hcl"), "vpc autoplans when a config it reads changes")
	Assert(t, !triggers(t, got["live/prod/vpc"], "live/prod/app/terragrunt.hcl"), "vpc does not depend on app")
}
