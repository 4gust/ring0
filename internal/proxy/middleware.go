package proxy

import (
	"compress/gzip"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// chain composes middlewares (outermost first).
func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// ---------------- Gzip ----------------

type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) { return g.gz.Write(b) }

func gzipMW(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.ServeHTTP(w, r)
			return
		}
		// Don't gzip already-compressed types; the upstream may set its own
		// content-encoding. We just signal capability and let it proxy.
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		h.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

// ---------------- Basic auth ----------------
// creds = "user:password" or "u1:p1,u2:p2"

func basicAuthMW(creds string) func(http.Handler) http.Handler {
	users := map[string]string{}
	for _, pair := range strings.Split(creds, ",") {
		pair = strings.TrimSpace(pair)
		if i := strings.Index(pair, ":"); i > 0 {
			users[pair[:i]] = pair[i+1:]
		}
	}
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHdr := r.Header.Get("Authorization")
			if strings.HasPrefix(authHdr, "Basic ") {
				if raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authHdr, "Basic ")); err == nil {
					parts := strings.SplitN(string(raw), ":", 2)
					if len(parts) == 2 {
						if pw, ok := users[parts[0]]; ok &&
							subtle.ConstantTimeCompare([]byte(pw), []byte(parts[1])) == 1 {
							h.ServeHTTP(w, r)
							return
						}
					}
				}
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="ring0"`)
			renderErrorPage(w, http.StatusUnauthorized, "auth required",
				r.URL.Path, "—", "Basic auth credentials missing or invalid")
		})
	}
}

// ---------------- IP allow-list ----------------

func allowCIDRMW(cidrs []string) func(http.Handler) http.Handler {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if !strings.Contains(c, "/") {
			c += "/32"
		}
		if _, n, err := net.ParseCIDR(c); err == nil {
			nets = append(nets, n)
		}
	}
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ipStr, _, _ := net.SplitHostPort(r.RemoteAddr)
			// Honor X-Forwarded-For if behind another LB.
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				ipStr = strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
			}
			ip := net.ParseIP(ipStr)
			for _, n := range nets {
				if ip != nil && n.Contains(ip) {
					h.ServeHTTP(w, r)
					return
				}
			}
			renderErrorPage(w, http.StatusForbidden, "forbidden",
				r.URL.Path, "—", "client IP not in allow list: "+ipStr)
		})
	}
}

// ---------------- Rate limit (token bucket per client IP) ----------------

type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	last     time.Time
	rate     float64
	capacity float64
}

func (b *tokenBucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * b.rate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	b, ok := rl.buckets[ip]
	if !ok {
		b = &tokenBucket{tokens: rl.rate, last: time.Now(), rate: rl.rate, capacity: rl.rate * 2}
		rl.buckets[ip] = b
	}
	rl.mu.Unlock()
	return b.take()
}

func rateLimitMW(perSec int) func(http.Handler) http.Handler {
	rl := &rateLimiter{buckets: map[string]*tokenBucket{}, rate: float64(perSec)}
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				ip = strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
			}
			if !rl.allow(ip) {
				w.Header().Set("Retry-After", "1")
				renderErrorPage(w, http.StatusTooManyRequests, "rate limit",
					r.URL.Path, "—", "too many requests; slow down")
				return
			}
			h.ServeHTTP(w, r)
		})
	}
}

// ---------------- CORS ----------------

func corsMW(origins []string) func(http.Handler) http.Handler {
	allowAll := false
	allow := map[string]bool{}
	for _, o := range origins {
		if o == "*" {
			allowAll = true
		}
		allow[o] = true
	}
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (allowAll || allow[origin]) {
				if allowAll {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h.ServeHTTP(w, r)
		})
	}
}

// ---------------- Access log ----------------

type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.size += n
	return n, err
}

// accessLogMW writes Combined-Log-Format-ish lines to w (e.g. an open file).
func accessLogMW(out io.Writer) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: 200}
			h.ServeHTTP(rec, r)
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			line := time.Now().Format("2006-01-02T15:04:05Z07:00") + " " +
				ip + " \"" + r.Method + " " + r.URL.RequestURI() + " " + r.Proto + "\" " +
				itoa(rec.status) + " " + itoa(rec.size) + " " +
				time.Since(start).String() + " \"" + r.Header.Get("User-Agent") + "\"\n"
			_, _ = out.Write([]byte(line))
		})
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
