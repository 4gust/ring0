# ring0

A modern terminal UI for managing local applications and routes — inspired by **htop** (dense, real-time data), **lazygit** (panel navigation), and **k9s** (resource UX).

```
┌─ Applications ─────────────┐ ┌─ System Monitor ───────────┐
│  NAME    STATUS  PID  PORT │ │ CPU [████████░░] 42.1%     │
│  api     ● running 1234 80 │ │ MEM [██████░░░░] 61.0% ...  │
│  worker  ● stopped  -   -  │ │ ▂▃▅▇▆▅▄▃▂▂▃ ▅▆▇█           │
└────────────────────────────┘ └────────────────────────────┘
┌─ Routes ───────────────────┐ ┌─ Logs ─────────────────────┐
│  /api      :3000   PUBLIC  │ │ 14:02:11 listening on :80  │
│  /admin    :4000   PRIVATE │ │ 14:02:14 GET /api/v1/...   │
└────────────────────────────┘ └────────────────────────────┘
 ✔ added api                                  a:add  s:start ...
```

## Install & Run

### Requirements

- **Go 1.22+** (`go version`)
- A 256-color terminal — iTerm2, kitty, Alacritty, WezTerm, or modern Terminal.app / GNOME Terminal all work
- Minimum terminal size: ~80×24 (larger is better)
- macOS, Linux, or Windows

### Option 1 — Install with `go install`

```bash
go install github.com/4gust/ring0/cmd/ring0@latest
ring0
```

This drops the `ring0` binary into `$(go env GOPATH)/bin`. Make sure that's on your `PATH`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"   # add to ~/.zshrc or ~/.bashrc
```

### Option 2 — Build from source

```bash
git clone https://github.com/4gust/ring0.git
cd ring0
go build -o ring0 ./cmd/ring0
./ring0
```

### Option 3 — Run without building

```bash
git clone https://github.com/4gust/ring0.git
cd ring0
go run ./cmd/ring0
```

### Install system-wide (optional)

```bash
sudo mv ring0 /usr/local/bin/
ring0
```

### Verify it works

When you launch `ring0` you should see four bordered panels (Applications, Routes, System Monitor, Logs) and a footer with context-sensitive keys. If colors look off, your terminal is probably not in 256-color/truecolor mode:

```bash
export COLORTERM=truecolor
```

### State & data

- Config + state file: `~/.ring0/state.json` (created on first run)
- Logs: in-memory ring buffer per app (last 2000 lines) — not persisted to disk

To reset everything:

```bash
rm -rf ~/.ring0
```

### Updating

```bash
go install github.com/4gust/ring0/cmd/ring0@latest
# or, if you cloned:
cd ring0 && git pull && go build -o ring0 ./cmd/ring0
```

### Uninstall

```bash
rm "$(go env GOPATH)/bin/ring0"   # or /usr/local/bin/ring0
rm -rf ~/.ring0
```

### Troubleshooting

| Symptom | Fix |
|---|---|
| `command not found: ring0` | Add `$(go env GOPATH)/bin` to `PATH` |
| Garbled borders / wrong colors | Use a truecolor terminal; `export COLORTERM=truecolor` |
| App stuck in `crashed` | Check logs panel (`l`) for the exit message |
| `port already in use` toast | Pick a different port or stop the conflicting app |
| State seems wrong | `rm -rf ~/.ring0` to start fresh |

## Reverse proxy (nginx replacement)

ring0 ships a built-in HTTP reverse proxy that routes by **path prefix** + optional **host header** to your local apps. One public port, many backends — no nginx needed.

### Quick start

```bash
# Start ring0 with the proxy listening on :8080
ring0 --proxy :8080
# or:
RING0_PROXY_ADDR=:8080 ring0
```

Add a route from the UI: press `2` (Routes) → `a` (add). Example:

```
Path     /api
Host     (empty = match any host)
Port     3001
Vis      public
Strip    n
Redirect (empty)
```

That sends every request matching `/api` or `/api/*` to `http://127.0.0.1:3001`.
The route reload is live — no restart needed.

### Open exactly one port (Azure / cloud)

```bash
az vm open-port -g <RG> -n <VM> --port 8080 --priority 1010
```

Now `http://YOUR_VM_IP:8080/api/...` reaches your backend. Your app ports stay closed.

### Features (the nginx-equivalents)

| nginx feature | ring0 equivalent |
|---|---|
| `location /api { proxy_pass http://127.0.0.1:3001; }` | Route: `Path=/api`, `Port=3001` |
| `proxy_set_header Host $host;` | automatic |
| `proxy_set_header X-Real-IP $remote_addr;` | automatic |
| `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;` | automatic |
| `proxy_set_header X-Forwarded-Proto $scheme;` | automatic |
| `rewrite ^/api/(.*) /$1 break;` | Set `Strip = y` on the route |
| `proxy_http_version 1.1; Upgrade $http_upgrade;` (websockets) | automatic |
| `return 308 https://example.com$request_uri;` | Set `Redirect = https://example.com` on the route |
| `server_name api.example.com;` | Set `Host = api.example.com` on the route |
| `location /` (catch-all) | Route with `Path = /` |
| Reload config without dropping connections | UI add/edit/delete reloads atomically |

### Path matching rules

- **Longest prefix wins.** `/api/v2 → port 9000` is preferred over `/api → port 8000` even if both match.
- A route with `Path = /` is a catch-all (use it last; long prefixes still win).
- Path matches are exact-or-segment: `/api` matches `/api` and `/api/users`, but **not** `/apiary`.

### Strip vs no-strip

| Strip | Request `GET /api/users` becomes upstream… |
|---|---|
| `n` (default) | `GET /api/users` |
| `y` | `GET /users` |

Use `y` when your backend doesn't know about the `/api` prefix (most APIs).

### Redirects

Set `Redirect = https://example.com` and any matching path returns **HTTP 308 Permanent Redirect** to the URL. `Port` is ignored in this mode. Use it for:

- Forcing HTTPS: `Path = /`, `Redirect = https://example.com`
- Rebranding domains
- Sending old paths to new locations

### Host-based routing (virtual hosts)

```
Path = /        Host = api.example.com   Port = 3001
Path = /        Host = app.example.com   Port = 5173
```

Point both DNS records at your VM, set `--proxy :80` (needs root or `setcap`), and ring0 routes by `Host` header — same as nginx `server_name`.

### Binding to port 80 / 443 without root

```bash
sudo setcap 'cap_net_bind_service=+ep' "$(which ring0)"
ring0 --proxy :80
```

### WebSockets / SSE / HMR

Just work. The proxy uses Go's `httputil.ReverseProxy` which handles HTTP/1.1 `Upgrade` transparently. Vite HMR and Socket.io both pass through unchanged.

### Live status

The System Monitor panel (`3`) shows `proxy: :8080   hits: N` so you can confirm requests are arriving.

### What ring0 does **not** do (yet)

- HTTP/3 (QUIC) — needs heavy quic-go dep
- Brotli compression — needs cgo
- Response caching — easy to do wrong; defer to Caddy/CDN

Everything else nginx does is in. See **Power features** below.

## Power features (state.json schema)

Most advanced settings live in `~/.ring0/state.json`. Edit that file (ring0 reloads on next launch — or after route add/edit/delete from the UI). Every field is optional.

### Full route schema

```jsonc
{
  "id": "rt-abc",
  "path": "/api",
  "host": "api.example.com",          // optional virtual host
  "target_port": 3001,                 // simple single-upstream
  "visibility": "public",
  "strip_prefix": true,                // strip "/api" before forwarding
  "redirect": "",                      // if set, sends 308 to this URL

  // Static file serving (no proxy)
  "static_dir": "/var/www/html",       // serves files from disk

  // Load balancing (overrides target_port)
  "upstreams": ["127.0.0.1:3001", "127.0.0.1:3002"],
  "health_path": "/healthz",           // poll every 5s; remove failing upstreams

  // Middleware
  "gzip": true,
  "basic_auth": "alice:s3cret,bob:hunter2",
  "allow_cidrs": ["10.0.0.0/8", "1.2.3.4/32"],
  "rate_limit_per_sec": 10,            // per-client-IP token bucket
  "cors_origins": ["*"]                // or ["https://app.example.com"]
}
```

### Server-level config

Add to top of `~/.ring0/state.json`:

```jsonc
{
  "apps": [...],
  "routes": [...],
  "server": {
    "tls_enabled": true,
    "tls_email": "you@example.com",
    "tls_domains": ["example.com", "www.example.com"],
    "tls_cert_dir": "~/.ring0/certs",   // optional
    "access_log": "~/.ring0/access.log" // optional
  }
}
```

### Feature reference

#### TLS / HTTPS / HTTP/2

Enable `server.tls_enabled` with `tls_domains` pointed at your real DNS.
ring0 listens on **:443** for TLS (with auto HTTP/2 negotiation) and uses
your `--proxy` port for ACME challenges + an HTTP→HTTPS 301 redirect.

```bash
sudo setcap 'cap_net_bind_service=+ep' "$(which ring0)"   # one time
ring0 --proxy :80                                          # serves :80 + :443
```

Certs auto-renew. They live in `~/.ring0/certs` by default.

#### Static file serving

Set `static_dir` on a route. The route serves files from that directory
instead of proxying. Combine with `strip_prefix` to map `/v8/*` to a build:

```jsonc
{ "path": "/v8", "static_dir": "/var/www/v8-build", "strip_prefix": true }
```

#### Load balancing

Set `upstreams` to a list. Round-robin distribution. With `health_path` set,
ring0 polls each upstream every 5s and removes failing ones from rotation.
The Routes panel shows `✓host:port` / `✗host:port` per upstream.

```jsonc
{
  "path": "/",
  "upstreams": ["10.0.0.1:8080", "10.0.0.2:8080", "10.0.0.3:8080"],
  "health_path": "/healthz"
}
```

#### gzip

`"gzip": true` — only fires when the client sends `Accept-Encoding: gzip`.
Adds `Content-Encoding: gzip` and `Vary: Accept-Encoding`.

#### Basic auth

`"basic_auth": "user:password"` — multiple users with comma:
`"basic_auth": "alice:s3cret,bob:hunter2"`. Failed auth shows the anime
401 page with a `WWW-Authenticate` challenge.

#### IP allow-list

`"allow_cidrs": ["10.0.0.0/8", "192.168.1.0/24"]` — only listed CIDRs may
hit the route. `X-Forwarded-For` is honored if you're behind another LB.
Bare IPs auto-expand to `/32`.

#### Rate limiting

`"rate_limit_per_sec": 10` — per-client-IP token bucket (burst = 2× rate).
Excess requests get the anime 429 page + `Retry-After: 1`.

#### CORS

`"cors_origins": ["*"]` or `["https://app.example.com", "https://other.com"]`.
Sets `Access-Control-Allow-*` headers and short-circuits OPTIONS preflight
with 204.

#### Custom error pages

Already on by default — Catppuccin pixel-art mascots for 401, 403, 404, 429,
500, 502, 503. Includes path + upstream + detail. No config needed.

#### Access logs to disk

`"server.access_log": "~/.ring0/access.log"` — appends one line per request
in a Combined-ish format:
`2026-04-20T10:00:00Z 1.2.3.4 "GET /api/users HTTP/1.1" 200 1234 12ms "Mozilla/..."`

### Restart vs hot-reload

| Change | Reload mode |
|---|---|
| Add/edit/delete route in UI | Hot — no restart |
| Edit route fields in `state.json` | Restart `ring0` |
| Edit `server.*` (TLS, access_log) | Restart `ring0` |


## Layout

Four panels, always visible:

| Panel | What it shows |
|---|---|
| **Applications** | Your managed processes, status, PID, port |
| **Routes** | Path → port mappings with public/private badges |
| **System Monitor** | CPU + memory bars, sparklines, totals |
| **Logs** | Streaming stdout/stderr of the selected app |

The **active panel** is highlighted with a blue border.

## Color system (consistent, semantic)

| Color | Meaning |
|---|---|
| 🟢 Green | running / healthy |
| 🟡 Yellow | warning |
| 🔴 Red | error / crashed |
| 🔵 Blue | selected / active |
| ⚪ Gray | inactive |

## Keybindings

### Global
| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Next / previous panel |
| `↑` `↓` / `j` `k` | Move within panel |
| `PgUp` / `PgDn` | Jump 10 rows |
| `/` | Search (filters Apps / Routes) |
| `q` / `Ctrl+C` | Quit |

### Applications panel
| Key | Action |
|---|---|
| `a` | **Add** application (modal form) |
| `s` | **Start** selected app |
| `x` | Stop selected app |
| `r` | Restart selected app |
| `l` | Jump to Logs panel for selected app |
| `d` | Delete (with confirmation) |

### Routes panel
| Key | Action |
|---|---|
| `a` | Add route |
| `e` | Edit selected route |
| `d` | Delete (with confirmation) |

### Logs panel
| Key | Action |
|---|---|
| `f` | Toggle follow (tail -f) ON/OFF |
| `↑` / `↓` | Scroll (auto-disables follow) |
| `g` / `G` | Jump to top / bottom (resume follow) |

### Modal forms
| Key | Action |
|---|---|
| `Tab` / `↓` | Next field |
| `Shift+Tab` / `↑` | Previous field |
| `Enter` | Next field; on last field, **save** |
| `Esc` | Cancel |

### Confirm dialogs
| Key | Action |
|---|---|
| `y` | Confirm |
| `n` / `Esc` | Cancel |

## 2-minute walkthrough

1. **Add an app** — focus *Applications*, press `a`, fill in:
   - Name: `web`
   - Cmd: `python -m http.server 8000`
   - Port: `8000`
   - `Enter` through fields → save.
2. **Start it** — `s`. The status dot turns 🟢 green. A toast confirms `● web running (pid …)`.
3. **Watch logs** — `l` jumps to the Logs panel. Follow mode is ON by default.
4. **Add a route** — `Tab` to *Routes*, press `a`:
   - Path: `/`
   - Port: `8000`
   - Vis: `public` → save.
5. **Search** — `/` then type `web`. Lists filter live. `Esc` clears.
6. **Stop / restart** — back in Apps, `x` or `r`.
7. **Quit** — `q`.

## Inline feedback

ring0 surfaces state changes as **toast messages** in the bottom bar, color-coded by severity:

- ✔ green — success (`✔ added web`)
- ● blue — info (`■ web stopped`)
- ⚠ yellow — warning (`⚠ not running`)
- ✖ red — error (`✖ port 3000 already in use`, `✖ web crashed (exit 1)`)

Validation runs on submit and blocks the save with a red toast — no silent failures.

## Real-time behavior

- System metrics resample every 1s.
- Process status updates push immediately on start/exit/crash.
- Log lines stream as they arrive into a 2000-line ring buffer per app.
- No manual refresh, no full-screen redraw — Bubble Tea diffs the frame.

## Status indicators

- Apps: ● green = running, ● gray = stopped, ● red = crashed (with exit code)
- Routes: `PUBLIC` (green badge) / `PRIVATE` (gray badge), and the target port

## Empty states

When a panel has no data, ring0 tells you what to do:

- *Applications* → `No apps. Press 'a' to add one.`
- *Routes* → `No routes. Press 'a' to add one.`
- *Logs* (no selection) → `Select an app in the Applications panel to view logs.`
- *Logs* (no output yet) → `(no output yet)`

## Performance notes

- Lists are O(n) cursor moves; tested layout target: 50+ apps, 100+ routes.
- Log buffer is fixed-capacity ring (2000 lines/app) — bounded memory.
- No blocking I/O on the UI goroutine; process I/O runs in dedicated goroutines and pushes via buffered channels.

## Project layout

```
cmd/ring0/         entrypoint
internal/model/    domain types (App, Route, Status, Visibility)
internal/store/    JSON-backed persistence
internal/proc/     process supervisor + log ring buffer
internal/sysmon/   CPU/memory sampler (gopsutil)
internal/ui/       Bubble Tea model + Lipgloss theme
```

## Anti-patterns avoided

- ❌ No flickering — alt-screen + diff renderer.
- ❌ No full redraws on every tick — only metrics + toast expiry on the 1s tick.
- ❌ No blocking input — process I/O is fully async.
- ❌ No inconsistent keys — same nav keys everywhere; action keys are panel-scoped and shown in the footer.

## Roadmap (out of scope for this build)

- Actual reverse proxy enforcement of routes
- Light theme toggle
- Per-app env vars + restart-on-crash policy
- Export/import config
