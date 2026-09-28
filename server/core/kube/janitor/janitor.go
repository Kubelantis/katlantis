// Package janitor removes a replica's local leftovers of closed pull
// requests: clones and job log files.
//
// Normally a closed pull is cleaned on every replica when it closes (the
// owner runs the full cleanup and broadcasts the local part). A replica that
// was down at that moment keeps its copies on its volume; the janitor finds
// pulls that no longer have a PullStatus and removes them.
package janitor

import (
	"context"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/runatlantis/atlantis/server/logging"
)

// Config configures a Janitor.
type Config struct {
	// ReposDir is <data dir>/repos, laid out as <repo full name>/<pull>/<workspace>.
	ReposDir string
	// LogDir is the job log root, laid out as <escaped repo>/<pull>/<job>.log.
	LogDir string
	// PullExists reports whether the pull still has shared state.
	PullExists func(repoFullName string, pullNum int) (bool, error)
	// Busy reports whether this replica is running a command for the pull.
	Busy func(repoFullName string, pullNum int) bool
	// RemoveClones and RemoveLogs delete the pull's local copies.
	RemoveClones func(repoFullName string, pullNum int) error
	RemoveLogs   func(repoFullName string, pullNum int) error
	// MinAge protects recently touched pulls, e.g. a first plan that has not
	// written its PullStatus yet. Defaults to 24h.
	MinAge time.Duration
	// Interval between sweeps. Defaults to 1h.
	Interval time.Duration
	Logger   logging.SimpleLogging
	Now      func() time.Time
}

// Janitor periodically removes local leftovers of closed pulls.
type Janitor struct{ cfg Config }

// New returns a Janitor.
func New(cfg Config) *Janitor {
	if cfg.MinAge == 0 {
		cfg.MinAge = 24 * time.Hour
	}
	if cfg.Interval == 0 {
		cfg.Interval = time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Janitor{cfg: cfg}
}

// Run sweeps every Interval until ctx is done.
func (j *Janitor) Run(ctx context.Context) {
	t := time.NewTicker(j.cfg.Interval)
	defer t.Stop()
	for {
		j.Sweep()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type pullRef struct {
	repoFullName string
	num          int
	// newest is the latest modification time seen for the pull's files.
	newest time.Time
}

// Sweep runs one pass and returns the number of pulls cleaned.
func (j *Janitor) Sweep() int {
	cleaned := 0
	seen := map[string]*pullRef{}
	add := func(ref pullRef) {
		key := ref.repoFullName + "#" + strconv.Itoa(ref.num)
		if cur, ok := seen[key]; ok {
			if ref.newest.After(cur.newest) {
				cur.newest = ref.newest
			}
			return
		}
		seen[key] = &ref
	}
	for _, ref := range j.clonePulls() {
		add(ref)
	}
	for _, ref := range j.logPulls() {
		add(ref)
	}
	for _, ref := range seen {
		if j.cfg.Now().Sub(ref.newest) < j.cfg.MinAge {
			continue
		}
		if j.cfg.Busy != nil && j.cfg.Busy(ref.repoFullName, ref.num) {
			continue
		}
		exists, err := j.cfg.PullExists(ref.repoFullName, ref.num)
		if err != nil {
			j.cfg.Logger.Warn("janitor: checking %s#%d: %s", ref.repoFullName, ref.num, err)
			continue
		}
		if exists {
			continue
		}
		j.cfg.Logger.Info("janitor: removing local files of closed pull %s#%d", ref.repoFullName, ref.num)
		if err := j.cfg.RemoveClones(ref.repoFullName, ref.num); err != nil {
			j.cfg.Logger.Warn("janitor: removing clones of %s#%d: %s", ref.repoFullName, ref.num, err)
		}
		if err := j.cfg.RemoveLogs(ref.repoFullName, ref.num); err != nil {
			j.cfg.Logger.Warn("janitor: removing job logs of %s#%d: %s", ref.repoFullName, ref.num, err)
		}
		cleaned++
	}
	return cleaned
}

// clonePulls finds <repo>/<pull> directories under ReposDir. A directory is
// a pull only if it is numeric and every child is a git checkout, so a repo
// whose name is a number (possible under GitLab groups) is never mistaken
// for a pull.
func (j *Janitor) clonePulls() []pullRef {
	var refs []pullRef
	if j.cfg.ReposDir == "" {
		return nil
	}
	_ = filepath.WalkDir(j.cfg.ReposDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == j.cfg.ReposDir {
			return nil
		}
		num, convErr := strconv.Atoi(d.Name())
		if convErr != nil || num <= 0 {
			return nil
		}
		newest, ok := checkouts(path)
		if !ok {
			return nil
		}
		repo, relErr := filepath.Rel(j.cfg.ReposDir, filepath.Dir(path))
		if relErr != nil || repo == "." {
			return nil
		}
		refs = append(refs, pullRef{repoFullName: filepath.ToSlash(repo), num: num, newest: newest})
		return filepath.SkipDir
	})
	return refs
}

// checkouts reports whether every child of dir is a git checkout, and the
// newest modification time among dir, the checkouts and their .git dirs.
func checkouts(dir string) (time.Time, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return time.Time{}, false
	}
	newest := modTime(dir)
	for _, e := range entries {
		if !e.IsDir() {
			return time.Time{}, false
		}
		ws := filepath.Join(dir, e.Name())
		git := filepath.Join(ws, ".git")
		if _, err := os.Stat(git); err != nil {
			return time.Time{}, false
		}
		for _, p := range []string{ws, git, filepath.Join(git, "HEAD"), filepath.Join(git, "index")} {
			if t := modTime(p); t.After(newest) {
				newest = t
			}
		}
	}
	return newest, true
}

// logPulls finds <escaped repo>/<pull> directories under LogDir.
func (j *Janitor) logPulls() []pullRef {
	if j.cfg.LogDir == "" {
		return nil
	}
	repos, err := os.ReadDir(j.cfg.LogDir)
	if err != nil {
		return nil
	}
	var refs []pullRef
	for _, r := range repos {
		repo, err := url.PathUnescape(r.Name())
		if err != nil || !r.IsDir() {
			continue
		}
		pulls, _ := os.ReadDir(filepath.Join(j.cfg.LogDir, r.Name()))
		for _, p := range pulls {
			num, err := strconv.Atoi(p.Name())
			if err != nil || !p.IsDir() {
				continue
			}
			dir := filepath.Join(j.cfg.LogDir, r.Name(), p.Name())
			newest := modTime(dir)
			files, _ := os.ReadDir(dir)
			for _, f := range files {
				if t := modTime(filepath.Join(dir, f.Name())); t.After(newest) {
					newest = t
				}
			}
			refs = append(refs, pullRef{repoFullName: repo, num: num, newest: newest})
		}
	}
	return refs
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
