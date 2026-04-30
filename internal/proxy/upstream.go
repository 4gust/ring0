package proxy

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// upstreamPool is a round-robin pool with health tracking.
type upstreamPool struct {
	mu  sync.RWMutex
	all []*upstream
	idx atomic.Uint64
}

type upstream struct {
	url     *url.URL
	healthy atomic.Bool
	proxy   *httputil.ReverseProxy
}

func newUpstreamPool(rawURLs []string, prefix string, strip bool, rewrite string) *upstreamPool {
	p := &upstreamPool{}
	for _, raw := range rawURLs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Allow "host:port" or "127.0.0.1:3001" shorthand.
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		up := &upstream{url: u, proxy: newReverseProxy(u, prefix, strip, rewrite)}
		up.healthy.Store(true)
		p.all = append(p.all, up)
	}
	return p
}

// next returns the next healthy upstream, or nil if none.
func (p *upstreamPool) next() *upstream {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := len(p.all)
	if n == 0 {
		return nil
	}
	for tries := 0; tries < n; tries++ {
		i := int(p.idx.Add(1)-1) % n
		if p.all[i].healthy.Load() {
			return p.all[i]
		}
	}
	return nil
}

// startHealthChecks polls each upstream's healthPath every interval.
// Pass empty healthPath to disable.
func (p *upstreamPool) startHealthChecks(ctx context.Context, healthPath string, interval time.Duration) {
	if healthPath == "" || len(p.all) == 0 {
		return
	}
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		t := time.NewTicker(interval)
		defer t.Stop()
		check := func() {
			p.mu.RLock()
			ups := append([]*upstream(nil), p.all...)
			p.mu.RUnlock()
			for _, up := range ups {
				u := *up.url
				u.Path = healthPath
				resp, err := client.Get(u.String())
				ok := err == nil && resp.StatusCode < 500
				if resp != nil {
					resp.Body.Close()
				}
				up.healthy.Store(ok)
			}
		}
		check()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				check()
			}
		}
	}()
}

func (p *upstreamPool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u := p.next()
	if u == nil {
		renderErrorPage(w, http.StatusBadGateway, "no healthy upstream",
			r.URL.Path, "—", "all upstreams are failing health checks")
		return
	}
	u.proxy.ServeHTTP(w, r)
}
