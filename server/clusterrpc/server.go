package clusterrpc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/runatlantis/atlantis/server/events"
	"github.com/runatlantis/atlantis/server/events/models"
	"github.com/runatlantis/atlantis/server/logging"
)

// ReplicaCleaner removes this replica's local resources for a closed pull.
type ReplicaCleaner interface {
	CleanUpReplica(logger logging.SimpleLogging, repo models.Repo, pull models.PullRequest) error
}

// JobLookup reports whether this replica holds a job's output.
type JobLookup interface {
	IsKeyExists(key string) bool
}

// Server is the internal listener's handler.
type Server struct {
	Token      string
	Local      events.ContextCommandRunner
	LocalClean events.PullCleaner
	// Replica handles replica-local cleanup broadcasts; optional.
	Replica ReplicaCleaner
	Jobs    JobLookup
	// JobsHandler serves /jobs/{id} and /jobs/{id}/ws for proxied requests.
	JobsHandler http.Handler
	Logger      logging.SimpleLogging
	// Drain is checked before accepting forwarded commands.
	Drain interface{ GetStatus() events.DrainStatus }

	seenMu sync.Mutex
	seen   map[string]time.Time
}

// seenTTL bounds how long request IDs are remembered; it only needs to
// outlast the sender's retries.
const seenTTL = 15 * time.Minute

// firstDelivery records id and reports whether it had not been seen before.
func (s *Server) firstDelivery(id string) bool {
	if id == "" {
		return true
	}
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	now := time.Now()
	if s.seen == nil {
		s.seen = map[string]time.Time{}
	}
	for k, t := range s.seen {
		if now.Sub(t) > seenTTL {
			delete(s.seen, k)
		}
	}
	if _, dup := s.seen[id]; dup {
		return false
	}
	s.seen[id] = now
	return true
}

// Handler returns the internal HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST "+commandsPath, s.auth(s.commands))
	mux.HandleFunc("POST "+cleanupPath, s.auth(s.cleanup))
	mux.HandleFunc("POST "+replicaCleanupPath, s.auth(s.cleanupReplica))
	mux.HandleFunc("GET "+jobExistsPath+"{id}", s.auth(s.jobExists))
	mux.HandleFunc("GET /jobs/", s.auth(func(w http.ResponseWriter, r *http.Request) {
		// Never proxy a proxied request again.
		r.Header.Set(ForwardedHeader, "1")
		s.JobsHandler.ServeHTTP(w, r)
	}))
	return mux
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !Authorized(r, s.Token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// traceContext returns a context for work that outlives the request. It keeps
// the request's span (otelhttp already extracted the caller's trace context)
// but not its cancellation. Without otelhttp in front, the propagated
// context is extracted here.
func traceContext(r *http.Request) context.Context {
	ctx := context.WithoutCancel(r.Context())
	if !trace.SpanContextFromContext(ctx).IsValid() {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(r.Header))
	}
	return ctx
}

func (s *Server) commands(w http.ResponseWriter, r *http.Request) {
	if s.Drain != nil && s.Drain.GetStatus().ShuttingDown {
		// The sender falls back to running the command itself.
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	var req commandRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.firstDelivery(req.RequestID) {
		// A retry of a request we already accepted: acknowledge, do not rerun.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	ctx := traceContext(r)
	s.Logger.Debug("accepted %s command for %s#%d forwarded from %s", req.Kind, req.BaseRepo.FullName, req.PullNum, req.ForwardedFrom)
	switch req.Kind {
	case kindComment:
		go s.Local.RunCommentCommandWithContext(ctx, req.BaseRepo, req.HeadRepo, req.Pull, req.User, req.PullNum, req.Command)
	case kindAutoplan:
		if req.HeadRepo == nil || req.Pull == nil {
			http.Error(w, "autoplan requires headRepo and pull", http.StatusBadRequest)
			return
		}
		go s.Local.RunAutoplanCommandWithContext(ctx, req.BaseRepo, *req.HeadRepo, *req.Pull, req.User)
	default:
		http.Error(w, "unknown kind", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) cleanup(w http.ResponseWriter, r *http.Request) {
	var req cleanupRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.LocalClean.CleanUpPull(s.Logger, req.Repo, req.Pull); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) cleanupReplica(w http.ResponseWriter, r *http.Request) {
	if s.Replica == nil {
		http.Error(w, "replica cleanup not supported", http.StatusNotImplemented)
		return
	}
	var req cleanupRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.Replica.CleanUpReplica(s.Logger, req.Repo, req.Pull); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) jobExists(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || strings.Contains(id, "/") || !s.Jobs.IsKeyExists(id) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}
