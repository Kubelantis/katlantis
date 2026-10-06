// Package cdktn runs CDK Terrain apps for Atlantis: it synthesizes an app
// into Terraform configuration, finds the directory of each stack, and turns
// the stacks into projects.
package cdktn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/runatlantis/atlantis/server/logging"
)

const (
	// ConfigFile makes a directory a CDK Terrain app.
	ConfigFile = "cdktf.json"
	// OutDir is where Atlantis synthesizes an app, relative to the app.
	OutDir = "cdktf.out"
	// markerFile records the commit OutDir was synthesized from.
	markerFile = ".atlantis-synth"
	// synthTimeout bounds installing dependencies plus one synth.
	synthTimeout = 15 * time.Minute
)

// Manifest is the part of OutDir/manifest.json Atlantis uses.
type Manifest struct {
	Stacks map[string]ManifestStack `json:"stacks"`
}

// ManifestStack is one synthesized stack.
type ManifestStack struct {
	Name string `json:"name"`
	// WorkingDirectory is relative to OutDir.
	WorkingDirectory string   `json:"workingDirectory"`
	Dependencies     []string `json:"dependencies"`
}

// StackNames returns the stack names, sorted.
func (m *Manifest) StackNames() []string {
	names := make([]string, 0, len(m.Stacks))
	for n := range m.Stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Synthesizer synthesizes apps, at most once per app directory and commit.
type Synthesizer struct {
	// Binary is the cdktn binary; looked up on PATH when empty.
	Binary string
	// exec runs a command in dir; replaced in tests.
	exec func(ctx context.Context, dir string, name string, args ...string) error

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// Default is shared by project discovery and the Terraform client, so a
// pull request synthesizes each app once and never twice at the same time.
var Default = &Synthesizer{}

func (s *Synthesizer) lock(dir string) func() {
	s.mu.Lock()
	if s.locks == nil {
		s.locks = map[string]*sync.Mutex{}
	}
	l, ok := s.locks[dir]
	if !ok {
		l = &sync.Mutex{}
		s.locks[dir] = l
	}
	s.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// Synth synthesizes the app in appDir into appDir/OutDir unless that output
// is already from the current commit, and returns its manifest. Node
// dependencies are installed first when package.json exists and
// node_modules does not, without running install scripts.
func (s *Synthesizer) Synth(log logging.SimpleLogging, appDir string) (*Manifest, error) {
	unlock := s.lock(appDir)
	defer unlock()

	out := filepath.Join(appDir, OutDir)
	commit, commitErr := headCommit(appDir)
	if commitErr == nil {
		if prev, err := os.ReadFile(filepath.Join(out, markerFile)); err == nil && string(prev) == commit { // #nosec G304 -- inside the clone
			if m, err := readManifest(out); err == nil {
				return m, nil
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), synthTimeout)
	defer cancel()
	run := s.exec
	if run == nil {
		run = runIn
	}
	if exists(filepath.Join(appDir, "package.json")) && !exists(filepath.Join(appDir, "node_modules")) {
		args := []string{"install", "--ignore-scripts", "--no-audit", "--no-fund"}
		if exists(filepath.Join(appDir, "package-lock.json")) {
			args = []string{"ci", "--ignore-scripts", "--no-audit", "--no-fund"}
		}
		log.Info("installing Node dependencies of CDK Terrain app %s", appDir)
		if err := run(ctx, appDir, "npm", args...); err != nil {
			return nil, fmt.Errorf("installing dependencies: %w", err)
		}
	}
	bin := s.Binary
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("cdktn"); err != nil {
			return nil, errors.New("project uses tool \"cdktn\" but no cdktn binary was found on PATH; install cdktn-cli in the Atlantis image")
		}
	}
	log.Info("synthesizing CDK Terrain app %s", appDir)
	if err := run(ctx, appDir, bin, "synth", "--output", OutDir); err != nil {
		return nil, fmt.Errorf("cdktn synth: %w", err)
	}
	m, err := readManifest(out)
	if err != nil {
		return nil, err
	}
	if commitErr == nil {
		if err := os.WriteFile(filepath.Join(out, markerFile), []byte(commit), 0o600); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// StackDir synthesizes the app if needed and returns the directory Terraform
// runs in for stack. stack may be empty when the app has a single stack.
func (s *Synthesizer) StackDir(log logging.SimpleLogging, appDir string, stack string) (string, error) {
	m, err := s.Synth(log, appDir)
	if err != nil {
		return "", err
	}
	names := m.StackNames()
	if stack == "" {
		if len(names) != 1 {
			return "", fmt.Errorf("app %s has stacks %s; set the project's stack", appDir, strings.Join(names, ", "))
		}
		stack = names[0]
	}
	st, ok := m.Stacks[stack]
	if !ok {
		return "", fmt.Errorf("app %s has no stack %q; its stacks are %s", appDir, stack, strings.Join(names, ", "))
	}
	dir := filepath.Join(appDir, OutDir, filepath.FromSlash(st.WorkingDirectory))
	if rel, err := filepath.Rel(filepath.Join(appDir, OutDir), dir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("stack %q has a working directory outside %s", stack, OutDir)
	}
	return dir, nil
}

func readManifest(out string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(out, "manifest.json")) // #nosec G304 -- inside the clone
	if err != nil {
		return nil, fmt.Errorf("reading synthesized manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("reading synthesized manifest: %w", err)
	}
	for name, st := range m.Stacks {
		st.Dependencies = slices.Clone(st.Dependencies)
		m.Stacks[name] = st
	}
	return &m, nil
}

func runIn(ctx context.Context, dir string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed commands; dir is inside the clone
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CI=1", "CHECKPOINT_DISABLE=1")
	var outBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &outBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, tail(outBuf.String(), 4000))
	}
	return nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func headCommit(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output() // #nosec G204 -- fixed arguments
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
