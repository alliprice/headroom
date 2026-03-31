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

// RGB triples for gradient interpolation. These must match their lipgloss
// counterparts above - when the theme system arrives, both flow from one source.
var (
	rgbBarEmpty = [3]uint8{0x1E, 0x10, 0x28} // matches colorBarEmpty
	rgbGlide    = [3]uint8{0xF5, 0xF3, 0xFF} // matches colorGlide
	rgbError    = [3]uint8{0xEF, 0x44, 0x44} // matches colorError
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

)
