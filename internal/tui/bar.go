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
		return uint8(float64(x)*(1-t) + float64(y)*t + 0.5)
	}
	return lerp(a[0], b[0], t), lerp(a[1], b[1], t), lerp(a[2], b[2], t)
}

// RenderBar returns a string representing a usage bar with a glide slope
// marker. The fill uses a gradient: purple→pink before the glide marker,
// yellow→orange after. Width is in terminal columns. glideOpacity controls
// the visibility of the glide marker: 0.0 = invisible (matches bar background),
// 1.0 = fully visible in the normal glide color.
func RenderBar(width int, usagePct, glidePct, glideOpacity float64) string {
	if width < 3 {
		return ""
	}

	usageEighths := clamp(int(math.Round(usagePct/100*float64(width)*8)), 0, width*8)
	glidePos := clamp(int(math.Round(glidePct/100*float64(width))), 0, width-1)

	fullCells := usageEighths / 8
	partialEighths := usageEighths % 8

	var buf strings.Builder
	buf.Grow(width * 30) // ~30 bytes per cell with lipgloss styles

	emptyStart := -1 // track run of empty cells for batching

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
		case i == glidePos:
			flushEmpty(i)
			// Interpolate glide foreground: barEmpty (#1E1028, 30,16,40) → glide (#F5F3FF, 245,243,255)
			gr := uint8(30 + glideOpacity*(245-30))
			gg := uint8(16 + glideOpacity*(243-16))
			gb := uint8(40 + glideOpacity*(255-40))
			fgHex := fmt.Sprintf("#%02x%02x%02x", gr, gg, gb)
			buf.WriteString(
				lipgloss.NewStyle().
					Foreground(lipgloss.Color(fgHex)).
					Background(colorBarEmpty).
					Bold(true).
					Render("│"),
			)

		case i < fullCells:
			flushEmpty(i)
			r, g, b := barGradientColor(i, glidePos, fullCells, width)
			hexColor := fmt.Sprintf("#%02x%02x%02x", r, g, b)
			buf.WriteString(
				lipgloss.NewStyle().
					Foreground(lipgloss.Color(hexColor)).
					Background(colorBarEmpty).
					Render("█"),
			)

		case i == fullCells && partialEighths > 0:
			flushEmpty(i)
			r, g, b := barGradientColor(i, glidePos, fullCells, width)
			hexColor := fmt.Sprintf("#%02x%02x%02x", r, g, b)
			buf.WriteString(
				lipgloss.NewStyle().
					Foreground(lipgloss.Color(hexColor)).
					Background(colorBarEmpty).
					Render(blockChars[partialEighths]),
			)

		default:
			// Accumulate empty cells for batched render
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

// easeOutCubic applies a cubic ease-out curve to t in [0,1].
func easeOutCubic(t float64) float64 {
	t -= 1
	return t*t*t + 1
}
