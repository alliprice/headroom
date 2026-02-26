package tui

import (
	"fmt"
	"math"
	"strings"
)

// Block characters by eighths for sub-cell precision
var blockChars = [9]string{" ", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}

// Gradient endpoint colors as RGB triples.
var (
	// Under pace: purple → hot pink
	gradUnderStart = [3]uint8{168, 85, 247} // #A855F7
	gradUnderEnd   = [3]uint8{236, 72, 153} // #EC4899

	// Over pace: yellow → orange
	gradOverStart = [3]uint8{251, 191, 36} // #FBBF24
	gradOverEnd   = [3]uint8{249, 115, 22} // #F97316

	// Background for all bar cells
	barBgR, barBgG, barBgB uint8 = 30, 16, 40 // #1E1028
)

// lerpRGB linearly interpolates between two RGB colors at fraction t in [0,1].
func lerpRGB(a, b [3]uint8, t float64) (uint8, uint8, uint8) {
	lerp := func(x, y uint8, t float64) uint8 {
		return uint8(float64(x)*(1-t) + float64(y)*t + 0.5)
	}
	return lerp(a[0], b[0], t), lerp(a[1], b[1], t), lerp(a[2], b[2], t)
}

// RenderBar returns a string representing a usage bar with a glide slope
// marker. The fill uses a gradient: purple→pink before the glide marker,
// yellow→orange after. Width is in terminal columns.
func RenderBar(width int, usagePct, glidePct float64) string {
	if width < 3 {
		return ""
	}

	usageEighths := clamp(int(math.Round(usagePct/100*float64(width)*8)), 0, width*8)
	glidePos := clamp(int(math.Round(glidePct/100*float64(width))), 0, width-1)

	fullCells := usageEighths / 8
	partialEighths := usageEighths % 8

	var buf strings.Builder
	buf.Grow(width * 25) // ~25 bytes per cell with ANSI codes

	for i := 0; i < width; i++ {
		switch {
		case i == glidePos:
			// Glide marker: bright white on dark bg
			fmt.Fprintf(&buf, "\x1b[38;2;245;243;255;48;2;%d;%d;%dm│\x1b[0m",
				barBgR, barBgG, barBgB)

		case i < fullCells:
			// Full block with gradient color
			r, g, b := barGradientColor(i, glidePos, fullCells, width)
			fmt.Fprintf(&buf, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm█\x1b[0m",
				r, g, b, barBgR, barBgG, barBgB)

		case i == fullCells && partialEighths > 0:
			// Partial block (transition cell)
			r, g, b := barGradientColor(i, glidePos, fullCells, width)
			fmt.Fprintf(&buf, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm%s\x1b[0m",
				r, g, b, barBgR, barBgG, barBgB, blockChars[partialEighths])

		default:
			// Empty cell: dark bg
			fmt.Fprintf(&buf, "\x1b[48;2;%d;%d;%dm \x1b[0m",
				barBgR, barBgG, barBgB)
		}
	}

	return buf.String()
}

// barGradientColor returns the RGB color for a filled cell at position i.
// Before the glide marker: purple→pink. After: yellow→orange.
func barGradientColor(i, glidePos, fillEnd, width int) (uint8, uint8, uint8) {
	if i < glidePos {
		// Under pace gradient: purple → pink
		t := 0.0
		if glidePos > 0 {
			t = float64(i) / float64(glidePos)
		}
		return lerpRGB(gradUnderStart, gradUnderEnd, t)
	}
	// Over pace gradient: yellow → orange
	span := fillEnd - glidePos
	if span <= 0 {
		return gradOverStart[0], gradOverStart[1], gradOverStart[2]
	}
	t := float64(i-glidePos) / float64(span)
	if t > 1 {
		t = 1
	}
	return lerpRGB(gradOverStart, gradOverEnd, t)
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
