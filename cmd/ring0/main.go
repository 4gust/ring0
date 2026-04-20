package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/4gust/ring0/internal/proc"
	"github.com/4gust/ring0/internal/proxy"
	"github.com/4gust/ring0/internal/store"
	"github.com/4gust/ring0/internal/ui"
)

func main() {
	defaultAddr := os.Getenv("RING0_PROXY_ADDR")
	if defaultAddr == "" {
		defaultAddr = ":80"
	}
	addr := flag.String("proxy", defaultAddr,
		"reverse-proxy listen address (default :80, falls back to :8080 if :80 needs root). "+
			"Pass --proxy='' to disable. Env: RING0_PROXY_ADDR")
	headless := flag.Bool("headless", false,
		"run proxy + apps in background, no TUI (for systemd/pm2-style use). "+
			"Apps marked auto_start in state.json will be launched.")
	flag.Parse()

	st, err := store.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		os.Exit(1)
	}
	pm := proc.NewManager()

	var px *proxy.Server
	if *addr != "" {
		ln, bound := tryListen(*addr)
		if ln == nil && *addr == ":80" {
			fmt.Fprintln(os.Stderr, "proxy: :80 unavailable (needs root or setcap), falling back to :8080")
			ln, bound = tryListen(":8080")
		}
		if ln != nil {
			px = proxy.New(bound)
			px.SetConfig(st.Server)
			px.Reload(st.ListRoutes())
			go func() {
				if err := px.Serve(ln); err != nil {
					fmt.Fprintln(os.Stderr, "proxy:", err)
				}
			}()
			fmt.Fprintln(os.Stderr, "proxy: listening on", bound)
		} else {
			fmt.Fprintln(os.Stderr, "proxy: could not bind", *addr, "— continuing without proxy")
		}
	}

	if *headless {
		runHeadless(st, pm, px)
		return
	}

	p := tea.NewProgram(ui.New(st, pm, px), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "ui:", err)
		os.Exit(1)
	}
	if px != nil {
		px.Stop()
	}
}

func runHeadless(st *store.Store, pm *proc.Manager, px *proxy.Server) {
	apps := st.ListApps()
	for _, a := range apps {
		if err := pm.Start(a); err != nil {
			fmt.Fprintln(os.Stderr, "start", a.Name+":", err)
		} else {
			fmt.Fprintln(os.Stderr, "started", a.Name)
		}
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	fmt.Fprintln(os.Stderr, "ring0: headless mode — Ctrl-C / SIGTERM to stop, SIGHUP to reload state.json")
	for s := range sigs {
		if s == syscall.SIGHUP {
			fresh, err := store.New()
			if err != nil {
				fmt.Fprintln(os.Stderr, "reload:", err)
				continue
			}
			if px != nil {
				px.SetConfig(fresh.Server)
				px.Reload(fresh.ListRoutes())
				fmt.Fprintln(os.Stderr, "ring0: reloaded routes from state.json")
			}
			continue
		}
		break
	}
	fmt.Fprintln(os.Stderr, "ring0: shutting down…")
	for _, a := range apps {
		_ = pm.Stop(a)
	}
	if px != nil {
		px.Stop()
	}
}

func tryListen(addr string) (net.Listener, string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, ""
	}
	return ln, ln.Addr().String()
}
