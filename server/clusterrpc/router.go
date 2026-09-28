// Package clusterrpc routes work between Atlantis replicas.
//
// Any replica can receive a webhook. After the webhook is validated and
// parsed, the Router forwards the typed command to the replica that owns the
// pull request (see cluster.Membership.Owner), where it runs as if received
// directly. If the owner cannot be reached the command runs locally: the
// Kubernetes project locks and pull lock keep that safe, at the cost of a
// fresh clone.
//
// The internal listener (Server) is separate from the public port so it can
// stay off ingresses, and every request must carry the shared cluster token.
package clusterrpc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	tally "github.com/uber-go/tally/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/runatlantis/atlantis/server/core/kube"
	"github.com/runatlantis/atlantis/server/core/kube/cluster"
	"github.com/runatlantis/atlantis/server/events"
	"github.com/runatlantis/atlantis/server/events/command"
	"github.com/runatlantis/atlantis/server/events/models"
	"github.com/runatlantis/atlantis/server/logging"
	"github.com/runatlantis/atlantis/server/tracing"
)

const (
	commandsPath  = "/internal/v1/commands"
	cleanupPath   = "/internal/v1/cleanup"
	jobExistsPath = "/internal/v1/jobs/"

	// ForwardedHeader marks proxied requests so they are never proxied again.
	ForwardedHeader = "X-Atlantis-Forwarded-By"
)

// Owners resolves pull ownership. It is implemented by *cluster.Membership.
type Owners interface {
	Identity() string
	Owner(key string) (cluster.Member, bool)
	Members() []cluster.Member
}

type commandKind string

const (
	kindComment  commandKind = "comment"
	kindAutoplan commandKind = "autoplan"
)

// commandRequest is the wire form of a CommandRunner call.
type commandRequest struct {
	Kind          commandKind            `json:"kind"`
	BaseRepo      models.Repo            `json:"baseRepo"`
	HeadRepo      *models.Repo           `json:"headRepo,omitempty"`
	Pull          *models.PullRequest    `json:"pull,omitempty"`
	User          models.User            `json:"user"`
	PullNum       int                    `json:"pullNum"`
	Command       *events.CommentCommand `json:"command,omitempty"`
	ForwardedFrom string                 `json:"forwardedFrom"`
	// RequestID is the same on every retry of one forward, so the owner runs
	// the command at most once.
	RequestID string `json:"requestID"`
}

type cleanupRequest struct {
	Repo models.Repo        `json:"repo"`
	Pull models.PullRequest `json:"pull"`
}

// Client makes authenticated calls to other replicas.
type Client struct {
	Token      string
	HTTPClient *http.Client
}

func (c *Client) post(ctx context.Context, member cluster.Member, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(member.Address, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(ctx, req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() // nolint: errcheck
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &rejectedError{fmt.Errorf("%s returned %s: %s", member.Identity, resp.Status, strings.TrimSpace(string(msg)))}
	}
	return nil
}

// rejectedError means the peer answered and did not accept the request.
type rejectedError struct{ error }

func (e *rejectedError) Unwrap() error { return e.error }

// notDelivered reports whether err proves the peer did not accept the request:
// it answered with a rejection, or no connection was ever established. Any
// other error (a timeout or reset after the request was sent) is ambiguous:
// the peer may already be running the command.
func notDelivered(err error) bool {
	var rejected *rejectedError
	if errors.As(err, &rejected) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

type outcome int

const (
	delivered outcome = iota
	// rejected: the owner certainly did not run it; running locally is safe.
	rejected
	// ambiguous: the owner may be running it.
	ambiguous
)

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *Client) authorize(ctx context.Context, req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
}

// Authorized reports whether r carries the cluster token.
func Authorized(r *http.Request, token string) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// Router is an events.ContextCommandRunner and events.PullCleaner that
// forwards work to the owning replica.
type Router struct {
	Local       events.ContextCommandRunner
	LocalClean  events.PullCleaner
	Owners      Owners
	Client      *Client
	Logger      logging.SimpleLogging
	Scope       tally.Scope
	CallTimeout time.Duration
	// Notify comments on a pull request; optional.
	Notify func(repo models.Repo, pullNum int, msg string) error

	// Plans reports which replica holds a pull's plan files. When set and
	// SharedPlans is false, applies are routed to that replica.
	Plans PlanHolders
	// SharedPlans is true when plans are in a store every replica can read
	// (e.g. S3), so applies can run anywhere.
	SharedPlans bool
	// PlanHolderWait is how long an apply waits for an absent plan holder to
	// come back (a StatefulSet pod restarting keeps its name and volume).
	PlanHolderWait time.Duration
	// PlanHolderPoll is how often membership is checked while waiting.
	PlanHolderPoll time.Duration
}

// PlanHolders is implemented by kubedb.KubeDB.
type PlanHolders interface {
	PlanHolder(repo models.Repo, pullNum int) (string, error)
}

// planHolder decides where an apply must run. local is true when this
// replica holds the plan; ok is true when another live replica does. Both
// false means there is no usable holder and normal ownership applies.
func (r *Router) planHolder(ctx context.Context, repo models.Repo, pullNum int) (holder cluster.Member, local bool, ok bool) {
	if r.Plans == nil || r.SharedPlans {
		return cluster.Member{}, false, false
	}
	id, err := r.Plans.PlanHolder(repo, pullNum)
	if err != nil {
		r.Logger.Warn("looking up plan holder for %s#%d: %s", repo.FullName, pullNum, err)
		return cluster.Member{}, false, false
	}
	if id == "" {
		return cluster.Member{}, false, false
	}
	if id == r.Owners.Identity() {
		return cluster.Member{}, true, false
	}
	find := func() (cluster.Member, bool) {
		for _, m := range r.Owners.Members() {
			if m.Identity == id && !m.Draining && m.Address != "" {
				return m, true
			}
		}
		return cluster.Member{}, false
	}
	if m, found := find(); found {
		return m, false, true
	}
	if r.PlanHolderWait <= 0 {
		return cluster.Member{}, false, false
	}
	poll := r.PlanHolderPoll
	if poll == 0 {
		poll = 2 * time.Second
	}
	r.Logger.Info("plan for %s#%d is on replica %s, which is not available; waiting up to %s for it to return", repo.FullName, pullNum, id, r.PlanHolderWait)
	r.Scope.Counter("plan_holder_wait").Inc(1)
	deadline := time.NewTimer(r.PlanHolderWait)
	defer deadline.Stop()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return cluster.Member{}, false, false
		case <-deadline.C:
			r.Scope.Counter("plan_holder_timeout").Inc(1)
			r.Logger.Warn("plan holder %s for %s#%d did not return within %s; the apply will run elsewhere and may need a new plan", id, repo.FullName, pullNum, r.PlanHolderWait)
			return cluster.Member{}, false, false
		case <-tick.C:
			if m, found := find(); found {
				return m, false, true
			}
		}
	}
}

var (
	_ events.ContextCommandRunner = (*Router)(nil)
	_ events.PullCleaner          = (*Router)(nil)
)

// ownerFor returns the remote owner of a pull, or ok=false to run locally.
func (r *Router) ownerFor(repo models.Repo, pullNum int) (cluster.Member, bool) {
	owner, ok := r.Owners.Owner(kube.PullKey(repo.VCSHost.Hostname, repo.FullName, pullNum))
	if !ok || owner.Identity == r.Owners.Identity() || owner.Address == "" {
		return cluster.Member{}, false
	}
	return owner, true
}

func (r *Router) timeout() time.Duration {
	if r.CallTimeout == 0 {
		return 10 * time.Second
	}
	return r.CallTimeout
}

// forward sends body to owner. Ambiguous failures are retried with the same
// payload (and so the same RequestID), which the owner de-duplicates.
func (r *Router) forward(ctx context.Context, owner cluster.Member, path string, body any, repo string, pullNum int) outcome {
	ctx, span := tracing.Start(ctx, "atlantis.cluster.forward",
		tracing.AttrRepo.String(repo), tracing.AttrPull.Int(pullNum), tracing.AttrReplica.String(owner.Identity))
	var err error
	backoff := 250 * time.Millisecond
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			backoff *= 4
		}
		cctx, cancel := context.WithTimeout(ctx, r.timeout())
		err = r.Client.post(cctx, owner, path, body)
		cancel()
		if err == nil || notDelivered(err) || ctx.Err() != nil {
			break
		}
	}
	tracing.End(span, err)
	switch {
	case err == nil:
		r.Scope.Counter("forward_success").Inc(1)
		r.Logger.Debug("forwarded %s#%d to owner %s", repo, pullNum, owner.Identity)
		return delivered
	case notDelivered(err):
		r.Scope.Counter("forward_error").Inc(1)
		r.Logger.Warn("owner %s did not accept %s#%d, running locally: %s", owner.Identity, repo, pullNum, err)
		return rejected
	default:
		r.Scope.Counter("forward_ambiguous").Inc(1)
		r.Logger.Warn("could not confirm owner %s received %s#%d: %s", owner.Identity, repo, pullNum, err)
		return ambiguous
	}
}

// isLive reports whether identity is currently a live member.
func (r *Router) isLive(identity string) bool {
	for _, m := range r.Owners.Members() {
		if m.Identity == identity {
			return true
		}
	}
	return false
}

// dispatch forwards req to owner, or runs it locally when forwarding proves
// the owner did not take it. When the outcome is ambiguous and the owner is
// still alive, the command is not run again here: running it twice is worse
// than asking the user to re-run it. If the owner has died its work died with
// it, so running locally is safe.
func (r *Router) dispatch(ctx context.Context, owner cluster.Member, req commandRequest, runLocal func()) {
	req.RequestID = newRequestID()
	switch r.forward(ctx, owner, commandsPath, req, req.BaseRepo.FullName, req.PullNum) {
	case delivered:
		return
	case ambiguous:
		if r.isLive(owner.Identity) {
			r.notify(req.BaseRepo, req.PullNum, fmt.Sprintf(ambiguousComment, owner.Identity))
			return
		}
	}
	r.Scope.Counter("local").Inc(1)
	runLocal()
}

const ambiguousComment = "**Atlantis could not confirm that replica `%s` received this command.** " +
	"To avoid running it twice it was not retried elsewhere. If no result appears shortly, run the command again."

func (r *Router) notify(repo models.Repo, pullNum int, msg string) {
	if r.Notify == nil {
		return
	}
	if err := r.Notify(repo, pullNum, msg); err != nil {
		r.Logger.Err("commenting on %s#%d: %s", repo.FullName, pullNum, err)
	}
}

// RunCommentCommand implements events.CommandRunner.
func (r *Router) RunCommentCommand(baseRepo models.Repo, maybeHeadRepo *models.Repo, maybePull *models.PullRequest, user models.User, pullNum int, cmd *events.CommentCommand) {
	r.RunCommentCommandWithContext(context.Background(), baseRepo, maybeHeadRepo, maybePull, user, pullNum, cmd)
}

// RunAutoplanCommand implements events.CommandRunner.
func (r *Router) RunAutoplanCommand(baseRepo models.Repo, headRepo models.Repo, pull models.PullRequest, user models.User) {
	r.RunAutoplanCommandWithContext(context.Background(), baseRepo, headRepo, pull, user)
}

// RunCommentCommandWithContext implements events.ContextCommandRunner.
func (r *Router) RunCommentCommandWithContext(ctx context.Context, baseRepo models.Repo, maybeHeadRepo *models.Repo, maybePull *models.PullRequest, user models.User, pullNum int, cmd *events.CommentCommand) {
	runLocal := func() {
		r.Local.RunCommentCommandWithContext(ctx, baseRepo, maybeHeadRepo, maybePull, user, pullNum, cmd)
	}
	if cmd != nil && cmd.Name == command.Apply {
		// Applies go where the plan files are, not to the hash owner.
		holder, local, ok := r.planHolder(ctx, baseRepo, pullNum)
		if local {
			r.Scope.Counter("local").Inc(1)
			runLocal()
			return
		}
		if ok {
			req := commandRequest{Kind: kindComment, BaseRepo: baseRepo, HeadRepo: maybeHeadRepo, Pull: maybePull, User: user, PullNum: pullNum, Command: cmd, ForwardedFrom: r.Owners.Identity()}
			r.dispatch(ctx, holder, req, runLocal)
			return
		}
	}
	if owner, ok := r.ownerFor(baseRepo, pullNum); ok {
		req := commandRequest{Kind: kindComment, BaseRepo: baseRepo, HeadRepo: maybeHeadRepo, Pull: maybePull, User: user, PullNum: pullNum, Command: cmd, ForwardedFrom: r.Owners.Identity()}
		r.dispatch(ctx, owner, req, runLocal)
		return
	}
	r.Scope.Counter("local").Inc(1)
	runLocal()
}

// RunAutoplanCommandWithContext implements events.ContextCommandRunner.
func (r *Router) RunAutoplanCommandWithContext(ctx context.Context, baseRepo models.Repo, headRepo models.Repo, pull models.PullRequest, user models.User) {
	runLocal := func() { r.Local.RunAutoplanCommandWithContext(ctx, baseRepo, headRepo, pull, user) }
	if owner, ok := r.ownerFor(baseRepo, pull.Num); ok {
		req := commandRequest{Kind: kindAutoplan, BaseRepo: baseRepo, HeadRepo: &headRepo, Pull: &pull, User: user, PullNum: pull.Num, ForwardedFrom: r.Owners.Identity()}
		r.dispatch(ctx, owner, req, runLocal)
		return
	}
	r.Scope.Counter("local").Inc(1)
	runLocal()
}

// CleanUpPull implements events.PullCleaner. It runs on the owner so the
// owner's clone and in-memory job output are removed.
func (r *Router) CleanUpPull(logger logging.SimpleLogging, repo models.Repo, pull models.PullRequest) error {
	if owner, ok := r.ownerFor(repo, pull.Num); ok {
		// Cleanup is idempotent, so any failure can fall back to local.
		if r.forward(context.Background(), owner, cleanupPath, cleanupRequest{Repo: repo, Pull: pull}, repo.FullName, pull.Num) == delivered {
			return nil
		}
	}
	return r.LocalClean.CleanUpPull(logger, repo, pull)
}
