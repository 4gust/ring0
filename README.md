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

## Install

Requires Go 1.22+.

```bash
git clone <this repo>
cd ring0
go build -o ring0 ./cmd/ring0
./ring0
```

State is persisted to `~/.ring0/state.json`.

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
