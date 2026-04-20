package main

import (
	"flag"
	"fmt"
	"net"
	"os"

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
			go func() {
				if err := px.Serve(ln); err != nil {
					fmt.Fprintln(os.Stderr, "proxy:", err)
				}
			}()
		} else {
			fmt.Fprintln(os.Stderr, "proxy: could not bind", *addr, "— continuing without proxy")
		}
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

func tryListen(addr string) (net.Listener, string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, ""
	}
	return ln, ln.Addr().String()
}
