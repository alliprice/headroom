package tui

import (
	"strings"
)

var sleepingFrames = [4][5]string{
	{
		"    zZz",
		"   zZ",
		"  z",
		"   ▐▛███▜▌",
		"  ▝▜█████▛▘",
	},
	{
		"     zZz",
		"    zZ",
		"   z",
		"   ▐▛███▜▌",
		"  ▝▜█████▛▘",
	},
	{
		"      zZz",
		"     zZ",
		"    z",
		"   ▐▛███▜▌",
		"  ▝▜█████▛▘",
	},
	{
		"     zZz",
		"    zZ",
		"   z",
		"   ▐▛███▜▌",
		"  ▝▜█████▛▘",
	},
}

var sleepingText = []string{
	"",
	"    Claude is sleeping...",
	"",
	"   Press any key to wake",
}

// RenderSleep returns the full sleep mode screen, centered in the given dimensions.
func RenderSleep(width, height, frameNum int) string {
	frame := sleepingFrames[frameNum%len(sleepingFrames)]

	totalLines := len(frame) + len(sleepingText)
	startY := (height - totalLines) / 2
	if startY < 0 {
		startY = 0
	}

	var b strings.Builder

	// Pad blank lines above
	for i := 0; i < startY; i++ {
		b.WriteString("\n")
	}

	// Draw the art frame lines with colors
	for i, line := range frame {
		// Center the line using rune-aware length for Unicode art characters
		pad := (width - len([]rune(line))) / 2
		if pad < 0 {
			pad = 0
		}
		padStr := strings.Repeat(" ", pad)

		if i < 3 {
			// z lines: blue bold
			b.WriteString(padStr + sleepZStyle.Render(line))
		} else {
			// logo lines: yellow/orange bold
			b.WriteString(padStr + sleepLogoStyle.Render(line))
		}
		b.WriteString("\n")
	}

	// Draw the text lines
	for _, line := range sleepingText {
		pad := (width - len(line)) / 2
		if pad < 0 {
			pad = 0
		}
		padStr := strings.Repeat(" ", pad)
		b.WriteString(padStr + sleepTextStyle.Render(line))
		b.WriteString("\n")
	}

	return b.String()
}
