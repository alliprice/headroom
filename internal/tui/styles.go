package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Bar segment styles
	barBlue   = lipgloss.NewStyle().Background(lipgloss.Color("4"))                                                      // Blue - usage within glide slope
	barYellow = lipgloss.NewStyle().Background(lipgloss.Color("3")).Foreground(lipgloss.Color("0"))                      // Yellow - usage exceeding glide slope
	barEmpty  = lipgloss.NewStyle().Background(lipgloss.Color("0"))                                                      // Black/dark empty portion
	barMarkerOverGlide  = lipgloss.NewStyle().Background(lipgloss.Color("4")).Foreground(lipgloss.Color("15")).Bold(true) // Marker on blue bg
	barMarkerUnderGlide = lipgloss.NewStyle().Background(lipgloss.Color("0")).Foreground(lipgloss.Color("15")).Bold(true) // Marker on black bg

	// Text styles
	titleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)  // Cyan bold
	normalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))            // White
	boldStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true) // White bold
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))             // Dim white
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))             // Red

	// Sleep mode styles
	sleepZStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true) // Blue z's
	sleepLogoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true) // Yellow/orange logo
	sleepTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))            // Dim text
)
