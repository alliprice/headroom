package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// katakana holds the half-width katakana character set (U+FF66–U+FF9D, 56 chars).
var katakana = []rune{
	'ｦ', 'ｧ', 'ｨ', 'ｩ', 'ｪ', 'ｫ', 'ｬ', 'ｭ', 'ｮ', 'ｯ',
	'ｰ', 'ｱ', 'ｲ', 'ｳ', 'ｴ', 'ｵ', 'ｶ', 'ｷ', 'ｸ', 'ｹ',
	'ｺ', 'ｻ', 'ｼ', 'ｽ', 'ｾ', 'ｿ', 'ﾀ', 'ﾁ', 'ﾂ', 'ﾃ',
	'ﾄ', 'ﾅ', 'ﾆ', 'ﾇ', 'ﾈ', 'ﾉ', 'ﾊ', 'ﾋ', 'ﾌ', 'ﾍ',
	'ﾎ', 'ﾏ', 'ﾐ', 'ﾑ', 'ﾒ', 'ﾓ', 'ﾔ', 'ﾕ', 'ﾖ', 'ﾗ',
	'ﾘ', 'ﾙ', 'ﾚ', 'ﾛ', 'ﾜ', 'ﾝ',
}

// noise2hash maps integer lattice coordinates to a pseudo-random float64 in [0,1].
func noise2hash(ix, iy int) float64 {
	h := ix*374761393 + iy*668265263
	h ^= h >> 13
	h *= 1274126177
	h ^= h >> 16
	// Mask to 31 bits to keep positive before dividing.
	return float64(h&0x7fffffff) / float64(0x7fffffff)
}

// smoothstep applies the classic cubic smoothstep curve to t.
func smoothstep(t float64) float64 {
	return t * t * (3 - 2*t)
}

// valueNoise2D samples bilinearly-interpolated 2D value noise at (x, y).
// Returns a value in [0, 1].
func valueNoise2D(x, y float64) float64 {
	ix := int(math.Floor(x))
	iy := int(math.Floor(y))
	fx := x - math.Floor(x)
	fy := y - math.Floor(y)

	ux := smoothstep(fx)
	uy := smoothstep(fy)

	v00 := noise2hash(ix, iy)
	v10 := noise2hash(ix+1, iy)
	v01 := noise2hash(ix, iy+1)
	v11 := noise2hash(ix+1, iy+1)

	// Bilinear interpolation.
	top := v00*(1-ux) + v10*ux
	bot := v01*(1-ux) + v11*ux
	return top*(1-uy) + bot*uy
}

// fbm2D computes fractal Brownian motion at (x, y) using the given number of
// octaves. Returns a value normalized to [0, 1].
func fbm2D(x, y float64, octaves int) float64 {
	value := 0.0
	amplitude := 0.5
	frequency := 1.0
	maxValue := 0.0

	for i := 0; i < octaves; i++ {
		value += valueNoise2D(x*frequency, y*frequency) * amplitude
		maxValue += amplitude
		amplitude *= 0.5
		frequency *= 2.0
	}

	return value / maxValue
}

// bgCell holds the pre-computed background character and grayscale brightness
// for a single terminal cell.
type bgCell struct {
	ch   rune
	gray uint8
}

// generateBgGrid builds a flat w*h slice of bgCell values using FBM noise.
func generateBgGrid(w, h int) []bgCell {
	grid := make([]bgCell, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			nx := float64(x) * 0.10
			ny := float64(y) * 0.20

			noise := fbm2D(nx, ny, 4)

			// Map [0,1] → [26, 45].
			gray := uint8(26 + int(math.Round(noise*float64(45-26))))

			// Independent hash for character selection using a different seed.
			charHash := noise2hash(x*1000003+7, y*999983+13)
			charIdx := int(charHash*float64(len(katakana))) % len(katakana)
			if charIdx < 0 {
				charIdx = 0
			}

			grid[y*w+x] = bgCell{
				ch:   katakana[charIdx],
				gray: gray,
			}
		}
	}
	return grid
}

// renderBgSegment renders a horizontal run of background cells starting at
// column xStart with the given count, from row y of the grid. ANSI color
// escape sequences are only emitted when the gray value changes. Returns an
// empty string when count <= 0.
func renderBgSegment(grid []bgCell, gridW, y, xStart, count int) string {
	if count <= 0 {
		return ""
	}

	var buf strings.Builder
	buf.Grow(count * 16)

	var prevGray uint8
	prevSet := false

	for i := 0; i < count; i++ {
		x := xStart + i
		// Wrap x into the grid width so the background tiles horizontally.
		gx := x % gridW
		if gx < 0 {
			gx += gridW
		}
		// Wrap y into the grid height so the background tiles vertically.
		gy := y % len(grid) // fallback guard
		if gridW > 0 {
			gy = (y % (len(grid) / gridW))
		}
		if gy < 0 {
			gy += len(grid) / gridW
		}

		cell := grid[gy*gridW+gx]

		if !prevSet || cell.gray != prevGray {
			g := cell.gray
			fmt.Fprintf(&buf, "\x1b[38;2;%d;%d;%dm", g, g, g)
			prevGray = cell.gray
			prevSet = true
		}

		buf.WriteRune(cell.ch)
	}

	buf.WriteString("\x1b[0m")
	return buf.String()
}

// compositeWithBackground composites the panels, error line, and status bar
// over the pre-computed background grid.
func compositeWithBackground(panelsStr, errorLine, statusBar string, grid []bgCell, gridW, gridH, screenW, screenH int) string {
	panelLines := strings.Split(panelsStr, "\n")

	// Find the visual width of the panels from the first non-empty line.
	panelVisualWidth := 0
	for _, line := range panelLines {
		if strings.TrimSpace(line) != "" {
			w := lipgloss.Width(line)
			if w > 0 {
				panelVisualWidth = w
				break
			}
		}
	}

	leftMargin := 0
	if panelVisualWidth > 0 && screenW > panelVisualWidth {
		leftMargin = (screenW - panelVisualWidth) / 2
	}

	var out strings.Builder
	out.Grow(screenW * screenH * 24)

	// Determine how many rows the error section consumes.
	errorRows := 0
	if errorLine != "" {
		errorRows = 2 // one for the error text, one blank
	}

	// Total content rows (everything except the status bar).
	contentRows := screenH - 1
	if contentRows < 0 {
		contentRows = 0
	}

	// Panel lines start after the error rows. Center the panels vertically in
	// the remaining space.
	remainingRows := contentRows - errorRows
	panelStartRow := errorRows
	if len(panelLines) < remainingRows {
		panelStartRow = errorRows + (remainingRows-len(panelLines))/2
	}
	panelEndRow := panelStartRow + len(panelLines) - 1

	panelLineIdx := 0

	for y := 0; y < contentRows; y++ {
		if y > 0 {
			out.WriteByte('\n')
		}

		// Error rows.
		if errorRows > 0 {
			if y == 0 {
				out.WriteString(errorLine)
				continue
			}
			if y == 1 {
				// Blank separator row — full background.
				out.WriteString(renderBgSegment(grid, gridW, y, 0, screenW))
				continue
			}
		}

		// Panel rows.
		if y >= panelStartRow && y <= panelEndRow {
			lineIdx := panelLineIdx
			panelLineIdx++

			if lineIdx < len(panelLines) {
				line := panelLines[lineIdx]
				if strings.TrimSpace(line) != "" && lipgloss.Width(line) > 0 {
					// Composite: left bg + panel + right bg.
					left := renderBgSegment(grid, gridW, y, 0, leftMargin)
					rightStart := leftMargin + panelVisualWidth
					rightCount := screenW - rightStart
					right := renderBgSegment(grid, gridW, y, rightStart, rightCount)
					out.WriteString(left)
					out.WriteString(line)
					out.WriteString(right)
					continue
				}
			}
		} else if y > panelEndRow {
			// Keep panelLineIdx advancing so we don't lose sync.
			// (Only matters if we re-enter panel range, which we don't.)
		}

		// Default: full-width background.
		out.WriteString(renderBgSegment(grid, gridW, y, 0, screenW))
	}

	// Final row: status bar.
	if screenH > 0 {
		out.WriteByte('\n')
		out.WriteString(statusBar)
	}

	return out.String()
}
