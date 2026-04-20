package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/4gust/ring0/internal/proc"
	"github.com/4gust/ring0/internal/proxy"
	"github.com/4gust/ring0/internal/store"
	"github.com/4gust/ring0/internal/ui"
)

func main() {
	addr := flag.String("proxy", os.Getenv("RING0_PROXY_ADDR"),
		"reverse-proxy listen address, e.g. :8080 or 0.0.0.0:80 (env RING0_PROXY_ADDR). Empty disables.")
	flag.Parse()

	st, err := store.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		os.Exit(1)
	}
	pm := proc.NewManager()

	var px *proxy.Server
	if *addr != "" {
		px = proxy.New(*addr)
		go func() {
			if err := px.Start(); err != nil {
				fmt.Fprintln(os.Stderr, "proxy:", err)
			}
		}()
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
