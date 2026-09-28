package clusterrpc

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/runatlantis/atlantis/server/core/kube/cluster"
	"github.com/runatlantis/atlantis/server/logging"
)

// JobProxy serves job pages and websockets for jobs running on another
// replica. Jobs run on the pull's owner, but the job URL posted to the pull
// request can be opened through any replica behind the Service.
type JobProxy struct {
	Jobs   JobLookup
	Owners Owners
	Client *Client
	Logger logging.SimpleLogging
	// Probe bounds each "do you have this job" request. Defaults to 2s.
	Probe time.Duration
}

// Middleware wraps the local jobs handlers.
func (p *JobProxy) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["job-id"]
		if id == "" || r.Header.Get(ForwardedHeader) != "" || p.Jobs.IsKeyExists(id) {
			next.ServeHTTP(w, r)
			return
		}
		member, ok := p.find(r.Context(), id)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		target, err := url.Parse(member.Address)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		rp := &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
				pr.Out.URL.Path = r.URL.Path
				pr.Out.Host = r.Host // keep Host so the websocket origin check still passes
				p.Client.authorize(pr.In.Context(), pr.Out)
				pr.Out.Header.Set(ForwardedHeader, p.Owners.Identity())
			},
			ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
				p.Logger.Warn("proxying job %s to %s: %s", id, member.Identity, err)
				http.Error(w, "job is on an unreachable replica", http.StatusBadGateway)
			},
		}
		rp.ServeHTTP(w, r)
	})
}

// find asks the other live members, in parallel, which one holds the job.
func (p *JobProxy) find(ctx context.Context, id string) (cluster.Member, bool) {
	timeout := p.Probe
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	found := make(chan cluster.Member, 1)
	var wg sync.WaitGroup
	for _, m := range p.Owners.Members() {
		if m.Identity == p.Owners.Identity() || m.Address == "" {
			continue
		}
		wg.Go(func() {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(m.Address, "/")+jobExistsPath+url.PathEscape(id), nil)
			if err != nil {
				return
			}
			p.Client.authorize(ctx, req)
			resp, err := p.Client.HTTPClient.Do(req)
			if err != nil {
				return
			}
			resp.Body.Close() // nolint: errcheck
			if resp.StatusCode == http.StatusOK {
				select {
				case found <- m:
					cancel()
				default:
				}
			}
		})
	}
	go func() { wg.Wait(); close(found) }()
	m, ok := <-found
	return m, ok
}
