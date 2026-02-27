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
			// Fall back to black on parse error — should never happen with
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

// RenderPlasma renders a full-screen plasma animation frame to a string
// ready for direct terminal output. The logo is centered and overlaid with
// a bright white foreground on top of the plasma background.
func RenderPlasma(width, height, frame int, subtitle string) string {
	t := float64(frame) * 0.08
	brightness := 0.85 + 0.15*math.Sin(t*0.3)

	// Logo lines — centered on the plasma canvas.
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
