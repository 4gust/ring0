package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nileshchoudhary/ring0/internal/proc"
	"github.com/nileshchoudhary/ring0/internal/store"
	"github.com/nileshchoudhary/ring0/internal/ui"
)

func main() {
	st, err := store.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		os.Exit(1)
	}
	pm := proc.NewManager()

	p := tea.NewProgram(ui.New(st, pm), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "ui:", err)
		os.Exit(1)
	}
}
