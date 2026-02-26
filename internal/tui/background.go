package tui

import (
	"fmt"
	"math"
	"strings"
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

// renderBackground renders the full w×h background grid as a single ANSI
// string suitable for use as a Canvas layer. Each cell uses a grayscale
// foreground color, with escapes only emitted when the color changes.
func renderBackground(grid []bgCell, w, h int) string {
	var buf strings.Builder
	buf.Grow(w * h * 16)

	var prevGray uint8
	prevSet := false

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cell := grid[y*w+x]
			if !prevSet || cell.gray != prevGray {
				fmt.Fprintf(&buf, "\x1b[38;2;%d;%d;%dm", cell.gray, cell.gray, cell.gray)
				prevGray = cell.gray
				prevSet = true
			}
			buf.WriteRune(cell.ch)
		}
		buf.WriteString("\x1b[0m")
		prevSet = false
		if y < h-1 {
			buf.WriteByte('\n')
		}
	}

	return buf.String()
}
