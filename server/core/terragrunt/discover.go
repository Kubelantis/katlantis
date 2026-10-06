// Package terragrunt turns the units of a Terragrunt repository into Atlantis
// projects.
package terragrunt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-version"

	"github.com/runatlantis/atlantis/server/core/config/valid"
	"github.com/runatlantis/atlantis/server/logging"
)

// ConfigFile is the file that makes a directory a Terragrunt unit.
const ConfigFile = "terragrunt.hcl"

// discoverTimeout bounds one `terragrunt find` run.
const discoverTimeout = 5 * time.Minute

// Unit is one entry of `terragrunt find --json`. Paths are relative to the
// repository root.
type Unit struct {
	Type         string            `json:"type"`
	Path         string            `json:"path"`
	Include      map[string]string `json:"include"`
	Dependencies []string          `json:"dependencies"`
	Reading      []string          `json:"reading"`
}

// Discoverer finds the Terragrunt units of a cloned repository.
type Discoverer struct {
	// Binary is the terragrunt binary; looked up on PATH when empty.
	Binary string
	// find runs terragrunt find; replaced in tests.
	find func(ctx context.Context, repoDir string) ([]Unit, error)

	mu    sync.Mutex
	cache map[string][]valid.Project
}

// NewDiscoverer returns a Discoverer that runs the terragrunt binary on PATH.
func NewDiscoverer() *Discoverer {
	return &Discoverer{}
}

// Discover returns one project per Terragrunt unit under repoDir, sorted by
// directory. Results are cached per repository directory and commit, since a
// command parses the repo config several times.
func (d *Discoverer) Discover(log logging.SimpleLogging, repoDir string) ([]valid.Project, error) {
	key := repoDir
	if sha, err := headCommit(repoDir); err == nil {
		key += "@" + sha
	} else {
		key = ""
	}
	if key != "" {
		d.mu.Lock()
		cached, ok := d.cache[key]
		d.mu.Unlock()
		if ok {
			return slices.Clone(cached), nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), discoverTimeout)
	defer cancel()
	find := d.find
	if find == nil {
		find = d.runFind
	}
	units, err := find(ctx, repoDir)
	if err != nil {
		return nil, err
	}
	projects, err := Projects(repoDir, units)
	if err != nil {
		return nil, err
	}
	log.Info("discovered %d Terragrunt unit(s)", len(projects))

	if key != "" {
		d.mu.Lock()
		if d.cache == nil || len(d.cache) >= 256 {
			d.cache = map[string][]valid.Project{}
		}
		d.cache[key] = projects
		d.mu.Unlock()
	}
	return slices.Clone(projects), nil
}

func headCommit(repoDir string) (string, error) {
	out, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output() // #nosec G204 -- fixed arguments
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *Discoverer) runFind(ctx context.Context, repoDir string) ([]Unit, error) {
	bin := d.Binary
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("terragrunt"); err != nil {
			return nil, errors.New("repo uses tool \"terragrunt\" but no terragrunt binary was found on PATH; install it in the Atlantis image")
		}
	}
	cmd := exec.CommandContext(ctx, bin, "find", "--json", "--dependencies", "--include", "--reading") // #nosec G204 -- fixed arguments
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "TG_NON_INTERACTIVE=true", "TG_LOG_LEVEL=error", "TG_NO_COLOR=true")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running terragrunt find: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var units []Unit
	if err := json.Unmarshal(stdout.Bytes(), &units); err != nil {
		return nil, fmt.Errorf("reading terragrunt find output: %w", err)
	}
	return units, nil
}

// Projects converts units into projects.
//
//   - A unit whose terragrunt.hcl another unit includes is a parent
//     configuration, not a project.
//   - A project autoplans when a file in its directory changes, when a file
//     it reads changes (included configs, module sources, read_terragrunt_config),
//     or when any of that changes for a unit it depends on, directly or not.
//   - Projects are ordered so a unit plans and applies after its dependencies.
//   - atlantis_* locals in the unit's terragrunt.hcl adjust the project; see
//     Settings.
func Projects(repoDir string, units []Unit) ([]valid.Project, error) {
	parents := map[string]bool{}
	for _, u := range units {
		for _, inc := range u.Include {
			parents[path.Clean(inc)] = true
		}
	}
	byDir := map[string]Unit{}
	var dirs []string
	for _, u := range units {
		dir := path.Clean(u.Path)
		if u.Type != "unit" || parents[path.Join(dir, ConfigFile)] {
			continue
		}
		u.Path = dir
		byDir[dir] = u
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	settings := map[string]Settings{}
	for _, dir := range dirs {
		s, err := ReadSettings(path.Join(repoDir, dir, ConfigFile))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path.Join(dir, ConfigFile), err)
		}
		settings[dir] = s
	}

	// triggers holds a unit's own repo-relative patterns, without its
	// dependencies.
	triggers := map[string][]string{}
	for _, dir := range dirs {
		u := byDir[dir]
		t := []string{path.Join(dir, "**", "*")}
		for _, f := range u.Reading {
			f = path.Clean(f)
			t = append(t, f)
			// A new file in a module source changes the module too.
			if strings.HasSuffix(f, ".tf") || strings.HasSuffix(f, ".tf.json") {
				t = append(t, path.Join(path.Dir(f), "*.tf*"))
			}
		}
		for _, x := range settings[dir].ExtraDependencies {
			t = append(t, path.Join(dir, x))
		}
		triggers[dir] = t
	}

	var projects []valid.Project
	for _, dir := range dirs {
		s := settings[dir]
		if s.Skip {
			continue
		}
		deps := closure(byDir, dir)
		patterns := slices.Clone(triggers[dir])
		for _, dep := range deps {
			patterns = append(patterns, triggers[dep]...)
		}
		// Files of a unit nested in this one belong to that unit, unless
		// this one depends on it.
		for _, other := range dirs {
			if other != dir && isUnder(other, dir) && !slices.Contains(deps, other) {
				patterns = append(patterns, "!"+path.Join(other, "**"))
			}
		}
		whenModified, err := relativeTo(dir, dedupe(patterns))
		if err != nil {
			return nil, err
		}
		p := valid.Project{
			Dir:                 dir,
			Workspace:           "default",
			Autoplan:            valid.Autoplan{Enabled: s.autoplan(), WhenModified: whenModified},
			ExecutionOrderGroup: depth(byDir, dir, map[string]int{}, map[string]bool{}),
			TerraformVersion:    s.TerraformVersion,
		}
		projects = append(projects, p)
	}
	return projects, nil
}

// closure returns every unit dir depends on, directly or not, sorted.
func closure(byDir map[string]Unit, dir string) []string {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(d string) {
		for _, dep := range byDir[d].Dependencies {
			dep = path.Clean(dep)
			if dep == dir || seen[dep] {
				continue
			}
			seen[dep] = true
			walk(dep)
		}
	}
	walk(dir)
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// depth is the length of the longest dependency chain below dir, so that
// dependencies get a lower execution order group. Cycles count once.
func depth(byDir map[string]Unit, dir string, memo map[string]int, visiting map[string]bool) int {
	if v, ok := memo[dir]; ok {
		return v
	}
	if visiting[dir] {
		return 0
	}
	visiting[dir] = true
	best := 0
	for _, dep := range byDir[dir].Dependencies {
		dep = path.Clean(dep)
		if _, known := byDir[dep]; !known {
			continue
		}
		best = max(best, depth(byDir, dep, memo, visiting)+1)
	}
	visiting[dir] = false
	memo[dir] = best
	return best
}

func isUnder(child, parent string) bool {
	return parent == "." || strings.HasPrefix(child, parent+"/")
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// relativeTo rewrites repo-relative patterns relative to dir, the form
// when_modified uses. Patterns may not leave the repository.
func relativeTo(dir string, patterns []string) ([]string, error) {
	depth := 0
	if dir != "." {
		depth = strings.Count(dir, "/") + 1
	}
	up := strings.Repeat("../", depth)
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		neg := strings.HasPrefix(p, "!")
		p = strings.TrimPrefix(p, "!")
		if p == ".." || strings.HasPrefix(p, "../") || path.IsAbs(p) {
			return nil, fmt.Errorf("unit %s: dependency %q is outside the repository", dir, p)
		}
		rel := p
		if dir != "." {
			if isUnder(p, dir) {
				rel = strings.TrimPrefix(p, dir+"/")
			} else {
				rel = up + p
			}
		}
		if neg {
			rel = "!" + rel
		}
		out = append(out, rel)
	}
	return out, nil
}

// Settings are read from atlantis_* locals in a unit's terragrunt.hcl. The
// names match terragrunt-atlantis-config. Values must be literals.
type Settings struct {
	// Skip leaves the unit out (atlantis_skip).
	Skip bool
	// Autoplan disables autoplan when false (atlantis_autoplan).
	Autoplan *bool
	// ExtraDependencies are more paths, relative to the unit, whose changes
	// autoplan it (extra_atlantis_dependencies). Globs are allowed.
	ExtraDependencies []string
	// TerraformVersion pins the Terraform version (atlantis_terraform_version).
	TerraformVersion *version.Version
}

func (s Settings) autoplan() bool {
	return s.Autoplan == nil || *s.Autoplan
}
