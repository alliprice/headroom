package tui

import (
	"math"
	"strings"
)

const markerChar = "|"
const fillChar = " "

// RenderBar returns a lipgloss-styled string representing a usage bar with
// a glide slope marker. width is in terminal columns.
//
// The bar is rendered as 3-4 batched styled segments rather than
// character-by-character, matching the Python draw_bar logic:
//
//   - Under/at glide slope: [blue fill][empty gap][marker on empty][empty tail]
//   - Over glide slope:     [blue fill][marker on blue][yellow fill][empty tail]
func RenderBar(width int, usagePct, glidePct float64) string {
	if width < 3 {
		return ""
	}

	usagePos := clamp(int(math.Round(usagePct/100*float64(width))), 0, width)
	glidePos := clamp(int(math.Round(glidePct/100*float64(width))), 0, width-1)

	var b strings.Builder

	if usagePct > glidePct {
		// Over glide slope: [blue fill][marker on blue][yellow fill][empty]
		// Blue portion: 0 to glidePos
		if glidePos > 0 {
			b.WriteString(barBlue.Render(strings.Repeat(fillChar, glidePos)))
		}
		// Marker at glidePos (on blue bg because we're over)
		b.WriteString(barMarkerOverGlide.Render(markerChar))
		// Yellow portion: glidePos+1 to usagePos
		yellowLen := usagePos - glidePos - 1
		if yellowLen > 0 {
			b.WriteString(barYellow.Render(strings.Repeat(fillChar, yellowLen)))
		}
		// Empty portion: usagePos to width
		emptyLen := width - usagePos
		if emptyLen > 0 {
			b.WriteString(barEmpty.Render(strings.Repeat(fillChar, emptyLen)))
		}
	} else {
		// Under/at glide slope: [blue fill][empty gap][marker on empty][empty tail]
		// Blue portion: 0 to usagePos
		if usagePos > 0 {
			b.WriteString(barBlue.Render(strings.Repeat(fillChar, usagePos)))
		}
		// Empty gap between usage and marker: usagePos to glidePos
		gapLen := glidePos - usagePos
		if gapLen > 0 {
			b.WriteString(barEmpty.Render(strings.Repeat(fillChar, gapLen)))
		}
		// Marker at glidePos (on empty/black bg because we're under)
		b.WriteString(barMarkerUnderGlide.Render(markerChar))
		// Remaining empty: glidePos+1 to width
		emptyLen := width - glidePos - 1
		if emptyLen > 0 {
			b.WriteString(barEmpty.Render(strings.Repeat(fillChar, emptyLen)))
		}
	}

	return b.String()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
