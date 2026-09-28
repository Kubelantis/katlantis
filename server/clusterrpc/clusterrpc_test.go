package clusterrpc_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	tally "github.com/uber-go/tally/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/runatlantis/atlantis/server/clusterrpc"
	"github.com/runatlantis/atlantis/server/core/kube/cluster"
	"github.com/runatlantis/atlantis/server/events"
	"github.com/runatlantis/atlantis/server/events/command"
	"github.com/runatlantis/atlantis/server/events/models"
	"github.com/runatlantis/atlantis/server/logging"
	. "github.com/runatlantis/atlantis/testing"
)

const token = "s3cret"

type call struct {
	kind    string
	pullNum int
	cmd     *events.CommentCommand
	traceID trace.TraceID
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []call
	done  chan struct{}
}

func newFakeRunner() *fakeRunner { return &fakeRunner{done: make(chan struct{}, 10)} }

func (f *fakeRunner) record(ctx context.Context, c call) {
	c.traceID = trace.SpanContextFromContext(ctx).TraceID()
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	f.done <- struct{}{}
}
func (f *fakeRunner) RunCommentCommand(models.Repo, *models.Repo, *models.PullRequest, models.User, int, *events.CommentCommand) {
	panic("use context variant")
}
func (f *fakeRunner) RunAutoplanCommand(models.Repo, models.Repo, models.PullRequest, models.User) {
	panic("use context variant")
}
func (f *fakeRunner) RunCommentCommandWithContext(ctx context.Context, _ models.Repo, _ *models.Repo, _ *models.PullRequest, _ models.User, pullNum int, cmd *events.CommentCommand) {
	f.record(ctx, call{kind: "comment", pullNum: pullNum, cmd: cmd})
}
func (f *fakeRunner) RunAutoplanCommandWithContext(ctx context.Context, _ models.Repo, _ models.Repo, pull models.PullRequest, _ models.User) {
	f.record(ctx, call{kind: "autoplan", pullNum: pull.Num})
}
func (f *fakeRunner) CleanUpPull(logging.SimpleLogging, models.Repo, models.PullRequest) error {
	f.record(context.Background(), call{kind: "cleanup"})
	return nil
}
func (f *fakeRunner) wait(t *testing.T) {
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for command")
	}
}
func (f *fakeRunner) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }

type staticOwners struct {
	self    string
	owner   cluster.Member
	members []cluster.Member
}

func (s staticOwners) Identity() string                    { return s.self }
func (s staticOwners) Owner(string) (cluster.Member, bool) { return s.owner, s.owner.Identity != "" }
func (s staticOwners) Members() []cluster.Member           { return s.members }

type jobs map[string]bool

func (j jobs) IsKeyExists(k string) bool { return j[k] }

type drain struct{ shutting bool }

func (d drain) GetStatus() events.DrainStatus { return events.DrainStatus{ShuttingDown: d.shutting} }

func ownerServer(t *testing.T, runner *fakeRunner, d drain, jobHandler http.Handler) *httptest.Server {
	s := &clusterrpc.Server{Token: token, Local: runner, LocalClean: runner, Jobs: jobs{"job-b": true}, JobsHandler: jobHandler, Logger: logging.NewNoopLogger(t), Drain: d}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func router(t *testing.T, local *fakeRunner, owner cluster.Member) *clusterrpc.Router {
	return &clusterrpc.Router{
		Local: local, LocalClean: local,
		Owners: staticOwners{self: "a", owner: owner},
		Client: &clusterrpc.Client{Token: token, HTTPClient: http.DefaultClient},
		Logger: logging.NewNoopLogger(t), Scope: tally.NoopScope, CallTimeout: 2 * time.Second,
	}
}

var repo = models.Repo{FullName: "org/repo", VCSHost: models.VCSHost{Hostname: "github.com"}}

func TestForwardsToOwnerWithTrace(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	ctx, span := tp.Tracer("t").Start(context.Background(), "webhook")
	defer span.End()

	remote, local := newFakeRunner(), newFakeRunner()
	srv := ownerServer(t, remote, drain{}, http.NotFoundHandler())
	r := router(t, local, cluster.Member{Identity: "b", Address: srv.URL})

	r.RunCommentCommandWithContext(ctx, repo, nil, nil, models.User{Username: "u"}, 5, &events.CommentCommand{Name: command.Plan, Workspace: "ws"})
	remote.wait(t)
	Equals(t, 0, local.count())
	Equals(t, 5, remote.calls[0].pullNum)
	Equals(t, "ws", remote.calls[0].cmd.Workspace)
	Equals(t, span.SpanContext().TraceID(), remote.calls[0].traceID)

	r.RunAutoplanCommandWithContext(ctx, repo, repo, models.PullRequest{Num: 6}, models.User{})
	remote.wait(t)
	Equals(t, "autoplan", remote.calls[1].kind)

	Ok(t, r.CleanUpPull(logging.NewNoopLogger(t), repo, models.PullRequest{Num: 6}))
	remote.wait(t)
	Equals(t, "cleanup", remote.calls[2].kind)
}

func TestRunsLocallyWhenSelfOwnedOrOwnerUnavailable(t *testing.T) {
	remote := newFakeRunner()
	draining := ownerServer(t, remote, drain{shutting: true}, http.NotFoundHandler())
	for name, owner := range map[string]cluster.Member{
		"self":        {Identity: "a", Address: "http://unused"},
		"no owner":    {},
		"unreachable": {Identity: "b", Address: "http://127.0.0.1:1"},
		"draining":    {Identity: "b", Address: draining.URL},
	} {
		t.Run(name, func(t *testing.T) {
			local := newFakeRunner()
			router(t, local, owner).RunCommentCommandWithContext(context.Background(), repo, nil, nil, models.User{}, 1, &events.CommentCommand{Name: command.Plan})
			local.wait(t)
			Equals(t, 1, local.count())
		})
	}
	Equals(t, 0, remote.count())
}

func TestInternalServerRequiresToken(t *testing.T) {
	remote := newFakeRunner()
	srv := ownerServer(t, remote, drain{}, http.NotFoundHandler())
	for _, auth := range []string{"", "Bearer wrong", token} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/internal/v1/commands", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		Ok(t, err)
		resp.Body.Close()
		Equals(t, http.StatusUnauthorized, resp.StatusCode)
	}
	resp, err := http.Get(srv.URL + "/jobs/job-b")
	Ok(t, err)
	resp.Body.Close()
	Equals(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestJobProxyServesRemoteJob(t *testing.T) {
	jobPage := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Assert(t, r.Header.Get(clusterrpc.ForwardedHeader) != "", "proxied request must be marked")
		_, _ = io.WriteString(w, "output of "+r.URL.Path)
	})
	b := ownerServer(t, newFakeRunner(), drain{}, jobPage)

	owners := staticOwners{self: "a", members: []cluster.Member{{Identity: "a", Address: "http://unused"}, {Identity: "c", Address: "http://127.0.0.1:1"}, {Identity: "b", Address: b.URL}}}
	proxy := &clusterrpc.JobProxy{Jobs: jobs{"job-a": true}, Owners: owners, Client: &clusterrpc.Client{Token: token, HTTPClient: http.DefaultClient}, Logger: logging.NewNoopLogger(t)}
	local := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "local") })
	rt := mux.NewRouter()
	rt.Handle("/jobs/{job-id}", proxy.Middleware(local))
	a := httptest.NewServer(rt)
	defer a.Close()

	get := func(path string) string {
		resp, err := http.Get(a.URL + path)
		Ok(t, err)
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	Equals(t, "local", get("/jobs/job-a"))
	Equals(t, "output of /jobs/job-b", get("/jobs/job-b"))
	Equals(t, "local", get("/jobs/unknown"))
}
