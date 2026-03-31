package tui

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
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

)

// lerpRGB linearly interpolates between two RGB colors at fraction t in [0,1].
func lerpRGB(a, b [3]uint8, t float64) (uint8, uint8, uint8) {
	lerp := func(x, y uint8, t float64) uint8 {
		// Separate multiplications prevent FMA from altering rounding
		// across ARM/x86. Both paths must produce identical uint8 results
		// for golden-file tests to be cross-platform.
		lo := float64(x) * (1 - t)
		hi := float64(y) * t
		return uint8(math.Round(lo + hi))
	}
	return lerp(a[0], b[0], t), lerp(a[1], b[1], t), lerp(a[2], b[2], t)
}

// RenderBar returns a string representing a usage bar with a glide slope
// marker. The fill uses a gradient: purple→pink before the glide marker,
// yellow→orange after. Width is in terminal columns. glideOpacity controls
// the visibility of the glide marker: 0.0 = invisible (matches bar background),
// 1.0 = fully visible in the normal glide color.
func RenderBar(width int, usagePct, glidePct, glideOpacity, sleepAdjGlide float64) string {
	if width < 3 {
		return ""
	}

	usageEighths := clamp(int(math.Round(usagePct/100*float64(width)*8)), 0, width*8)
	glidePos := clamp(int(math.Round(glidePct/100*float64(width))), 0, width-1)
	sleepAdjPos := clamp(int(math.Round(sleepAdjGlide/100*float64(width))), 0, width-1)

	fullCells := usageEighths / 8
	partialEighths := usageEighths % 8

	var buf strings.Builder
	buf.Grow(width * 30)

	emptyStart := -1

	flushEmpty := func(end int) {
		if emptyStart >= 0 {
			count := end - emptyStart
			if count > 0 {
				buf.WriteString(barEmptyStyle.Render(strings.Repeat(" ", count)))
			}
			emptyStart = -1
		}
	}

	for i := 0; i < width; i++ {
		switch {
		case i == glidePos && glideOpacity > 0:
			flushEmpty(i)
			gr, gg, gb := lerpRGB(rgbBarEmpty, rgbGlide, glideOpacity)
			fgHex := fmt.Sprintf("#%02x%02x%02x", gr, gg, gb)
			buf.WriteString(
				lipgloss.NewStyle().
					Foreground(lipgloss.Color(fgHex)).
					Background(colorBarEmpty).
					Bold(true).
					Render("\u2502"),
			)

		case i == sleepAdjPos && sleepAdjPos != glidePos:
			flushEmpty(i)
			buf.WriteString(
				lipgloss.NewStyle().
					Foreground(colorError).
					Background(colorBarEmpty).
					Render("\u250a"),
			)

		case i < fullCells:
			flushEmpty(i)
			r, g, b := barGradientColor(i, glidePos, sleepAdjPos, fullCells)
			hexColor := fmt.Sprintf("#%02x%02x%02x", r, g, b)
			buf.WriteString(
				lipgloss.NewStyle().
					Foreground(lipgloss.Color(hexColor)).
					Background(colorBarEmpty).
					Render("\u2588"),
			)

		case i == fullCells && partialEighths > 0:
			flushEmpty(i)
			r, g, b := barGradientColor(i, glidePos, sleepAdjPos, fullCells)
			hexColor := fmt.Sprintf("#%02x%02x%02x", r, g, b)
			buf.WriteString(
				lipgloss.NewStyle().
					Foreground(lipgloss.Color(hexColor)).
					Background(colorBarEmpty).
					Render(blockChars[partialEighths]),
			)

		default:
			if emptyStart < 0 {
				emptyStart = i
			}
		}
	}

	flushEmpty(width)

	return buf.String()
}

// barGradientColor returns the RGB color for a filled cell at position i.
// Before the glide marker: purple→pink. After: yellow→orange.
func barGradientColor(i, glidePos, sleepAdjPos, fillEnd int) (uint8, uint8, uint8) {
	if i < glidePos {
		t := 0.0
		if glidePos > 0 {
			t = float64(i) / float64(glidePos)
		}
		return lerpRGB(gradUnderStart, gradUnderEnd, t)
	}
	if sleepAdjPos > glidePos && i < sleepAdjPos {
		span := sleepAdjPos - glidePos
		t := float64(i-glidePos) / float64(span)
		if t > 1 {
			t = 1
		}
		return lerpRGB(gradOverStart, gradOverEnd, t)
	}
	if sleepAdjPos > glidePos {
		span := fillEnd - sleepAdjPos
		if span <= 0 {
			return gradOverEnd[0], gradOverEnd[1], gradOverEnd[2]
		}
		t := float64(i-sleepAdjPos) / float64(span)
		if t > 1 {
			t = 1
		}
		return lerpRGB(gradOverEnd, rgbError, t)
	}
	// sleepAdjPos <= glidePos: two-zone fallback
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

