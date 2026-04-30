package model

import "time"

// Status of a managed application process.
type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusCrashed Status = "crashed"
)

// App is a user-defined process managed by ring0.
type App struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Cmd    string            `json:"cmd"`    // run command, e.g. "node server.js"
	Setup  string            `json:"setup"`  // optional one-shot setup, e.g. "npm install"
	Repo   string            `json:"repo"`   // optional git URL; pulled/cloned on update
	Branch string            `json:"branch"` // optional git branch (default: main)
	Cwd    string            `json:"cwd"`    // working directory
	Port   int               `json:"port"`   // optional listening port
	Env    map[string]string `json:"env,omitempty"`

	// AutoRestart re-spawns the process if it exits with a non-zero status
	// (i.e. crashes — not when the user pressed `x` to stop it).
	// RestartDelaySec is the delay between exit and respawn (default 3s).
	AutoRestart     bool `json:"auto_restart,omitempty"`
	RestartDelaySec int  `json:"restart_delay_sec,omitempty"`

	Status    Status    `json:"-"`
	PID       int       `json:"-"`
	StartedAt time.Time `json:"-"`
	ExitCode  int       `json:"-"`
}

// Visibility for routes.
type Visibility string

const (
	Public  Visibility = "public"
	Private Visibility = "private"
)

// Route maps an inbound path/host to a target port, static dir, or redirect URL.
//
// Resolution order (first non-zero wins): Redirect → StaticDir → Upstreams.
// All routes can layer middleware: Gzip, BasicAuth, AllowCIDRs, RateLimitPerSec, CORSOrigins.
type Route struct {
	ID         string     `json:"id"`
	Path       string     `json:"path"` // e.g. "/api"
	Host       string     `json:"host"` // optional, e.g. "api.local"
	TargetPort int        `json:"target_port"`
	Visibility Visibility `json:"visibility"`

	// Path rewriting / redirects
	StripPrefix bool `json:"strip_prefix"`
	// RewritePrefix replaces the matched Path prefix with this string before
	// forwarding upstream. Equivalent to nginx:
	//   rewrite ^/v8/api/(.*) /api/$1 break;
	// Set RewritePrefix="/api" on a route with Path="/v8/api" and a request
	// for /v8/api/users hits upstream as /api/users. If non-empty,
	// supersedes StripPrefix.
	RewritePrefix string `json:"rewrite_prefix,omitempty"`
	Redirect      string `json:"redirect"` // 308

	// Static file serving — if set, request is served from disk (no proxy).
	StaticDir string `json:"static_dir,omitempty"` // e.g. "/var/www/html"
	// SPAFallback serves <StaticDir>/index.html (HTTP 200) for any request
	// that does not match an existing file. Required for client-side routers
	// (React Router, Vue Router, etc.) so deep links like /app/users work.
	SPAFallback bool `json:"spa_fallback,omitempty"`

	// Load balancing — if set, overrides TargetPort. Round-robin across these.
	// Form: ["127.0.0.1:3001", "127.0.0.1:3002"] or full URLs.
	Upstreams []string `json:"upstreams,omitempty"`

	// Health checks — if set, ring0 polls "<upstream>/HealthPath" every 5s
	// and removes unhealthy upstreams from rotation. Empty = no health check.
	HealthPath string `json:"health_path,omitempty"` // e.g. "/healthz"

	// Middleware
	Gzip            bool     `json:"gzip,omitempty"`
	BasicAuth       string   `json:"basic_auth,omitempty"`         // "user:password" (plaintext for now; comma-separate multi)
	AllowCIDRs      []string `json:"allow_cidrs,omitempty"`        // ["10.0.0.0/8", "1.2.3.4/32"]
	RateLimitPerSec int      `json:"rate_limit_per_sec,omitempty"` // tokens/sec per client IP; 0 = off
	CORSOrigins     []string `json:"cors_origins,omitempty"`       // ["*"] or ["https://app.example.com"]
}

// ServerConfig holds proxy-server-level settings persisted in state.json.
type ServerConfig struct {
	// TLS via Let's Encrypt (autocert). When Domains is non-empty, ring0
	// listens on :443 and serves auto-renewed certs. HTTP listener (default
	// :80 or --proxy) does ACME challenges + redirects to HTTPS.
	TLSEnabled bool     `json:"tls_enabled,omitempty"`
	TLSEmail   string   `json:"tls_email,omitempty"`    // contact for ACME
	TLSDomains []string `json:"tls_domains,omitempty"`  // ["example.com", "www.example.com"]
	TLSCertDir string   `json:"tls_cert_dir,omitempty"` // default: ~/.ring0/certs

	// Access log file path. Empty = no access log file (still in TUI).
	AccessLog string `json:"access_log,omitempty"` // e.g. "~/.ring0/access.log"
}
