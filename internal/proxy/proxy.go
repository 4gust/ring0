package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/4gust/ring0/internal/model"
)

// Server is a single (or HTTPS+HTTP) listener that reverse-proxies, serves
// static files, redirects, or load-balances based on path + optional host.
//
// Per-route features: redirect, strip-prefix, static-dir, multi-upstream
// load-balancing, health checks, gzip, basic-auth, IP allow-list, rate-limit,
// CORS. Server-level: TLS via Let's Encrypt (autocert) → HTTP/2 free.
type Server struct {
	addr   string
	mu     sync.RWMutex
	routes []routeEntry
	srv    *http.Server
	hits   atomic.Int64

	cfg      *model.ServerConfig
	accessFh *os.File
	cancelHC context.CancelFunc
}

type routeEntry struct {
	host        string
	prefix      string
	stripPrefix bool
	redirect    string
	staticDir   string
	target      *url.URL // first upstream (for index display)
	pool        *upstreamPool
	handler     http.Handler // wrapped with middleware
}

// New returns a not-yet-started proxy bound to addr.
func New(addr string) *Server { return &Server{addr: addr} }

func (s *Server) Addr() string { return s.addr }
func (s *Server) Hits() int64  { return s.hits.Load() }

// SetConfig installs server-level config (TLS, access log).
func (s *Server) SetConfig(c *model.ServerConfig) { s.cfg = c }

// Routes returns a snapshot for diagnostics.
func (s *Server) Routes() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.routes))
	for _, e := range s.routes {
		host := e.host
		if host == "" {
			host = "*"
		}
		var dst string
		switch {
		case e.redirect != "":
			dst = "redirect → " + e.redirect
		case e.staticDir != "":
			dst = "static " + e.staticDir
		case e.pool != nil:
			dst = "pool " + e.pool.summary()
		}
		out = append(out, fmt.Sprintf("%s%s → %s", host, e.prefix, dst))
	}
	return out
}

func (p *upstreamPool) summary() string {
	if p == nil {
		return ""
	}
	parts := make([]string, len(p.all))
	for i, u := range p.all {
		flag := "✓"
		if !u.healthy.Load() {
			flag = "✗"
		}
		parts[i] = flag + u.url.Host
	}
	return strings.Join(parts, ",")
}

// Reload swaps the routing table atomically and restarts health checks.
func (s *Server) Reload(rs []*model.Route) {
	if s.cancelHC != nil {
		s.cancelHC()
	}
	hcCtx, cancel := context.WithCancel(context.Background())
	s.cancelHC = cancel

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
			staticDir:   strings.TrimSpace(r.StaticDir),
		}

		var inner http.Handler
		switch {
		case e.redirect != "":
			redir := e.redirect
			inner = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				http.Redirect(w, req, redir, http.StatusPermanentRedirect)
			})
		case e.staticDir != "":
			fs := http.FileServer(http.Dir(e.staticDir))
			if r.StripPrefix && r.Path != "/" {
				inner = http.StripPrefix(r.Path, fs)
			} else {
				inner = fs
			}
		default:
			ups := r.Upstreams
			if len(ups) == 0 && r.TargetPort > 0 {
				ups = []string{fmt.Sprintf("127.0.0.1:%d", r.TargetPort)}
			}
			if len(ups) == 0 {
				continue
			}
			pool := newUpstreamPool(ups, r.Path, r.StripPrefix)
			pool.startHealthChecks(hcCtx, r.HealthPath, 5*time.Second)
			e.pool = pool
			if len(pool.all) > 0 {
				e.target = pool.all[0].url
			}
			inner = pool
		}

		// Stack middleware (applied innermost-out).
		var mws []func(http.Handler) http.Handler
		if len(r.CORSOrigins) > 0 {
			mws = append(mws, corsMW(r.CORSOrigins))
		}
		if len(r.AllowCIDRs) > 0 {
			mws = append(mws, allowCIDRMW(r.AllowCIDRs))
		}
		if r.BasicAuth != "" {
			mws = append(mws, basicAuthMW(r.BasicAuth))
		}
		if r.RateLimitPerSec > 0 {
			mws = append(mws, rateLimitMW(r.RateLimitPerSec))
		}
		if r.Gzip {
			mws = append(mws, gzipMW)
		}
		e.handler = chain(inner, mws...)

		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].prefix) > len(entries[j].prefix)
	})
	s.mu.Lock()
	s.routes = entries
	s.mu.Unlock()
}

// newReverseProxy is shared by upstream.go.
func newReverseProxy(target *url.URL, prefix string, strip bool) *httputil.ReverseProxy {
	rp := httputil.NewSingleHostReverseProxy(target)
	orig := rp.Director
	rp.Director = func(req *http.Request) {
		clientIP, _, _ := net.SplitHostPort(req.RemoteAddr)
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		origHost := req.Host
		// Strip the incoming prefix BEFORE the default director joins it with
		// the target path. This gives nginx-style behavior:
		//   route /hello → http://localhost:3000/api  (strip=true)
		//   request /hello/world → upstream /api/world
		if strip && prefix != "/" {
			p := strings.TrimPrefix(req.URL.Path, prefix)
			if p == "" {
				p = "/"
			}
			req.URL.Path = p
		}
		orig(req)
		req.Host = target.Host
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
		renderErrorPage(w, http.StatusBadGateway, "bad gateway",
			req.URL.Path, target.String(), err.Error())
	}
	rp.FlushInterval = 100 * time.Millisecond
	return rp
}

// ServeHTTP routes the request and writes the response.
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
		e.handler.ServeHTTP(w, r)
		return
	}
	renderIndex(w, r.Host, r.URL.Path, routes)
}

func pathMatches(p, prefix string) bool {
	if prefix == "/" {
		return true
	}
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

// Start binds and serves with optional TLS + access logging.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve uses an already-bound listener.
func (s *Server) Serve(ln net.Listener) error {
	handler := http.Handler(s)

	if s.cfg != nil && s.cfg.AccessLog != "" {
		fh, err := openAccessLog(s.cfg.AccessLog)
		if err == nil {
			s.accessFh = fh
			handler = accessLogMW(io.MultiWriter(fh))(handler)
		}
	}

	if s.cfg != nil && s.cfg.TLSEnabled && len(s.cfg.TLSDomains) > 0 {
		certDir := s.cfg.TLSCertDir
		if certDir == "" {
			home, _ := os.UserHomeDir()
			certDir = filepath.Join(home, ".ring0", "certs")
		}
		_ = os.MkdirAll(certDir, 0o700)
		mgr := &autocert.Manager{
			Cache:      autocert.DirCache(certDir),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(s.cfg.TLSDomains...),
			Email:      s.cfg.TLSEmail,
		}
		// HTTP listener: ACME-01 challenges + redirect everything else to HTTPS.
		go func() {
			httpSrv := &http.Server{
				Handler: mgr.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusMovedPermanently)
				})),
				ReadHeaderTimeout: 10 * time.Second,
			}
			_ = httpSrv.Serve(ln)
		}()
		// HTTPS listener: :443
		s.srv = &http.Server{
			Addr:              ":443",
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			TLSConfig:         mgr.TLSConfig(), // negotiates HTTP/2
		}
		return s.srv.ListenAndServeTLS("", "")
	}

	s.srv = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s.srv.Serve(ln)
}

// Stop shuts the listener down gracefully.
func (s *Server) Stop() {
	if s.cancelHC != nil {
		s.cancelHC()
	}
	if s.accessFh != nil {
		_ = s.accessFh.Close()
	}
	if s.srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
}

// openAccessLog handles ~ expansion and append.
func openAccessLog(path string) (*os.File, error) {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}
