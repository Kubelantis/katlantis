package cdktn

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"sort"

	"github.com/runatlantis/atlantis/server/core/config/valid"
	"github.com/runatlantis/atlantis/server/logging"
)

// skipDirs are never searched for apps: dependencies and generated output.
var skipDirs = []string{".git", "node_modules", OutDir, ".terraform", ".terragrunt-cache"}

// Discoverer turns the stacks of every CDK Terrain app in a repository into
// projects.
type Discoverer struct {
	Synth *Synthesizer
}

// NewDiscoverer returns a Discoverer that shares Default with the Terraform
// client.
func NewDiscoverer() *Discoverer {
	return &Discoverer{Synth: Default}
}

// Discover finds every directory with a cdktf.json, synthesizes it, and
// returns one project per stack.
//
//   - The project is named after the stack, prefixed with the app directory
//     when the app is not at the repository root.
//   - A stack autoplans when any file of its app changes, except
//     dependencies, synthesized output and files of apps nested inside it.
//   - Stacks plan and apply after the stacks they depend on.
func (d *Discoverer) Discover(log logging.SimpleLogging, repoDir string) ([]valid.Project, error) {
	apps, err := findApps(repoDir)
	if err != nil {
		return nil, err
	}
	var projects []valid.Project
	for _, app := range apps {
		m, err := d.Synth.Synth(log, filepath.Join(repoDir, filepath.FromSlash(app)))
		if err != nil {
			return nil, fmt.Errorf("app %s: %w", app, err)
		}
		whenModified := []string{"**/*", "!" + OutDir + "/**", "!node_modules/**"}
		for _, other := range apps {
			if other != app && (app == "." || len(other) > len(app) && other[:len(app)+1] == app+"/") {
				rel := other
				if app != "." {
					rel = other[len(app)+1:]
				}
				whenModified = append(whenModified, "!"+path.Join(rel, "**"))
			}
		}
		for _, stack := range m.StackNames() {
			name := stack
			if app != "." {
				name = app + "/" + stack
			}
			projects = append(projects, valid.Project{
				Dir:                 app,
				Workspace:           "default",
				Name:                &name,
				Stack:               stack,
				Autoplan:            valid.Autoplan{Enabled: true, WhenModified: slices.Clone(whenModified)},
				ExecutionOrderGroup: depth(m, stack, map[string]int{}, map[string]bool{}),
			})
		}
	}
	log.Info("discovered %d CDK Terrain stack(s) in %d app(s)", len(projects), len(apps))
	return projects, nil
}

// findApps returns the repo-relative directories that contain a cdktf.json,
// sorted.
func findApps(repoDir string) ([]string, error) {
	var apps []string
	err := filepath.WalkDir(repoDir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if p != repoDir && slices.Contains(skipDirs, e.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Name() != ConfigFile {
			return nil
		}
		rel, err := filepath.Rel(repoDir, filepath.Dir(p))
		if err != nil {
			return err
		}
		apps = append(apps, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(apps)
	return apps, err
}

// depth is the length of the longest chain of stacks below stack.
func depth(m *Manifest, stack string, memo map[string]int, visiting map[string]bool) int {
	if v, ok := memo[stack]; ok {
		return v
	}
	if visiting[stack] {
		return 0
	}
	visiting[stack] = true
	best := 0
	for _, dep := range m.Stacks[stack].Dependencies {
		if _, ok := m.Stacks[dep]; ok {
			best = max(best, depth(m, dep, memo, visiting)+1)
		}
	}
	visiting[stack] = false
	memo[stack] = best
	return best
}
