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
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tally "github.com/uber-go/tally/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/runatlantis/atlantis/server/core/kube"
	"github.com/runatlantis/atlantis/server/core/kube/cluster"
	"github.com/runatlantis/atlantis/server/events"
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
		return fmt.Errorf("%s returned %s: %s", member.Identity, resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
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

func (r *Router) forward(ctx context.Context, owner cluster.Member, path string, body any, repo string, pullNum int) bool {
	ctx, span := tracing.Start(ctx, "atlantis.cluster.forward",
		tracing.AttrRepo.String(repo), tracing.AttrPull.Int(pullNum), tracing.AttrReplica.String(owner.Identity))
	cctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	err := r.Client.post(cctx, owner, path, body)
	tracing.End(span, err)
	if err != nil {
		r.Scope.Counter("forward_error").Inc(1)
		r.Logger.Warn("forwarding %s#%d to owner %s failed, running locally: %s", repo, pullNum, owner.Identity, err)
		return false
	}
	r.Scope.Counter("forward_success").Inc(1)
	r.Logger.Debug("forwarded %s#%d to owner %s", repo, pullNum, owner.Identity)
	return true
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
	if owner, ok := r.ownerFor(baseRepo, pullNum); ok {
		req := commandRequest{Kind: kindComment, BaseRepo: baseRepo, HeadRepo: maybeHeadRepo, Pull: maybePull, User: user, PullNum: pullNum, Command: cmd, ForwardedFrom: r.Owners.Identity()}
		if r.forward(ctx, owner, commandsPath, req, baseRepo.FullName, pullNum) {
			return
		}
	}
	r.Scope.Counter("local").Inc(1)
	r.Local.RunCommentCommandWithContext(ctx, baseRepo, maybeHeadRepo, maybePull, user, pullNum, cmd)
}

// RunAutoplanCommandWithContext implements events.ContextCommandRunner.
func (r *Router) RunAutoplanCommandWithContext(ctx context.Context, baseRepo models.Repo, headRepo models.Repo, pull models.PullRequest, user models.User) {
	if owner, ok := r.ownerFor(baseRepo, pull.Num); ok {
		req := commandRequest{Kind: kindAutoplan, BaseRepo: baseRepo, HeadRepo: &headRepo, Pull: &pull, User: user, PullNum: pull.Num, ForwardedFrom: r.Owners.Identity()}
		if r.forward(ctx, owner, commandsPath, req, baseRepo.FullName, pull.Num) {
			return
		}
	}
	r.Scope.Counter("local").Inc(1)
	r.Local.RunAutoplanCommandWithContext(ctx, baseRepo, headRepo, pull, user)
}

// CleanUpPull implements events.PullCleaner. It runs on the owner so the
// owner's clone and in-memory job output are removed.
func (r *Router) CleanUpPull(logger logging.SimpleLogging, repo models.Repo, pull models.PullRequest) error {
	if owner, ok := r.ownerFor(repo, pull.Num); ok {
		if r.forward(context.Background(), owner, cleanupPath, cleanupRequest{Repo: repo, Pull: pull}, repo.FullName, pull.Num) {
			return nil
		}
	}
	return r.LocalClean.CleanUpPull(logger, repo, pull)
}
