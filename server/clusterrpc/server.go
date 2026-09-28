package clusterrpc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/runatlantis/atlantis/server/events"
	"github.com/runatlantis/atlantis/server/logging"
)

// JobLookup reports whether this replica holds a job's output.
type JobLookup interface {
	IsKeyExists(key string) bool
}

// Server is the internal listener's handler.
type Server struct {
	Token      string
	Local      events.ContextCommandRunner
	LocalClean events.PullCleaner
	Jobs       JobLookup
	// JobsHandler serves /jobs/{id} and /jobs/{id}/ws for proxied requests.
	JobsHandler http.Handler
	Logger      logging.SimpleLogging
	// Drain is checked before accepting forwarded commands.
	Drain interface{ GetStatus() events.DrainStatus }
}

// Handler returns the internal HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST "+commandsPath, s.auth(s.commands))
	mux.HandleFunc("POST "+cleanupPath, s.auth(s.cleanup))
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

func traceContext(r *http.Request) context.Context {
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), propagation.HeaderCarrier(r.Header))
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

func (s *Server) jobExists(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || strings.Contains(id, "/") || !s.Jobs.IsKeyExists(id) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}
