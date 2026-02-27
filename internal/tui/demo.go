package tui

import (
	"image"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alliprice/headroom/internal/provider"
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
	demoWait4                      // pause after trash, then quit
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
	if m.demoFrame == 1 {
		if len(m.layoutState.panelOrder) < 2 {
			m.demoStep = demoWait2
			m.demoFrame = 0
			return m, demoTickCmd()
		}
		topPanelID := m.layoutState.panelOrder[0]
		botPanelID := m.layoutState.panelOrder[1]
		topRect := m.layout.panels[topPanelID]
		botRect := m.layout.panels[botPanelID]
		start := rectCenter(topRect)
		end := rectCenter(botRect)
		p := provider.ByID(topPanelID)
		displayName := topPanelID
		if p != nil {
			displayName = p.DisplayName
		}
		m.drag = dragState{
			phase:      dragActive,
			target:     dragTargetPanel,
			panelID:    topPanelID,
			ghostLabel: displayName,
			startX:     start.X, startY: start.Y,
			currX: start.X, currY: start.Y,
		}
		m.demoEndX = end.X
		m.demoEndY = end.Y
		return m, demoTickCmd()
	}

	if m.demoFrame <= demoAnimDuration {
		t := easeOutCubic(float64(m.demoFrame-1) / float64(demoAnimDuration-1))
		m.drag.currX = m.drag.startX + int(t*float64(m.demoEndX-m.drag.startX))
		m.drag.currY = m.drag.startY + int(t*float64(m.demoEndY-m.drag.startY))
		m.liveReorderPanel()
		return m, demoTickCmd()
	}

	m.drag = dragState{}
	m.demoStep = demoWait2
	m.demoFrame = 0
	return m, demoTickCmd()
}

// demoBarReorderStep drags the second bar in the top panel to the first slot.
func (m Model) demoBarReorderStep() (Model, tea.Cmd) {
	if m.demoFrame == 1 {
		topPanelID := m.layoutState.panelOrder[0]
		bars := m.layout.bars[topPanelID]
		if len(bars) < 2 {
			m.demoStep = demoWait3
			m.demoFrame = 0
			return m, demoTickCmd()
		}
		srcBar := bars[1]
		dstBar := bars[0]
		start := rectCenter(srcBar.bounds)
		end := image.Pt(rectCenter(dstBar.bounds).X, dstBar.bounds.Min.Y)
		m.drag = dragState{
			phase:      dragActive,
			target:     dragTargetBar,
			panelID:    topPanelID,
			barKey:     srcBar.key,
			ghostLabel: m.catDisplayName(srcBar.key),
			startX:     start.X, startY: start.Y,
			currX: start.X, currY: start.Y,
		}
		m.demoEndX = end.X
		m.demoEndY = end.Y
		return m, demoTickCmd()
	}

	if m.demoFrame <= demoAnimDuration {
		t := easeOutCubic(float64(m.demoFrame-1) / float64(demoAnimDuration-1))
		m.drag.currX = m.drag.startX + int(t*float64(m.demoEndX-m.drag.startX))
		m.drag.currY = m.drag.startY + int(t*float64(m.demoEndY-m.drag.startY))
		m.liveReorderBar()
		return m, demoTickCmd()
	}

	m.drag = dragState{}
	m.demoStep = demoWait3
	m.demoFrame = 0
	return m, demoTickCmd()
}

// demoBarTrashStep drags the last bar in the first provider's panel to the trash zone.
func (m Model) demoBarTrashStep() (Model, tea.Cmd) {
	if m.demoFrame == 1 {
		// Find the first provider panel that has bars.
		var trashPID string
		var bars []barGeom
		for _, pid := range m.layoutState.panelOrder {
			if b := m.layout.bars[pid]; len(b) > 0 {
				trashPID = pid
				bars = b
				break
			}
		}
		if len(bars) == 0 {
			m.demoStep = demoWait4
			m.demoFrame = 0
			return m, demoTickCmd()
		}
		trashBar := bars[len(bars)-1]
		tz := trashZoneRect(m.width, m.height)
		start := rectCenter(trashBar.bounds)
		end := rectCenter(tz)
		m.drag = dragState{
			phase:      dragActive,
			target:     dragTargetBar,
			panelID:    trashPID,
			barKey:     trashBar.key,
			ghostLabel: m.catDisplayName(trashBar.key),
			startX:     start.X, startY: start.Y,
			currX: start.X, currY: start.Y,
		}
		m.demoEndX = end.X
		m.demoEndY = end.Y
		m.layout.trashZone = tz
		return m, demoTickCmd()
	}

	if m.demoFrame <= demoAnimDuration {
		t := easeOutCubic(float64(m.demoFrame-1) / float64(demoAnimDuration-1))
		m.drag.currX = m.drag.startX + int(t*float64(m.demoEndX-m.drag.startX))
		m.drag.currY = m.drag.startY + int(t*float64(m.demoEndY-m.drag.startY))
		m.layout.trashZone = trashZoneRect(m.width, m.height)
		return m, demoTickCmd()
	}

	// Done: hide the bar.
	m.layoutState.hidden[m.drag.barKey] = true
	m.drag = dragState{}
	m.demoStep = demoWait4
	m.demoFrame = 0
	return m, demoTickCmd()
}

// rectCenter returns the center point of a rectangle.
func rectCenter(r image.Rectangle) image.Point {
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}
