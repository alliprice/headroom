package tui

import (
	"image"
	"time"

	tea "charm.land/bubbletea/v2"
)

// demoStep tracks the current phase of the auto-play demo sequence.
type demoStep int

const (
	demoIdle       demoStep = iota // not running
	demoWait1                      // pause after bars settle
	demoPanelSwap                  // drag top panel down to swap
	demoWait2                      // pause after swap
	demoBarReorder                 // drag a bar to a different slot
	demoWait3                      // pause after reorder
	demoBarTrash                   // drag a bar to the trash zone
	demoWait4                      // pause after trash
	demoRestore                    // reset layout (like pressing 0)
	demoWait5                      // pause after restore
	demoDone                       // quit
)

// demoTickMsg drives the demo state machine forward.
type demoTickMsg time.Time

// demoTickCmd returns a tea.Cmd that fires a demoTickMsg after 50ms.
func demoTickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return demoTickMsg(t)
	})
}

// demoAnimDuration is the number of frames for a drag animation (50ms per frame).
const demoAnimDuration = 20 // 20 frames = 1000ms

// demoWaitFrames converts a duration in milliseconds to frame count.
func demoWaitFrames(ms int) int {
	return ms / 50
}

// updateDemo dispatches to the current demo step handler.
func (m Model) updateDemo() (Model, tea.Cmd) {
	m.demoFrame++

	switch m.demoStep {
	case demoWait1:
		if m.demoFrame >= demoWaitFrames(1500) {
			m.demoStep = demoPanelSwap
			m.demoFrame = 0
		}
		return m, demoTickCmd()

	case demoPanelSwap:
		return m.demoPanelSwapStep()

	case demoWait2:
		if m.demoFrame >= demoWaitFrames(1000) {
			m.demoStep = demoBarReorder
			m.demoFrame = 0
		}
		return m, demoTickCmd()

	case demoBarReorder:
		return m.demoBarReorderStep()

	case demoWait3:
		if m.demoFrame >= demoWaitFrames(1000) {
			m.demoStep = demoBarTrash
			m.demoFrame = 0
		}
		return m, demoTickCmd()

	case demoBarTrash:
		return m.demoBarTrashStep()

	case demoWait4:
		if m.demoFrame >= demoWaitFrames(1500) {
			m.demoStep = demoRestore
			m.demoFrame = 0
		}
		return m, demoTickCmd()

	case demoRestore:
		return m.demoRestoreStep()

	case demoWait5:
		if m.demoFrame >= demoWaitFrames(2000) {
			m.demoStep = demoDone
			return m, tea.Quit
		}
		return m, demoTickCmd()

	default:
		return m, nil
	}
}

// demoPanelSwapStep drags the top panel downward past the midpoint to trigger a swap.
func (m Model) demoPanelSwapStep() (Model, tea.Cmd) {
	if len(m.layoutState.panelOrder) < 2 {
		// No second panel — skip to next step.
		m.demoStep = demoWait2
		m.demoFrame = 0
		return m, demoTickCmd()
	}

	topPanelID := m.layoutState.panelOrder[0]

	if m.demoFrame == 1 {
		// Start: grab the top panel center.
		var panelRect image.Rectangle
		if topPanelID == "claude" {
			panelRect = m.layout.claudePanel
		} else {
			panelRect = m.layout.codexPanel
		}
		start := rectCenter(panelRect)
		m.drag = dragState{
			phase:      dragActive,
			target:     dragTargetPanel,
			panelID:    topPanelID,
			ghostLabel: m.panelDisplayName(topPanelID),
			startX:     start.X, startY: start.Y,
			currX: start.X, currY: start.Y,
		}
		return m, demoTickCmd()
	}

	if m.demoFrame <= demoAnimDuration {
		// Animate: move cursor from top panel center toward bottom panel center.
		var topRect, botRect image.Rectangle
		if topPanelID == "claude" {
			topRect = m.layout.claudePanel
			botRect = m.layout.codexPanel
		} else {
			topRect = m.layout.codexPanel
			botRect = m.layout.claudePanel
		}
		startPt := rectCenter(topRect)
		endPt := rectCenter(botRect)
		t := easeOutCubic(float64(m.demoFrame-1) / float64(demoAnimDuration-1))
		m.drag.currX = startPt.X + int(t*float64(endPt.X-startPt.X))
		m.drag.currY = startPt.Y + int(t*float64(endPt.Y-startPt.Y))
		m.liveReorderPanel()
		return m, demoTickCmd()
	}

	// Done: release.
	m.drag = dragState{}
	m.demoStep = demoWait2
	m.demoFrame = 0
	return m, demoTickCmd()
}

// demoBarReorderStep drags the second bar in the top panel to the first slot.
func (m Model) demoBarReorderStep() (Model, tea.Cmd) {
	// Pick the top panel's bars.
	topPanelID := m.layoutState.panelOrder[0]
	var bars []barGeom
	if topPanelID == "claude" {
		bars = m.layout.claudeBars
	} else {
		bars = m.layout.codexBars
	}

	if len(bars) < 2 {
		// Not enough bars — skip.
		m.demoStep = demoWait3
		m.demoFrame = 0
		return m, demoTickCmd()
	}

	srcBar := bars[1] // second bar
	dstBar := bars[0] // first bar

	if m.demoFrame == 1 {
		start := rectCenter(srcBar.bounds)
		m.drag = dragState{
			phase:      dragActive,
			target:     dragTargetBar,
			barKey:     srcBar.key,
			ghostLabel: m.catDisplayName(srcBar.key),
			startX:     start.X, startY: start.Y,
			currX: start.X, currY: start.Y,
		}
		return m, demoTickCmd()
	}

	if m.demoFrame <= demoAnimDuration {
		startPt := rectCenter(srcBar.bounds)
		endPt := rectCenter(dstBar.bounds)
		t := easeOutCubic(float64(m.demoFrame-1) / float64(demoAnimDuration-1))
		m.drag.currX = startPt.X + int(t*float64(endPt.X-startPt.X))
		m.drag.currY = startPt.Y + int(t*float64(endPt.Y-startPt.Y))
		m.liveReorderBar()
		return m, demoTickCmd()
	}

	// Done: release.
	m.drag = dragState{}
	m.demoStep = demoWait3
	m.demoFrame = 0
	return m, demoTickCmd()
}

// demoBarTrashStep drags the last bar in the top panel to the trash zone.
func (m Model) demoBarTrashStep() (Model, tea.Cmd) {
	topPanelID := m.layoutState.panelOrder[0]
	var bars []barGeom
	if topPanelID == "claude" {
		bars = m.layout.claudeBars
	} else {
		bars = m.layout.codexBars
	}

	if len(bars) == 0 {
		m.demoStep = demoWait4
		m.demoFrame = 0
		return m, demoTickCmd()
	}

	trashBar := bars[len(bars)-1] // last bar
	tz := trashZoneRect(m.width, m.height)
	tzCenter := rectCenter(tz)

	if m.demoFrame == 1 {
		start := rectCenter(trashBar.bounds)
		m.drag = dragState{
			phase:      dragActive,
			target:     dragTargetBar,
			barKey:     trashBar.key,
			ghostLabel: m.catDisplayName(trashBar.key),
			startX:     start.X, startY: start.Y,
			currX: start.X, currY: start.Y,
		}
		// Store trash zone in layout so handleMouseUp can find it.
		m.layout.trashZone = tz
		return m, demoTickCmd()
	}

	if m.demoFrame <= demoAnimDuration {
		startPt := rectCenter(trashBar.bounds)
		t := easeOutCubic(float64(m.demoFrame-1) / float64(demoAnimDuration-1))
		m.drag.currX = startPt.X + int(t*float64(tzCenter.X-startPt.X))
		m.drag.currY = startPt.Y + int(t*float64(tzCenter.Y-startPt.Y))
		// Keep trash zone stored for the view's trash overlay.
		m.layout.trashZone = tz
		return m, demoTickCmd()
	}

	// Done: hide the bar (simulate drop on trash zone).
	m.layoutState.hidden[trashBar.key] = true
	m.drag = dragState{}
	m.demoStep = demoWait4
	m.demoFrame = 0
	return m, demoTickCmd()
}

// demoRestoreStep simulates pressing the 0 key to restore all hidden bars.
func (m Model) demoRestoreStep() (Model, tea.Cmd) {
	var claudeKeys, codexKeys []string
	for _, c := range m.categories {
		if len(c.Key) > 6 && c.Key[:6] == "codex_" {
			codexKeys = append(codexKeys, c.Key)
		} else {
			claudeKeys = append(claudeKeys, c.Key)
		}
	}
	m.layoutState = defaultLayoutState(claudeKeys, codexKeys)
	m.demoStep = demoWait5
	m.demoFrame = 0
	return m, demoTickCmd()
}

// rectCenter returns the center point of a rectangle.
func rectCenter(r image.Rectangle) image.Point {
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

// panelDisplayName returns the display name for a panel ID.
func (m Model) panelDisplayName(panelID string) string {
	switch panelID {
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	default:
		return panelID
	}
}
