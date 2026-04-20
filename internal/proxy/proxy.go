package proxy

import (
	"context"
	"fmt"
	"net"
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

// Server is a single HTTP listener that reverse-proxies (or redirects)
// based on path + optional host prefixes pulled from the route store.
//
// Features:
//   - Path-prefix routing with optional host match
//   - Longest-prefix-wins
//   - Permanent redirects (308) when Route.Redirect is set
//   - Optional StripPrefix (like nginx `rewrite ^/api(/.*)$ $1 break`)
//   - X-Forwarded-{For,Proto,Host} + X-Real-IP headers (nginx-compatible)
//   - WebSocket upgrades (handled transparently by net/http/httputil)
//   - Hot reload — Reload() can be called any time without dropping conns
type Server struct {
	addr   string
	mu     sync.RWMutex
	routes []routeEntry
	srv    *http.Server
	hits   atomic.Int64
}

type routeEntry struct {
	host        string // optional, lowercased; empty = any host
	prefix      string // path prefix, e.g. "/api"
	target      *url.URL
	proxy       *httputil.ReverseProxy
	stripPrefix bool
	redirect    string // if set, send 308 here instead of proxying
}

// New returns a not-yet-started proxy bound to addr (e.g. ":8080").
func New(addr string) *Server { return &Server{addr: addr} }

func (s *Server) Addr() string { return s.addr }
func (s *Server) Hits() int64  { return s.hits.Load() }

// Routes returns a snapshot of installed routes for diagnostics.
func (s *Server) Routes() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.routes))
	for _, e := range s.routes {
		host := e.host
		if host == "" {
			host = "*"
		}
		dst := e.target.String()
		if e.redirect != "" {
			dst = "redirect → " + e.redirect
		} else if e.stripPrefix {
			dst += " (strip)"
		}
		out = append(out, fmt.Sprintf("%s%s → %s", host, e.prefix, dst))
	}
	return out
}

// Reload swaps the routing table atomically.
func (s *Server) Reload(rs []*model.Route) {
	entries := make([]routeEntry, 0, len(rs))
	for _, r := range rs {
		if r.Path == "" {
			continue
		}
		e := routeEntry{
			host:        strings.ToLower(r.Host),
			prefix:      r.Path,
			stripPrefix: r.StripPrefix,
			redirect:    strings.TrimSpace(r.Redirect),
		}
		if e.redirect == "" {
			if r.TargetPort == 0 {
				continue
			}
			u, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", r.TargetPort))
			if err != nil {
				continue
			}
			e.target = u
			e.proxy = newReverseProxy(u, r.Path, r.StripPrefix)
		}
		entries = append(entries, e)
	}
	// Longest-prefix wins.
	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].prefix) > len(entries[j].prefix)
	})
	s.mu.Lock()
	s.routes = entries
	s.mu.Unlock()
}

func newReverseProxy(target *url.URL, prefix string, strip bool) *httputil.ReverseProxy {
	rp := httputil.NewSingleHostReverseProxy(target)
	orig := rp.Director
	rp.Director = func(req *http.Request) {
		// Snapshot client info BEFORE upstream rewriting.
		clientIP, _, _ := net.SplitHostPort(req.RemoteAddr)
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		origHost := req.Host

		orig(req)
		req.Host = target.Host

		// Path rewrite (nginx: rewrite ^/api/(.*)$ /$1 break).
		if strip && prefix != "/" {
			p := strings.TrimPrefix(req.URL.Path, prefix)
			if p == "" {
				p = "/"
			}
			req.URL.Path = p
		}

		// nginx-style forwarded headers.
		if clientIP != "" {
			if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
				req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
			} else {
				req.Header.Set("X-Forwarded-For", clientIP)
			}
			req.Header.Set("X-Real-IP", clientIP)
		}
		req.Header.Set("X-Forwarded-Proto", scheme)
		req.Header.Set("X-Forwarded-Host", origHost)
	}
	rp.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		http.Error(w,
			fmt.Sprintf("ring0 proxy: upstream %s unreachable (%v)", target, err),
			http.StatusBadGateway)
	}
	// Generous timeouts for long-lived streams (HMR, websockets, SSE).
	rp.FlushInterval = 100 * time.Millisecond
	return rp
}

// ServeHTTP performs the routing.
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
		if !pathMatches(r.URL.Path, e.prefix) {
			continue
		}
		if e.redirect != "" {
			http.Redirect(w, r, e.redirect, http.StatusPermanentRedirect)
			return
		}
		e.proxy.ServeHTTP(w, r)
		return
	}
	s.writeIndex(w, routes)
}

func pathMatches(p, prefix string) bool {
	if prefix == "/" {
		return true
	}
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

func (s *Server) writeIndex(w http.ResponseWriter, routes []routeEntry) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintln(w, `<!doctype html><title>ring0</title><style>body{font:14px/1.4 ui-monospace,monospace;padding:2rem;color:#cdd6f4;background:#1e1e2e}code{color:#a6e3a1}</style>`)
	fmt.Fprintln(w, `<h2>ring0 proxy</h2><p>No route matched. Configured routes:</p><ul>`)
	if len(routes) == 0 {
		fmt.Fprintln(w, `<li><em>(none)</em></li>`)
	}
	for _, e := range routes {
		host := e.host
		if host == "" {
			host = "*"
		}
		dst := ""
		if e.redirect != "" {
			dst = "redirect → " + e.redirect
		} else {
			dst = e.target.String()
			if e.stripPrefix {
				dst += " (strip)"
			}
		}
		fmt.Fprintf(w, `<li><code>%s%s</code> → %s</li>`, host, e.prefix, dst)
	}
	fmt.Fprintln(w, `</ul>`)
}

// Start binds and serves.
func (s *Server) Start() error {
	s.srv = &http.Server{
		Addr:              s.addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s.srv.ListenAndServe()
}

// Serve uses an already-bound listener (so the caller can validate binding
// before the UI starts and can fall back to a different port).
func (s *Server) Serve(ln net.Listener) error {
	s.srv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s.srv.Serve(ln)
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
