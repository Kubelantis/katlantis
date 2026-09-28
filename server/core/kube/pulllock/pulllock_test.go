package pulllock_test

import (
	"sync"
	"testing"
	"time"

	"github.com/runatlantis/atlantis/server/core/kube/kubetest"
	"github.com/runatlantis/atlantis/server/core/kube/pulllock"
	"github.com/runatlantis/atlantis/server/events"
	"github.com/runatlantis/atlantis/server/events/command"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

func replicas(t *testing.T, n int) []*pulllock.Locker {
	c, ns := kubetest.Client(t)
	var out []*pulllock.Locker
	for i := range n {
		out = append(out, pulllock.New(pulllock.Config{
			Local:     events.NewDefaultWorkingDirLocker(),
			Client:    c,
			Namespace: ns,
			Identity:  string(rune('a' + i)),
			Duration:  3 * time.Second,
			Logger:    logging.NewNoopLogger(t),
		}))
	}
	return out
}

func TestPullLockIsExclusiveAcrossReplicas(t *testing.T) {
	r := replicas(t, 2)
	unlock, err := r[0].TryLockPull("org/repo", 1, command.Apply, events.WorkingDirLockMetadata{})
	Ok(t, err)

	_, err = r[1].TryLockPull("org/repo", 1, command.Plan, events.WorkingDirLockMetadata{})
	Assert(t, err != nil, "second replica must not lock the same pull")
	ErrContains(t, "another Atlantis replica", err)

	// A different pull is independent.
	unlock2, err := r[1].TryLockPull("org/repo", 2, command.Plan, events.WorkingDirLockMetadata{})
	Ok(t, err)
	unlock2()

	unlock()
	unlock, err = r[1].TryLockPull("org/repo", 1, command.Plan, events.WorkingDirLockMetadata{})
	Ok(t, err)
	unlock()
}

func TestPullLockIsReentrantWithinReplica(t *testing.T) {
	r := replicas(t, 2)
	var unlocks []func()
	for _, dir := range []string{"a", "b", "c"} {
		u, err := r[0].TryLock("org/repo", 1, "default", dir, "", command.Plan, events.WorkingDirLockMetadata{})
		Ok(t, err)
		unlocks = append(unlocks, u)
	}
	unlocks[0]()
	unlocks[1]()
	_, err := r[1].TryLock("org/repo", 1, "default", "a", "", command.Plan, events.WorkingDirLockMetadata{})
	Assert(t, err != nil, "lease must be held while any project lock is held")
	unlocks[2]()
	u, err := r[1].TryLock("org/repo", 1, "default", "a", "", command.Plan, events.WorkingDirLockMetadata{})
	Ok(t, err)
	u()
}

func TestPullLockSurvivesPastDurationWhileRenewed(t *testing.T) {
	r := replicas(t, 2)
	unlock, err := r[0].TryLockPull("org/repo", 1, command.Apply, events.WorkingDirLockMetadata{})
	Ok(t, err)
	time.Sleep(5 * time.Second)
	_, err = r[1].TryLockPull("org/repo", 1, command.Plan, events.WorkingDirLockMetadata{})
	Assert(t, err != nil, "renewed lease must not expire")
	unlock()
}

func TestStaleReleaseDoesNotDropNewAcquisition(t *testing.T) {
	r := replicas(t, 2)
	stale, err := r[0].TryLock("org/repo", 1, "default", "a", "", command.Plan, events.WorkingDirLockMetadata{})
	Ok(t, err)
	r[0].UnlockByPull("org/repo", 1)
	fresh, err := r[0].TryLock("org/repo", 1, "default", "b", "", command.Plan, events.WorkingDirLockMetadata{})
	Ok(t, err)
	stale()
	_, err = r[1].TryLockPull("org/repo", 1, command.Plan, events.WorkingDirLockMetadata{})
	Assert(t, err != nil, "fresh acquisition must still hold the lease")
	fresh()
}

func TestConcurrentReplicasOneWinner(t *testing.T) {
	r := replicas(t, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for _, l := range r {
		wg.Go(func() {
			if _, err := l.TryLockPull("org/repo", 9, command.Apply, events.WorkingDirLockMetadata{}); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	Equals(t, 1, wins)
}
