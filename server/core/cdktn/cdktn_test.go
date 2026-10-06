package cdktn

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(dir, p)
		Ok(t, os.MkdirAll(filepath.Dir(full), 0o755))
		Ok(t, os.WriteFile(full, []byte(c), 0o600))
	}
}

func gitCommit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "x", "--allow-empty"}} {
		Ok(t, exec.Command("git", append([]string{"-C", dir}, args...)...).Run())
	}
}

// fakeSynth records commands and writes a manifest with the given stacks
// (name -> dependencies) when synth runs.
type fakeSynth struct {
	stacks map[string][]string
	calls  []string
}

func (f *fakeSynth) exec(_ context.Context, dir string, name string, args ...string) error {
	f.calls = append(f.calls, filepath.Base(name)+" "+strings.Join(args, " "))
	if len(args) == 0 || args[0] != "synth" {
		return nil
	}
	m := Manifest{Stacks: map[string]ManifestStack{}}
	for s, deps := range f.stacks {
		m.Stacks[s] = ManifestStack{Name: s, WorkingDirectory: "stacks/" + s, Dependencies: deps}
		if err := os.MkdirAll(filepath.Join(dir, OutDir, "stacks", s), 0o755); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(m)
	return os.WriteFile(filepath.Join(dir, OutDir, "manifest.json"), b, 0o600)
}

func TestSynthOncePerCommit(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{ConfigFile: "{}", "package.json": "{}", "package-lock.json": "{}"})
	gitCommit(t, dir)
	f := &fakeSynth{stacks: map[string][]string{"vpc": nil}}
	s := &Synthesizer{Binary: "cdktn", exec: f.exec}
	log := logging.NewNoopLogger(t)

	for range 2 {
		d, err := s.StackDir(log, dir, "")
		Ok(t, err)
		Equals(t, filepath.Join(dir, OutDir, "stacks", "vpc"), d)
	}
	// npm ci because of the lock file; no install scripts; one synth.
	Equals(t, []string{"npm ci --ignore-scripts --no-audit --no-fund", "cdktn synth --output cdktf.out"}, f.calls)

	// A new commit synthesizes again.
	writeFiles(t, dir, map[string]string{"main.js": "x"})
	gitCommit(t, dir)
	_, err := s.Synth(log, dir)
	Ok(t, err)
	// The fake npm creates no node_modules, so it installs again.
	Equals(t, []string{"npm ci --ignore-scripts --no-audit --no-fund", "cdktn synth --output cdktf.out"}, f.calls[2:])
}

func TestStackSelection(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{ConfigFile: "{}"})
	gitCommit(t, dir)
	f := &fakeSynth{stacks: map[string][]string{"vpc": nil, "app": {"vpc"}}}
	s := &Synthesizer{Binary: "cdktn", exec: f.exec}
	log := logging.NewNoopLogger(t)

	_, err := s.StackDir(log, dir, "")
	ErrContains(t, "has stacks app, vpc; set the project's stack", err)
	_, err = s.StackDir(log, dir, "db")
	ErrContains(t, `has no stack "db"; its stacks are app, vpc`, err)
	d, err := s.StackDir(log, dir, "app")
	Ok(t, err)
	Equals(t, filepath.Join(dir, OutDir, "stacks", "app"), d)
	// No package.json: nothing to install.
	Equals(t, []string{"cdktn synth --output cdktf.out"}, f.calls)
}

func TestStackDirMustStayInOutput(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{ConfigFile: "{}", OutDir + "/manifest.json": `{"stacks":{"x":{"name":"x","workingDirectory":"../../etc"}}}`})
	gitCommit(t, dir)
	head, err := headCommit(dir)
	Ok(t, err)
	writeFiles(t, dir, map[string]string{OutDir + "/" + markerFile: head})
	_, err = (&Synthesizer{}).StackDir(logging.NewNoopLogger(t), dir, "x")
	ErrContains(t, "outside cdktf.out", err)
}

func TestDiscover(t *testing.T) {
	repo := t.TempDir()
	writeFiles(t, repo, map[string]string{
		"infra/" + ConfigFile:                "{}",
		"infra/main.js":                      "",
		"infra/node_modules/x/" + ConfigFile: "{}",
		"infra/nested/" + ConfigFile:         "{}",
		"README.md":                          "",
	})
	gitCommit(t, repo)
	f := &fakeSynth{stacks: map[string][]string{"vpc": nil, "app": {"vpc"}}}
	d := &Discoverer{Synth: &Synthesizer{Binary: "cdktn", exec: f.exec}}
	ps, err := d.Discover(logging.NewNoopLogger(t), repo)
	Ok(t, err)
	Equals(t, 4, len(ps)) // infra and infra/nested, two stacks each; node_modules skipped

	app := ps[0]
	Equals(t, "infra", app.Dir)
	Equals(t, "app", app.Stack)
	Equals(t, "infra/app", app.GetName())
	Equals(t, 1, app.ExecutionOrderGroup)
	Equals(t, 0, ps[1].ExecutionOrderGroup) // vpc
	Equals(t, []string{"**/*", "!cdktf.out/**", "!node_modules/**", "!nested/**"}, app.Autoplan.WhenModified)
	Equals(t, "infra/nested/app", ps[2].GetName())
}

// TestSynthWithCdktn synthesizes a real app with the cdktn CLI.
func TestSynthWithCdktn(t *testing.T) {
	if _, err := exec.LookPath("cdktn"); err != nil {
		t.Skip("cdktn is not installed")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed")
	}
	src, err := filepath.Abs("../../controllers/events/testdata/test-repos/cdktn")
	Ok(t, err)
	repo := t.TempDir()
	Ok(t, os.CopyFS(repo, os.DirFS(src)))
	gitCommit(t, repo)
	ps, err := NewDiscoverer().Discover(logging.NewNoopLogger(t), repo)
	Ok(t, err)
	Equals(t, 2, len(ps))
	Equals(t, "app", ps[0].Stack)
	Equals(t, 1, ps[0].ExecutionOrderGroup)
	_, err = os.Stat(filepath.Join(repo, OutDir, "stacks", "app", "cdk.tf.json"))
	Ok(t, err)
}
