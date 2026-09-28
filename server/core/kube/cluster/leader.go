package cluster

import (
	"context"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/runatlantis/atlantis/server/logging"
)

// LeaderConfig configures leader election.
type LeaderConfig struct {
	RestConfig    *rest.Config
	Namespace     string
	Identity      string
	LeaseName     string
	LeaseDuration time.Duration
	RenewDeadline time.Duration
	RetryPeriod   time.Duration
	Logger        logging.SimpleLogging
	// Run is started when this replica becomes leader; its context is
	// cancelled when leadership is lost.
	Run func(ctx context.Context)
	// OnChange is called with the new leader identity.
	OnChange func(isLeader bool, leader string)
}

// Leader runs client-go leader election on a Lease.
type Leader struct {
	cfg      LeaderConfig
	isLeader atomic.Bool
	leader   atomic.Value
}

// NewLeader returns a Leader. Call Run to start it.
func NewLeader(cfg LeaderConfig) *Leader {
	if cfg.LeaseName == "" {
		cfg.LeaseName = "atlantis-leader"
	}
	if cfg.LeaseDuration == 0 {
		cfg.LeaseDuration = 15 * time.Second
	}
	if cfg.RenewDeadline == 0 {
		cfg.RenewDeadline = 10 * time.Second
	}
	if cfg.RetryPeriod == 0 {
		cfg.RetryPeriod = 2 * time.Second
	}
	l := &Leader{cfg: cfg}
	l.leader.Store("")
	return l
}

// IsLeader reports whether this replica currently leads.
func (l *Leader) IsLeader() bool { return l.isLeader.Load() }

// Current returns the identity of the current leader, if known.
func (l *Leader) Current() string { return l.leader.Load().(string) }

// Run participates in elections until ctx is done. Leadership is released on
// exit so a successor is elected without waiting for the lease to expire.
func (l *Leader) Run(ctx context.Context) error {
	cs, err := kubernetes.NewForConfig(l.cfg.RestConfig)
	if err != nil {
		return err
	}
	lock := &resourcelock.LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Name: l.cfg.LeaseName, Namespace: l.cfg.Namespace},
		Client:     cs.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: l.cfg.Identity},
	}
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:            lock,
		LeaseDuration:   l.cfg.LeaseDuration,
		RenewDeadline:   l.cfg.RenewDeadline,
		RetryPeriod:     l.cfg.RetryPeriod,
		ReleaseOnCancel: true,
		Name:            l.cfg.LeaseName,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				l.isLeader.Store(true)
				l.cfg.Logger.Info("became cluster leader")
				if l.cfg.Run != nil {
					l.cfg.Run(ctx)
				}
			},
			OnStoppedLeading: func() {
				l.isLeader.Store(false)
				l.cfg.Logger.Info("stopped being cluster leader")
			},
			OnNewLeader: func(identity string) {
				l.leader.Store(identity)
				if l.cfg.OnChange != nil {
					l.cfg.OnChange(identity == l.cfg.Identity, identity)
				}
			},
		},
	})
	if err != nil {
		return err
	}
	// RunOrDie returns when leadership is lost; keep campaigning until ctx ends.
	for ctx.Err() == nil {
		elector.Run(ctx)
	}
	return nil
}
