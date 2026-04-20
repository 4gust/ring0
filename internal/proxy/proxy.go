package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/4gust/ring0/internal/model"
)

// Server is a single HTTP listener that reverse-proxies based on path
// (and optional host) prefixes pulled from the route store.
type Server struct {
	addr   string
	mu     sync.RWMutex
	routes []routeEntry
	srv    *http.Server
	hits   atomic.Int64
}

type routeEntry struct {
	host    string // optional, lowercased; empty = match any host
	prefix  string // path prefix, e.g. "/api"
	target  *url.URL
	proxy   *httputil.ReverseProxy
	stripPx bool
}

// New returns a not-yet-started proxy bound to addr (e.g. ":8080" or
// "0.0.0.0:80"). Use Reload to install routes; Start to listen.
func New(addr string) *Server {
	return &Server{addr: addr}
}

// Addr returns the listener address.
func (s *Server) Addr() string { return s.addr }

// Hits returns the total number of requests served since process start.
func (s *Server) Hits() int64 { return s.hits.Load() }

// Reload swaps the routing table atomically. Call any time routes change.
func (s *Server) Reload(rs []*model.Route) {
	entries := make([]routeEntry, 0, len(rs))
	for _, r := range rs {
		if r.TargetPort == 0 || r.Path == "" {
			continue
		}
		u, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", r.TargetPort))
		if err != nil {
			continue
		}
		rp := httputil.NewSingleHostReverseProxy(u)
		// Preserve original Host header so apps that care (vite, etc) see it.
		orig := rp.Director
		rp.Director = func(req *http.Request) {
			orig(req)
			req.Host = u.Host
		}
		rp.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
			http.Error(w, fmt.Sprintf("ring0 proxy: upstream :%d unreachable (%v)", r.TargetPort, err), http.StatusBadGateway)
		}
		entries = append(entries, routeEntry{
			host:   strings.ToLower(r.Host),
			prefix: r.Path,
			target: u,
			proxy:  rp,
		})
	}
	// Longest prefix wins (so /api/v2 beats /api).
	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].prefix) > len(entries[j].prefix)
	})
	s.mu.Lock()
	s.routes = entries
	s.mu.Unlock()
}

// ServeHTTP implements the routing.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.hits.Add(1)
	s.mu.RLock()
	routes := s.routes
	s.mu.RUnlock()
	host := strings.ToLower(strings.SplitN(r.Host, ":", 2)[0])
	for _, e := range routes {
		if e.host != "" && e.host != host {
			continue
		}
		if r.URL.Path == e.prefix || strings.HasPrefix(r.URL.Path, e.prefix+"/") || e.prefix == "/" {
			e.proxy.ServeHTTP(w, r)
			return
		}
	}
	s.writeIndex(w, routes)
}

func (s *Server) writeIndex(w http.ResponseWriter, routes []routeEntry) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintln(w, `<!doctype html><title>ring0</title><style>body{font:14px/1.4 ui-monospace,monospace;padding:2rem;color:#cdd6f4;background:#1e1e2e}a{color:#89b4fa}</style>`)
	fmt.Fprintln(w, `<h2>ring0 proxy</h2><p>No route matched. Configured routes:</p><ul>`)
	if len(routes) == 0 {
		fmt.Fprintln(w, `<li><em>(none)</em></li>`)
	}
	for _, e := range routes {
		host := e.host
		if host == "" {
			host = "*"
		}
		fmt.Fprintf(w, `<li><code>%s%s</code> &rarr; %s</li>`, host, e.prefix, e.target)
	}
	fmt.Fprintln(w, `</ul>`)
}

// Start binds and serves. Returns once the listener stops.
func (s *Server) Start() error {
	s.srv = &http.Server{
		Addr:              s.addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s.srv.ListenAndServe()
}

// Stop shuts the listener down gracefully.
func (s *Server) Stop() {
	if s.srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
}
