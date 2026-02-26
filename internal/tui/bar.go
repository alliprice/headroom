package tui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Block characters by eighths for sub-cell precision
var blockChars = [9]string{" ", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}

// styleTag identifies a bar style for batching purposes.
type styleTag int

const (
	stFill styleTag = iota
	stOver
	stEmpty
)

var styleByTag = [3]lipgloss.Style{barFillStyle, barOverStyle, barEmptyStyle}

// RenderBar returns a lipgloss-styled string representing a usage bar with
// a glide slope marker. width is in terminal columns.
//
// The bar uses foreground-colored Unicode block characters for sub-cell
// precision rather than background-colored spaces. The glide marker always
// takes priority over fill characters.
func RenderBar(width int, usagePct, glidePct float64) string {
	if width < 3 {
		return ""
	}

	usageEighths := clamp(int(math.Round(usagePct/100*float64(width)*8)), 0, width*8)
	glidePos := clamp(int(math.Round(glidePct/100*float64(width))), 0, width-1)

	fullCells := usageEighths / 8
	partialEighths := usageEighths % 8

	var b strings.Builder
	var curTag styleTag
	var curSegment strings.Builder
	segActive := false

	flushSegment := func() {
		if curSegment.Len() > 0 {
			b.WriteString(styleByTag[curTag].Render(curSegment.String()))
			curSegment.Reset()
		}
		segActive = false
	}

	appendCell := func(ch string, tag styleTag) {
		if segActive && curTag == tag {
			curSegment.WriteString(ch)
		} else {
			flushSegment()
			curTag = tag
			segActive = true
			curSegment.WriteString(ch)
		}
	}

	for i := 0; i < width; i++ {
		switch {
		case i == glidePos:
			// Glide marker always takes priority
			flushSegment()
			b.WriteString(barGlideStyle.Render("│"))

		case i < fullCells:
			// Full block cell
			if i < glidePos {
				appendCell("█", stFill)
			} else {
				appendCell("█", stOver)
			}

		case i == fullCells && partialEighths > 0:
			// Partial block cell (transition cell)
			if i < glidePos {
				appendCell(blockChars[partialEighths], stFill)
			} else {
				appendCell(blockChars[partialEighths], stOver)
			}

		default:
			// Empty cell
			appendCell(" ", stEmpty)
		}
	}

	flushSegment()

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
