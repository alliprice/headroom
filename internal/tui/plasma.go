package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	colorful "github.com/lucasb-eyer/go-colorful"

	tea "charm.land/bubbletea/v2"
)

// gradientRGB is a 256-entry lookup table of RGB values interpolated through
// the plasma color keypoints using HCL blending.
var gradientRGB [256][3]uint8

func init() {
	type keypoint struct {
		pos   float64
		color colorful.Color
	}

	rawKeypoints := []struct {
		pos float64
		hex string
	}{
		{0.000, "#1a0533"},
		{0.125, "#5B21B6"},
		{0.250, "#7C3AED"},
		{0.375, "#A855F7"},
		{0.500, "#EC4899"},
		{0.625, "#F472B6"},
		{0.750, "#2DD4BF"},
		{0.875, "#7C3AED"},
		{1.000, "#1a0533"},
	}

	keypoints := make([]keypoint, 0, len(rawKeypoints))
	for _, rk := range rawKeypoints {
		c, err := colorful.Hex(rk.hex)
		if err != nil {
			// Fall back to black on parse error - should never happen with
			// hard-coded literals.
			c = colorful.Color{}
		}
		keypoints = append(keypoints, keypoint{rk.pos, c})
	}

	for i := 0; i < 256; i++ {
		t := float64(i) / 255.0

		// Find the two bracketing keypoints.
		lo := keypoints[0]
		hi := keypoints[len(keypoints)-1]
		for j := 0; j < len(keypoints)-1; j++ {
			if t >= keypoints[j].pos && t <= keypoints[j+1].pos {
				lo = keypoints[j]
				hi = keypoints[j+1]
				break
			}
		}

		// Compute local interpolation factor within the segment.
		span := hi.pos - lo.pos
		var factor float64
		if span > 0 {
			factor = (t - lo.pos) / span
		}

		blended := lo.color.BlendHcl(hi.color, factor).Clamped()
		r, g, b := blended.RGB255()
		gradientRGB[i] = [3]uint8{r, g, b}
	}
}

// plasmaTickCmd returns a tea.Cmd that fires a sleepTickMsg after 100ms.
func plasmaTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return sleepTickMsg(t)
	})
}

// RenderLoadingFrame renders a loading animation frame with a plasma-colored
// ball on a katakana background, and border/title that fade in based on fadeT.
// bgGrid may be nil (graceful fallback to spaces).
func RenderLoadingFrame(bgGrid []bgCell, w, h, frame int, fadeT float64) string {
	t := float64(frame) * 0.08
	brightness := 0.85 + 0.15*math.Sin(t*0.3)

	// Logo lines (same as RenderPlasma but without the subtitle).
	logo := []string{
		"╭─────────╮",
		"│  ▄███▄  │",
		"│ ███████ │",
		"│  ▀███▀  │",
		"╰─────────╯",
		"",
		"h e a d r o o m",
	}

	// Ball characters set.
	ballChars := map[rune]bool{'▄': true, '█': true, '▀': true}

	// Border characters set.
	borderChars := map[rune]bool{'╭': true, '─': true, '╮': true, '│': true, '╰': true, '╯': true}

	// Compute widest line.
	logoWidth := 0
	for _, l := range logo {
		rw := len([]rune(l))
		if rw > logoWidth {
			logoWidth = rw
		}
	}
	logoStartX := (w - logoWidth) / 2
	logoStartY := (h - len(logo)) / 2

	// Build cell map tagging each logo cell as ball, border, or title.
	type cellType int
	const (
		cellBall cellType = iota
		cellBorder
		cellTitle
	)
	type logoCell struct{ y, x int }
	type logoCellInfo struct {
		ch   rune
		kind cellType
	}
	logoCells := make(map[logoCell]logoCellInfo)

	for dy, line := range logo {
		runes := []rune(line)
		lineStartX := logoStartX + (logoWidth-len(runes))/2
		for dx, r := range runes {
			if r == ' ' {
				continue
			}
			var kind cellType
			if ballChars[r] {
				kind = cellBall
			} else if borderChars[r] {
				kind = cellBorder
			} else {
				kind = cellTitle
			}
			logoCells[logoCell{logoStartY + dy, lineStartX + dx}] = logoCellInfo{r, kind}
		}
	}

	// Target colors.
	borderR, borderG, borderB := uint8(109), uint8(40), uint8(217)  // #6D28D9
	titleR, titleG, titleB := uint8(236), uint8(72), uint8(153)     // #EC4899

	var buf strings.Builder
	buf.Grow(w * h * 20)

	// Track previous foreground RGB to avoid redundant escape sequences.
	var prevFgR, prevFgG, prevFgB uint8
	prevFgSet := false

	emitFg := func(r, g, b uint8) {
		if !prevFgSet || r != prevFgR || g != prevFgG || b != prevFgB {
			fmt.Fprintf(&buf, "\x1b[38;2;%d;%d;%dm", r, g, b)
			prevFgR, prevFgG, prevFgB = r, g, b
			prevFgSet = true
		}
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Get bg cell info.
			var bgCh rune = ' '
			var bgGray uint8 = 35
			if bgGrid != nil && y*w+x < len(bgGrid) {
				cell := bgGrid[y*w+x]
				bgCh = cell.ch
				bgGray = cell.gray
			}

			if info, ok := logoCells[logoCell{y, x}]; ok {
				switch info.kind {
				case cellBall:
					// Compute plasma color (same formula as RenderPlasma).
					fx, fy := float64(x), float64(y)
					v := math.Sin(fx/16.0+t) +
						math.Sin(fy/12.0+t*0.7) +
						math.Sin((fx+fy)/18.0+t*0.5) +
						math.Sin(math.Sqrt(fx*fx+fy*fy)/14.0+t*0.3)
					idx := int((v + 4.0) / 8.0 * 255.0)
					if idx < 0 {
						idx = 0
					}
					if idx > 255 {
						idx = 255
					}
					rgb := gradientRGB[idx]
					pr := uint8(math.Min(float64(rgb[0])*brightness, 255))
					pg := uint8(math.Min(float64(rgb[1])*brightness, 255))
					pb := uint8(math.Min(float64(rgb[2])*brightness, 255))
					emitFg(pr, pg, pb)
					buf.WriteRune(info.ch)

				case cellBorder:
					// Interpolate foreground: bgGray → colorBorder (#6D28D9).
					fr := uint8(float64(bgGray) + fadeT*(float64(borderR)-float64(bgGray)))
					fg := uint8(float64(bgGray) + fadeT*(float64(borderG)-float64(bgGray)))
					fb := uint8(float64(bgGray) + fadeT*(float64(borderB)-float64(bgGray)))
					emitFg(fr, fg, fb)
					buf.WriteRune(info.ch)

				case cellTitle:
					// Interpolate foreground: bgGray → colorTitle (#EC4899).
					fr := uint8(float64(bgGray) + fadeT*(float64(titleR)-float64(bgGray)))
					fg := uint8(float64(bgGray) + fadeT*(float64(titleG)-float64(bgGray)))
					fb := uint8(float64(bgGray) + fadeT*(float64(titleB)-float64(bgGray)))
					emitFg(fr, fg, fb)
					buf.WriteRune(info.ch)
				}
			} else {
				// Regular background cell: render katakana char in its grayscale color.
				emitFg(bgGray, bgGray, bgGray)
				buf.WriteRune(bgCh)
			}
		}

		// Reset at end of every row; newline after every row except the last.
		buf.WriteString("\x1b[0m")
		prevFgSet = false
		if y < h-1 {
			buf.WriteByte('\n')
		}
	}

	return buf.String()
}

// RenderPlasma renders a full-screen plasma animation frame to a string
// ready for direct terminal output. The logo is centered and overlaid with
// a bright white foreground on top of the plasma background.
func RenderPlasma(width, height, frame int, subtitle string) string {
	t := float64(frame) * 0.08
	brightness := 0.85 + 0.15*math.Sin(t*0.3)

	// Logo lines - centered on the plasma canvas.
	logo := []string{
		"╭─────────╮",
		"│  ▄███▄  │",
		"│ ███████ │",
		"│  ▀███▀  │",
		"╰─────────╯",
		"",
		"h e a d r o o m",
		"",
		subtitle,
	}

	// Compute the widest logo line.
	logoWidth := 0
	for _, l := range logo {
		rw := len([]rune(l))
		if rw > logoWidth {
			logoWidth = rw
		}
	}
	logoStartX := (width - logoWidth) / 2
	logoStartY := (height - len(logo)) / 2

	// Build a map of (y, x) -> rune for non-space logo characters.
	type logoCell struct{ y, x int }
	logoCells := make(map[logoCell]rune)
	for dy, line := range logo {
		runes := []rune(line)
		// Center each line within the overall logo width.
		lineStartX := logoStartX + (logoWidth-len(runes))/2
		for dx, r := range runes {
			if r != ' ' {
				logoCells[logoCell{logoStartY + dy, lineStartX + dx}] = r
			}
		}
	}

	var buf strings.Builder
	buf.Grow(width * height * 20)

	var prevR, prevG, prevB uint8
	prevSet := false

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Compute plasma value for this cell.
			fx, fy := float64(x), float64(y)
			v := math.Sin(fx/16.0+t) +
				math.Sin(fy/12.0+t*0.7) +
				math.Sin((fx+fy)/18.0+t*0.5) +
				math.Sin(math.Sqrt(fx*fx+fy*fy)/14.0+t*0.3)

			idx := int((v + 4.0) / 8.0 * 255.0)
			if idx < 0 {
				idx = 0
			}
			if idx > 255 {
				idx = 255
			}

			rgb := gradientRGB[idx]
			r := uint8(math.Min(float64(rgb[0])*brightness, 255))
			g := uint8(math.Min(float64(rgb[1])*brightness, 255))
			b := uint8(math.Min(float64(rgb[2])*brightness, 255))

			// Emit background escape only when the color changes.
			if !prevSet || r != prevR || g != prevG || b != prevB {
				fmt.Fprintf(&buf, "\x1b[48;2;%d;%d;%dm", r, g, b)
				prevR, prevG, prevB = r, g, b
				prevSet = true
			}

			// Overlay logo character with bright-white foreground, or emit
			// a plain space for plain plasma cells.
			if ch, ok := logoCells[logoCell{y, x}]; ok {
				fmt.Fprintf(&buf, "\x1b[38;2;245;243;255m%c\x1b[39m", ch)
			} else {
				buf.WriteByte(' ')
			}
		}

		// Reset at end of every row; newline after every row except the last.
		buf.WriteString("\x1b[0m")
		prevSet = false
		if y < height-1 {
			buf.WriteByte('\n')
		}
	}

	return buf.String()
}
