package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/4gust/ring0/internal/model"
)

// Verifies SPAFallback serves index.html for unknown paths, real files for known paths.
func TestSPAFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("JS"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(":0")
	s.Reload([]*model.Route{{
		ID: "r1", Path: "/v8", Visibility: model.Public,
		StaticDir: dir, SPAFallback: true, StripPrefix: true,
	}})

	cases := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/v8/", 200, "INDEX"},
		{"/v8/login", 200, "INDEX"},          // SPA deep link → index.html
		{"/v8/customers/123", 200, "INDEX"},  // nested deep link → index.html
		{"/v8/assets/app.js", 200, "JS"},     // real asset
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", tc.path, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != tc.wantStatus {
			t.Errorf("%s: status=%d want %d", tc.path, w.Code, tc.wantStatus)
		}
		body, _ := io.ReadAll(w.Body)
		if string(body) != tc.wantBody {
			t.Errorf("%s: body=%q want %q", tc.path, body, tc.wantBody)
		}
	}
}

// Verifies RewritePrefix replaces the matched prefix on the upstream request.
func TestRewritePrefix(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	s := New(":0")
	s.Reload([]*model.Route{{
		ID: "r1", Path: "/v8/api", Visibility: model.Public,
		Upstreams: []string{upstream.URL}, RewritePrefix: "/api",
	}})

	req := httptest.NewRequest("GET", "/v8/api/customers/42", nil)
	s.ServeHTTP(httptest.NewRecorder(), req)
	if got != "/api/customers/42" {
		t.Errorf("upstream got %q, want /api/customers/42", got)
	}
}

// Verifies StripPrefix still works (rewrite empty, strip true).
func TestStripPrefixUnchanged(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	s := New(":0")
	s.Reload([]*model.Route{{
		ID: "r1", Path: "/v8", Visibility: model.Public,
		Upstreams: []string{upstream.URL}, StripPrefix: true,
	}})

	req := httptest.NewRequest("GET", "/v8/foo/bar", nil)
	s.ServeHTTP(httptest.NewRecorder(), req)
	if got != "/foo/bar" {
		t.Errorf("upstream got %q, want /foo/bar", got)
	}
}

// Sanity: longest-prefix-wins still holds with the new fields.
func TestLongestPrefixWins(t *testing.T) {
	var hit string
	apiUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = "api"
	}))
	defer apiUp.Close()
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("static"), 0o644)

	s := New(":0")
	s.Reload([]*model.Route{
		{ID: "a", Path: "/v8", StaticDir: dir, SPAFallback: true, StripPrefix: true, Visibility: model.Public},
		{ID: "b", Path: "/v8/api", Upstreams: []string{apiUp.URL}, RewritePrefix: "/api", Visibility: model.Public},
	})

	hit = ""
	req := httptest.NewRequest("GET", "/v8/api/x", nil)
	s.ServeHTTP(httptest.NewRecorder(), req)
	if hit != "api" {
		t.Errorf("/v8/api/x should hit api upstream (got %q)", hit)
	}

	hit = ""
	req = httptest.NewRequest("GET", "/v8/login", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if hit != "" {
		t.Errorf("/v8/login should NOT hit api (got %q)", hit)
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != "static" {
		t.Errorf("/v8/login body=%q want static (SPA fallback)", body)
	}
	// silence "imported and not used" if time isn't otherwise referenced
	_ = time.Second
}
