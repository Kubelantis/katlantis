package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
	tally "github.com/uber-go/tally/v4"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/runatlantis/atlantis/server/clusterrpc"
	"github.com/runatlantis/atlantis/server/core/kube"
	"github.com/runatlantis/atlantis/server/core/kube/cluster"
	"github.com/runatlantis/atlantis/server/core/kube/kubedb"
	"github.com/runatlantis/atlantis/server/core/kube/pulllock"
	"github.com/runatlantis/atlantis/server/events"
	"github.com/runatlantis/atlantis/server/events/models"
	"github.com/runatlantis/atlantis/server/events/vcs"
	"github.com/runatlantis/atlantis/server/jobs"
	"github.com/runatlantis/atlantis/server/logging"
)

// kubeRuntime holds everything Atlantis needs to run as one of several
// replicas on Kubernetes. It is nil unless --locking-db-type=kubernetes.
type kubeRuntime struct {
	client     client.Client
	restConfig *rest.Config
	namespace  string
	identity   string
	token      string
	port       int

	membership  *cluster.Membership
	leader      *cluster.Leader
	housekeeper *cluster.Housekeeper
	rpcClient   *clusterrpc.Client
	logger      logging.SimpleLogging
	scope       tally.Scope

	cancel context.CancelFunc
	wg     sync.WaitGroup
	srv    *http.Server

	// started is set once the cluster listener, membership and leader
	// election are running.
	started atomic.Bool
}

func newKubeRuntime(userConfig UserConfig, logger logging.SimpleLogging, scope tally.Scope) (*kubeRuntime, error) {
	ns, err := kube.ResolveNamespace(userConfig.KubernetesNamespace)
	if err != nil {
		return nil, err
	}
	identity, err := kube.ResolveIdentity(userConfig.KubernetesIdentity)
	if err != nil {
		return nil, fmt.Errorf("resolving replica identity: %w", err)
	}
	c, restCfg, err := kube.NewClient(50, 100)
	if err != nil {
		return nil, err
	}
	address := userConfig.ClusterAddress
	if address == "" {
		ip := os.Getenv("POD_IP")
		if ip == "" {
			return nil, errors.New("--cluster-address is not set and $POD_IP is empty; set POD_IP from the downward API")
		}
		address = "http://" + net.JoinHostPort(ip, strconv.Itoa(userConfig.ClusterPort))
	}

	logger = logger.With("replica", identity)
	scope = scope.SubScope("cluster")
	k := &kubeRuntime{
		client: c, restConfig: restCfg, namespace: ns, identity: identity,
		token: userConfig.ClusterToken, port: userConfig.ClusterPort,
		logger: logger, scope: scope,
		rpcClient: &clusterrpc.Client{
			Token:      userConfig.ClusterToken,
			HTTPClient: &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
		},
	}
	membersGauge := scope.Gauge("members")
	apiHealthy := scope.Gauge("api_healthy")
	k.membership = cluster.NewMembership(cluster.MembershipConfig{
		Client: c, Namespace: ns, Identity: identity, Address: address, Logger: logger,
		OnChange: func(members []cluster.Member) {
			membersGauge.Update(float64(len(members)))
			logger.Info("cluster membership changed: %d live replicas", len(members))
		},
		OnSync: func(err error) {
			if err != nil {
				apiHealthy.Update(0)
			} else {
				apiHealthy.Update(1)
			}
		},
	})
	k.housekeeper = &cluster.Housekeeper{Client: c, Namespace: ns, Logger: logger}
	leaderGauge := scope.Gauge("leader")
	k.leader = cluster.NewLeader(cluster.LeaderConfig{
		RestConfig: restCfg, Namespace: ns, Identity: identity, Logger: logger,
		Run: k.housekeeper.Run,
		OnChange: func(isLeader bool, leader string) {
			if isLeader {
				leaderGauge.Update(1)
			} else {
				leaderGauge.Update(0)
			}
			logger.Info("cluster leader is %s", leader)
		},
	})
	logger.Info("running on Kubernetes in namespace %s as %s (cluster address %s)", ns, identity, address)
	return k, nil
}

func (k *kubeRuntime) database() (*kubedb.KubeDB, error) {
	return kubedb.New(kubedb.Config{Client: k.client, Namespace: k.namespace, Identity: k.identity})
}

// workingDirLocker returns the Lease-backed locker. onLost is called when a
// pull's lease is lost to another replica.
func (k *kubeRuntime) workingDirLocker(local events.WorkingDirLocker, onLost func(repoFullName string, pullNum int)) events.WorkingDirLocker {
	lost := k.scope.Counter("pull_lock_lost")
	return pulllock.New(pulllock.Config{
		Local: local, Client: k.client, Namespace: k.namespace, Identity: k.identity, Logger: k.logger,
		OnLost: func(repoFullName string, pullNum int) {
			lost.Inc(1)
			onLost(repoFullName, pullNum)
		},
	})
}

// router wraps the local command runner and pull cleaner so work is sent to
// the owning replica.
func (k *kubeRuntime) router(local events.ContextCommandRunner, localClean events.PullCleaner, vcsClient vcs.Client, plans clusterrpc.PlanHolders, sharedPlans bool, planHolderWait time.Duration) *clusterrpc.Router {
	return &clusterrpc.Router{
		Plans: plans, SharedPlans: sharedPlans, PlanHolderWait: planHolderWait,
		Local: local, LocalClean: localClean, Owners: k.membership,
		Client: k.rpcClient, Logger: k.logger, Scope: k.scope.SubScope("routing"),
		Notify: func(repo models.Repo, pullNum int, msg string) error {
			return vcsClient.CreateComment(k.logger, repo, pullNum, msg, "")
		},
	}
}

func (k *kubeRuntime) jobProxy(output jobs.ProjectCommandOutputHandler) *clusterrpc.JobProxy {
	return &clusterrpc.JobProxy{Jobs: output, Owners: k.membership, Client: k.rpcClient, Logger: k.logger}
}

// start begins membership, leader election, and the internal listener.
func (k *kubeRuntime) start(s *Server, local events.ContextCommandRunner, localClean events.PullCleaner) error {
	jobsRouter := mux.NewRouter()
	jobsRouter.HandleFunc("/jobs/{job-id}", s.JobsController.GetProjectJobs).Methods("GET")
	jobsRouter.HandleFunc("/jobs/{job-id}/ws", s.JobsController.GetProjectJobsWS).Methods("GET")
	internal := &clusterrpc.Server{
		Token: k.token, Local: local, LocalClean: localClean,
		Jobs: s.ProjectCmdOutputHandler, JobsHandler: jobsRouter,
		Logger: k.logger, Drain: s.Drainer,
	}
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", k.port))
	if err != nil {
		return fmt.Errorf("listening on cluster port: %w", err)
	}
	k.srv = &http.Server{Handler: otelhttp.NewHandler(internal.Handler(), "atlantis.internal"), ReadHeaderTimeout: 10 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	k.cancel = cancel
	k.wg.Go(func() {
		if err := k.srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			k.logger.Err("cluster listener: %s", err)
		}
	})
	k.wg.Go(func() { k.membership.Run(ctx) })
	k.wg.Go(func() {
		if err := k.leader.Run(ctx); err != nil {
			k.logger.Err("leader election: %s", err)
		}
	})
	k.started.Store(true)
	return nil
}

// ready reports whether this replica should receive traffic. It depends only
// on local state: the Kubernetes API is shared by every replica, so gating
// readiness on it would take all replicas out of the Service at once during
// an API server outage. Commands that need the API fail individually instead,
// and atlantis_cluster_api_healthy reports API reachability.
func (k *kubeRuntime) ready(draining bool) (bool, string) {
	if draining {
		return false, "draining"
	}
	if !k.started.Load() {
		return false, "starting"
	}
	return true, ""
}

// drain stops this replica from owning pulls while its running jobs finish.
func (k *kubeRuntime) drain() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	k.membership.SetDraining(ctx)
}

// stop releases the member and leader Leases and closes the listener.
func (k *kubeRuntime) stop() {
	if k.cancel == nil {
		return
	}
	k.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = k.srv.Shutdown(ctx)
	k.wg.Wait()
}
