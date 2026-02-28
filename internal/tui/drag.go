package tui

import (
	"image"

	tea "charm.land/bubbletea/v2"

	"github.com/alliprice/headroom/internal/provider"
)

// dragPhase tracks the current phase of a drag interaction.
type dragPhase int

const (
	dragIdle    dragPhase = iota
	dragPending           // mousedown received, waiting for movement threshold
	dragActive            // dragging - past movement threshold
)

// dragTarget identifies what was grabbed.
type dragTarget int

const (
	dragTargetNone  dragTarget = iota
	dragTargetPanel            // grabbed a panel border/title
	dragTargetBar              // grabbed a category (title + bar region)
)

// dragState holds all state for an in-progress drag.
type dragState struct {
	phase  dragPhase
	target dragTarget

	// What was grabbed.
	panelID    string // provider ID (for panel drag or bar's owning panel)
	barKey     string // category key (for bar drag)
	ghostLabel string // display name for the ghost indicator

	// Mouse positions.
	startX, startY int // initial click position
	currX, currY   int // current mouse position

	// Pre-drag layout snapshot for computing net change.
	preLayout layoutState
}

const dragThreshold = 2 // cells of movement before drag activates

// handleMouseDown processes a mouse click. It performs hit-testing against
// stored layout geometry and starts a pending drag if something was hit.
func (m Model) handleMouseDown(msg tea.MouseClickMsg) (Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}

	x, y := mouse.X, mouse.Y

	if m.layout == nil {
		return m, nil
	}

	// Hit-test category regions first (more specific), then panels.
	// Iterate in panel order for determinism.
	for _, pid := range m.layoutState.panelOrder {
		for _, bg := range m.layout.bars[pid] {
			if image.Pt(x, y).In(bg.bounds) {
				if bg.pinned {
					return m, nil // absorb click, no drag
				}
				m.drag = dragState{
					phase:      dragPending,
					target:     dragTargetBar,
					panelID:    pid,
					barKey:     bg.key,
					ghostLabel: m.catDisplayName(bg.key),
					startX:     x, startY: y,
					currX: x, currY: y,
				}
				m.drag.preLayout = m.layoutState.clone()
				return m, nil
			}
		}
	}
	// Check panels (border/empty space - anything not a bar).
	for _, pid := range m.layoutState.panelOrder {
		if pRect, ok := m.layout.panels[pid]; ok && image.Pt(x, y).In(pRect) {
			p := provider.ByID(pid)
			displayName := pid
			if p != nil {
				displayName = p.DisplayName
			}
			m.drag = dragState{
				phase:      dragPending,
				target:     dragTargetPanel,
				panelID:    pid,
				ghostLabel: displayName,
				startX:     x, startY: y,
				currX: x, currY: y,
			}
			m.drag.preLayout = m.layoutState.clone()
			return m, nil
		}
	}

	return m, nil
}

// handleMouseMove processes mouse motion. Promotes pending → active if the
// movement threshold is exceeded, then live-reorders items as the cursor moves.
func (m Model) handleMouseMove(msg tea.MouseMotionMsg) (Model, tea.Cmd) {
	mouse := msg.Mouse()
	if m.drag.phase == dragIdle {
		return m, nil
	}

	m.drag.currX = mouse.X
	m.drag.currY = mouse.Y

	if m.drag.phase == dragPending {
		dx := m.drag.currX - m.drag.startX
		dy := m.drag.currY - m.drag.startY
		if abs(dx) >= dragThreshold || abs(dy) >= dragThreshold {
			m.drag.phase = dragActive
		}
	}

	// Live reorder during active drag.
	if m.drag.phase == dragActive && m.layout != nil {
		switch m.drag.target {
		case dragTargetBar:
			m.liveReorderBar()
		case dragTargetPanel:
			m.liveReorderPanel()
		}
	}

	return m, nil
}

// liveReorderBar moves the dragged bar to the slot under the cursor,
// causing other bars to shift like phone app icons. The target slot is
// computed by counting non-dragged items whose midpoint is above the
// cursor - this avoids oscillation from the dragged item's own geometry.
func (m *Model) liveReorderBar() {
	pid := m.drag.panelID
	bars := m.layout.bars[pid]
	order := m.layoutState.catOrder[pid]

	if order == nil || len(bars) < 2 {
		return
	}

	// Count how many non-dragged items have their midpoint above the cursor.
	slot := 0
	for _, bg := range bars {
		if bg.key == m.drag.barKey {
			continue
		}
		midY := (bg.bounds.Min.Y + bg.bounds.Max.Y) / 2
		if m.drag.currY >= midY {
			slot++
		}
	}

	newOrder := moveToSlot(order, m.layoutState.hidden, m.drag.barKey, slot)
	if newOrder != nil {
		m.layoutState.catOrder[pid] = newOrder
	}
}

// liveReorderPanel moves the dragged panel to the slot under the cursor.
// Slot is determined by counting non-dragged panels whose midpoint is above
// the cursor - same approach as bar reordering.
func (m *Model) liveReorderPanel() {
	order := m.layoutState.panelOrder
	if len(order) < 2 {
		return
	}

	// Count how many non-dragged panels have their midpoint above the cursor.
	slot := 0
	for _, pid := range order {
		if pid == m.drag.panelID {
			continue
		}
		pRect, ok := m.layout.panels[pid]
		if !ok {
			continue
		}
		midY := (pRect.Min.Y + pRect.Max.Y) / 2
		if m.drag.currY >= midY {
			slot++
		}
	}

	// Find current position.
	currentIdx := -1
	for i, pid := range order {
		if pid == m.drag.panelID {
			currentIdx = i
			break
		}
	}
	if currentIdx < 0 {
		return
	}

	// Remove dragged panel and reinsert at target slot.
	without := make([]string, 0, len(order)-1)
	without = append(without, order[:currentIdx]...)
	without = append(without, order[currentIdx+1:]...)

	if slot < 0 {
		slot = 0
	}
	if slot > len(without) {
		slot = len(without)
	}

	reordered := make([]string, 0, len(order))
	reordered = append(reordered, without[:slot]...)
	reordered = append(reordered, m.drag.panelID)
	reordered = append(reordered, without[slot:]...)

	m.layoutState.panelOrder = reordered
}

// handleMouseUp processes mouse release. Reordering already happened live
// during drag; this just handles hide-on-release and clears drag state.
func (m Model) handleMouseUp(msg tea.MouseReleaseMsg) (Model, tea.Cmd) {
	if m.drag.phase == dragIdle {
		return m, nil
	}

	if m.drag.phase == dragActive && m.layout != nil {
		mouse := msg.Mouse()
		pt := image.Pt(mouse.X, mouse.Y)
		if pt.In(m.layout.trashZone) {
			// Trash drop: create hide command.
			switch m.drag.target {
			case dragTargetBar:
				cmd := hideBarCmd{barKey: m.drag.barKey}
				cmd.Execute(&m.layoutState)
				m.pushCmd(cmd)
			case dragTargetPanel:
				// Collect the keys that will be hidden (only currently visible ones).
				var keys []string
				for _, k := range m.layoutState.catOrder[m.drag.panelID] {
					if !m.layoutState.hidden[k] {
						keys = append(keys, k)
					}
				}
				cmd := hidePanelCmd{panelID: m.drag.panelID, keys: keys}
				cmd.Execute(&m.layoutState)
				m.pushCmd(cmd)
			}
		} else {
			// Non-trash: record net reorder as a command.
			switch m.drag.target {
			case dragTargetBar:
				pre := m.drag.preLayout.catOrder[m.drag.panelID]
				post := m.layoutState.catOrder[m.drag.panelID]
				if !slicesEqual(pre, post) {
					cmd := reorderBarCmd{
						panelID:  m.drag.panelID,
						oldOrder: copyStrings(pre),
						newOrder: copyStrings(post),
					}
					m.pushCmd(cmd)
				}
			case dragTargetPanel:
				if !slicesEqual(m.drag.preLayout.panelOrder, m.layoutState.panelOrder) {
					m.pushCmd(swapPanelsCmd{})
				}
			}
		}
	}

	m.drag = dragState{}
	return m, nil
}

// catDisplayName looks up the display name for a category key.
func (m Model) catDisplayName(key string) string {
	for _, c := range m.categories {
		if c.Key == key {
			return c.Name
		}
	}
	return key
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// slicesEqual returns true if a and b contain the same strings in the same order.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copyStrings returns a copy of a string slice.
func copyStrings(s []string) []string {
	c := make([]string, len(s))
	copy(c, s)
	return c
}
