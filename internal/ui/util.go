package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Truncate returns s shortened to width with an ellipsis when needed.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	r := []rune(s)
	if len(r) <= width-1 {
		return s
	}
	return string(r[:width-1]) + "…"
}

// PadRight pads s with spaces to width (no truncation).
func PadRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// Dot returns a colored bullet for a status.
func Dot(color lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(color).Render("●")
}
