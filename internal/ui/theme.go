package ui

import "github.com/charmbracelet/lipgloss"

// Catppuccin Mocha palette — STRICT. Do not introduce ad-hoc colors elsewhere.
// Reference: https://github.com/catppuccin/catppuccin
var (
	// Catppuccin Mocha base tokens
	mochaBase     = lipgloss.Color("#1e1e2e")
	mochaMantle   = lipgloss.Color("#181825")
	mochaSurface0 = lipgloss.Color("#313244")
	mochaSurface1 = lipgloss.Color("#45475a")
	mochaText     = lipgloss.Color("#cdd6f4")
	mochaSubtext0 = lipgloss.Color("#a6adc8")
	mochaOverlay0 = lipgloss.Color("#6c7086")
	mochaGreen    = lipgloss.Color("#a6e3a1")
	mochaYellow   = lipgloss.Color("#f9e2af")
	mochaRed      = lipgloss.Color("#f38ba8")
	mochaBlue     = lipgloss.Color("#89b4fa")
	mochaMauve    = lipgloss.Color("#cba6f7")

	// Semantic aliases used throughout the UI
	ColorGreen  = mochaGreen    // running / healthy
	ColorYellow = mochaYellow   // warning
	ColorRed    = mochaRed      // error / crashed
	ColorBlue   = mochaBlue     // selected / active
	ColorGray   = mochaOverlay0 // inactive
	ColorDim    = mochaSurface1
	ColorFg     = mochaText
	ColorBg     = mochaBase
	ColorPanel  = mochaMantle
	ColorAccent = mochaMauve // brand accent
)

// Reusable styles.
var (
	StyleBase = lipgloss.NewStyle().Foreground(ColorFg)

	StylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDim).
			Padding(0, 1)

	StylePanelActive = StylePanel.
				BorderForeground(ColorBlue)

	StyleTitle = lipgloss.NewStyle().
			Foreground(ColorBlue).
			Bold(true).
			Padding(0, 1)

	StyleTitleInactive = lipgloss.NewStyle().
				Foreground(ColorGray).
				Padding(0, 1)

	StyleSelected = lipgloss.NewStyle().
			Background(ColorBlue).
			Foreground(mochaBase).
			Bold(true)

	StyleDim     = lipgloss.NewStyle().Foreground(ColorGray)
	StyleOK      = lipgloss.NewStyle().Foreground(ColorGreen)
	StyleWarn    = lipgloss.NewStyle().Foreground(ColorYellow)
	StyleErr     = lipgloss.NewStyle().Foreground(ColorRed)
	StyleBadgePub = lipgloss.NewStyle().
			Foreground(mochaBase).
			Background(ColorGreen).
			Padding(0, 1).
			Bold(true)
	StyleBadgePriv = lipgloss.NewStyle().
			Foreground(ColorFg).
			Background(mochaSurface0).
			Padding(0, 1)

	StyleStatusBar = lipgloss.NewStyle().
			Background(mochaSurface0).
			Foreground(ColorFg).
			Padding(0, 1)

	StyleToastInfo = StyleStatusBar.
			Foreground(ColorBlue)
	StyleToastWarn = StyleStatusBar.
			Foreground(ColorYellow)
	StyleToastErr = StyleStatusBar.
			Foreground(ColorRed)
	StyleToastOK = StyleStatusBar.
			Foreground(ColorGreen)
)
