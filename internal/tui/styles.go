package tui

import "charm.land/lipgloss/v2"

// Color palette
var (
	colorBarEmpty = lipgloss.Color("#1E1028") // Dark purple-black
	colorGlide    = lipgloss.Color("#F5F3FF") // Bright white - glide marker
	colorBorder   = lipgloss.Color("#6D28D9") // Violet - panel border
	colorTitle    = lipgloss.Color("#EC4899") // Coral pink - titles
	colorNormal   = lipgloss.Color("#F5F3FF") // Off-white
	colorDim      = lipgloss.Color("#A78BFA") // Lavender
	colorError    = lipgloss.Color("#EF4444") // Red
	colorStatusBg = lipgloss.Color("#1E1028") // Dark purple - status bar bg
	colorBrand    = lipgloss.Color("#7C3AED") // Violet - brand accent
)

var (
	// Bar styles (used by bar.go)
	barEmptyStyle = lipgloss.NewStyle().Foreground(colorBarEmpty).Background(colorBarEmpty)

	// Text styles (used by view.go)
	titleStyle  = lipgloss.NewStyle().Foreground(colorTitle).Bold(true)
	normalStyle = lipgloss.NewStyle().Foreground(colorNormal)
	boldStyle   = lipgloss.NewStyle().Foreground(colorNormal).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(colorDim)
	errorStyle  = lipgloss.NewStyle().Foreground(colorError)

	// Panel styles (used by view.go)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)

	// Status bar styles (used by view.go)
	statusBarStyle = lipgloss.NewStyle().
			Background(colorStatusBg).
			Foreground(colorDim)

	statusBrandStyle = lipgloss.NewStyle().
				Background(colorBrand).
				Foreground(colorNormal).
				Bold(true).
				Padding(0, 1)

	// Help styles (used by bubbles/help in status bar)
	helpKeyStyle  = lipgloss.NewStyle().Foreground(colorNormal).Bold(true).Background(colorStatusBg)
	helpDescStyle = lipgloss.NewStyle().Foreground(colorDim).Background(colorStatusBg)
	helpSepStyle  = lipgloss.NewStyle().Foreground(colorDim).Background(colorStatusBg)

	// Trash zone styles
	trashStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Foreground(colorDim).
			Padding(0, 1)

	trashActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorError).
				Foreground(colorError).
				Padding(0, 1)
)
