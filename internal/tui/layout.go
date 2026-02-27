package tui

import "image"

// layoutState holds user-customizable layout: panel order, category order
// within panels, and hidden categories. Persists across data refreshes.
type layoutState struct {
	panelOrder []string            // e.g. ["claude", "codex"] - render order
	catOrder   map[string][]string // provider ID → category key order
	hidden     map[string]bool     // category keys hidden by drag-off
}

// barGeom associates a category key with its screen-space bounding rectangle.
type barGeom struct {
	key    string
	bounds image.Rectangle
	pinned bool // true = absorbs clicks but can't be dragged
}

// defaultLayoutState returns a layoutState with the given per-provider
// category keys in their natural order, nothing hidden, panels in order.
func defaultLayoutState(catsByProvider map[string][]string) layoutState {
	panelOrder := make([]string, 0, len(catsByProvider))
	catOrder := make(map[string][]string, len(catsByProvider))
	for _, p := range providerOrder() {
		if keys, ok := catsByProvider[p]; ok {
			panelOrder = append(panelOrder, p)
			catOrder[p] = append([]string(nil), keys...)
		}
	}
	return layoutState{
		panelOrder: panelOrder,
		catOrder:   catOrder,
		hidden:     make(map[string]bool),
	}
}

// providerOrder returns provider IDs in their canonical display order.
func providerOrder() []string {
	return []string{"claude", "codex"}
}

// orderedCats returns the category keys for a panel in custom order,
// excluding hidden entries.
func (ls *layoutState) orderedCats(panel string) []string {
	order := ls.catOrder[panel]
	if order == nil {
		return nil
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		if !ls.hidden[k] {
			out = append(out, k)
		}
	}
	return out
}

// syncCategories ensures that any new category keys from a data fetch are
// appended to the end of the ordering, and stale keys are left in place
// (they'll just never match).
func (ls *layoutState) syncCategories(catsByProvider map[string][]string) {
	if ls.catOrder == nil {
		ls.catOrder = make(map[string][]string)
	}
	for pid, keys := range catsByProvider {
		ls.catOrder[pid] = mergeOrder(ls.catOrder[pid], keys)
	}
}

// mergeOrder appends any keys in incoming that aren't already in existing.
func mergeOrder(existing, incoming []string) []string {
	set := make(map[string]bool, len(existing))
	for _, k := range existing {
		set[k] = true
	}
	for _, k := range incoming {
		if !set[k] {
			existing = append(existing, k)
			set[k] = true
		}
	}
	return existing
}

// hideAllBarsInPanel hides every bar that belongs to the given panel.
func (ls *layoutState) hideAllBarsInPanel(panelID string) {
	order := ls.catOrder[panelID]
	for _, k := range order {
		ls.hidden[k] = true
	}
}

// clone returns a deep copy of the layout state.
func (ls layoutState) clone() layoutState {
	po := make([]string, len(ls.panelOrder))
	copy(po, ls.panelOrder)
	co := make(map[string][]string, len(ls.catOrder))
	for k, v := range ls.catOrder {
		vc := make([]string, len(v))
		copy(vc, v)
		co[k] = vc
	}
	h := make(map[string]bool, len(ls.hidden))
	for k, v := range ls.hidden {
		h[k] = v
	}
	return layoutState{panelOrder: po, catOrder: co, hidden: h}
}

// trashZoneRect returns the screen-space rectangle for the trash drop zone
// positioned in the bottom-right corner of the screen (above the status bar).
func trashZoneRect(w, h int) image.Rectangle {
	const (
		tzWidth  = 11 // columns (matches block-pixel art width)
		tzHeight = 7  // rows
		margin   = 2 // from screen edges
	)
	x1 := w - tzWidth - margin
	y1 := h - 1 - tzHeight - margin // h-1 = status bar row
	return image.Rect(x1, y1, x1+tzWidth, y1+tzHeight)
}

// moveToSlot reorders a full order slice by placing dragKey at the given
// slot index among visible (non-hidden) entries. The slot is computed
// externally by counting non-dragged items whose midpoint is above the
// cursor. Hidden entries keep their relative positions. Returns nil if
// the order didn't change.
func moveToSlot(order []string, hidden map[string]bool, dragKey string, slot int) []string {
	// Build visible-only order.
	var visible []string
	for _, k := range order {
		if !hidden[k] {
			visible = append(visible, k)
		}
	}

	// Find current position.
	srcIdx := -1
	for i, k := range visible {
		if k == dragKey {
			srcIdx = i
			break
		}
	}
	if srcIdx < 0 {
		return nil
	}

	// Remove dragKey.
	without := make([]string, 0, len(visible)-1)
	without = append(without, visible[:srcIdx]...)
	without = append(without, visible[srcIdx+1:]...)

	// Clamp slot.
	if slot < 0 {
		slot = 0
	}
	if slot > len(without) {
		slot = len(without)
	}

	// Insert at target slot.
	reordered := make([]string, 0, len(visible))
	reordered = append(reordered, without[:slot]...)
	reordered = append(reordered, dragKey)
	reordered = append(reordered, without[slot:]...)

	// Check if order actually changed.
	changed := false
	for i := range visible {
		if visible[i] != reordered[i] {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}

	// Rebuild full order preserving hidden entries' positions.
	newOrder := make([]string, 0, len(order))
	vi := 0
	for _, k := range order {
		if hidden[k] {
			newOrder = append(newOrder, k)
		} else {
			newOrder = append(newOrder, reordered[vi])
			vi++
		}
	}
	return newOrder
}
