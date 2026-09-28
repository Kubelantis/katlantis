package janitor_test

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/runatlantis/atlantis/server/core/kube/janitor"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

type fixture struct {
	repos, logs string
	removed     []string
	open        map[string]bool
	busy        map[string]bool
	apiErr      error
}

func key(repo string, n int) string { return repo + "#" + strconv.Itoa(n) }

func (f *fixture) clone(t *testing.T, repo string, n int, age time.Duration) {
	ws := filepath.Join(f.repos, repo, strconv.Itoa(n), "default")
	Ok(t, os.MkdirAll(filepath.Join(ws, ".git"), 0o700))
	Ok(t, os.WriteFile(filepath.Join(ws, ".git", "HEAD"), []byte("ref"), 0o600))
	old := time.Now().Add(-age)
	for _, p := range []string{filepath.Join(ws, ".git", "HEAD"), filepath.Join(ws, ".git"), ws, filepath.Dir(ws)} {
		Ok(t, os.Chtimes(p, old, old))
	}
}

func (f *fixture) log(t *testing.T, repo string, n int, age time.Duration) {
	dir := filepath.Join(f.logs, url.PathEscape(repo), strconv.Itoa(n))
	Ok(t, os.MkdirAll(dir, 0o700))
	Ok(t, os.WriteFile(filepath.Join(dir, "job.log"), []byte("x"), 0o600))
	old := time.Now().Add(-age)
	Ok(t, os.Chtimes(filepath.Join(dir, "job.log"), old, old))
	Ok(t, os.Chtimes(dir, old, old))
}

func (f *fixture) janitor(t *testing.T) *janitor.Janitor {
	return janitor.New(janitor.Config{
		ReposDir: f.repos, LogDir: f.logs, Logger: logging.NewNoopLogger(t),
		PullExists: func(repo string, n int) (bool, error) {
			return f.open[key(repo, n)], f.apiErr
		},
		Busy: func(repo string, n int) bool { return f.busy[key(repo, n)] },
		RemoveClones: func(repo string, n int) error {
			f.removed = append(f.removed, "clone:"+key(repo, n))
			return os.RemoveAll(filepath.Join(f.repos, repo, strconv.Itoa(n)))
		},
		RemoveLogs: func(repo string, n int) error {
			f.removed = append(f.removed, "logs:"+key(repo, n))
			return nil
		},
	})
}

func newFixture(t *testing.T) *fixture {
	return &fixture{repos: t.TempDir(), logs: t.TempDir(), open: map[string]bool{}, busy: map[string]bool{}}
}

func TestSweepRemovesOnlyOldClosedIdlePulls(t *testing.T) {
	f := newFixture(t)
	day2 := 48 * time.Hour
	f.clone(t, "org/closed", 1, day2)
	f.log(t, "org/closed", 1, day2)
	f.log(t, "org/logs-only", 4, day2)
	f.clone(t, "org/open", 2, day2)
	f.open[key("org/open", 2)] = true
	f.clone(t, "org/recent", 3, time.Hour)
	f.clone(t, "org/busy", 5, day2)
	f.busy[key("org/busy", 5)] = true

	Equals(t, 2, f.janitor(t).Sweep())
	slices.Sort(f.removed)
	Equals(t, []string{"clone:org/closed#1", "clone:org/logs-only#4", "logs:org/closed#1", "logs:org/logs-only#4"}, f.removed)
	_, err := os.Stat(filepath.Join(f.repos, "org/open/2"))
	Ok(t, err)
}

// GitLab subgroups allow numeric repo names: group/123 is a repo whose pull 5
// is cloned at group/123/5. It must not be read as pull 123 of repo "group".
func TestNumericRepoNamesAreNotMistakenForPulls(t *testing.T) {
	f := newFixture(t)
	f.clone(t, "group/123", 5, 48*time.Hour)
	f.janitor(t).Sweep()
	Equals(t, []string{"clone:group/123#5", "logs:group/123#5"}, f.removed)
}

func TestAPIErrorsKeepEverything(t *testing.T) {
	f := newFixture(t)
	f.clone(t, "org/closed", 1, 48*time.Hour)
	f.apiErr = errors.New("apiserver unavailable")
	Equals(t, 0, f.janitor(t).Sweep())
	Equals(t, 0, len(f.removed))
}
