package tui

import (
	"image"

	"github.com/alliprice/headroom/internal/provider"
)

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

// providerOrder returns provider IDs in their canonical display order,
// derived from the provider registry.
func providerOrder() []string {
	order := make([]string, len(provider.All))
	for i, p := range provider.All {
		order[i] = p.ID
	}
	return order
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

// moveToSlot reorders a slice by placing dragKey at the given slot index.
// Returns nil if the order didn't change, dragKey wasn't found, or the
// slice has fewer than 2 elements.
func moveToSlot(order []string, dragKey string, slot int) []string {
	srcIdx := -1
	for i, k := range order {
		if k == dragKey {
			srcIdx = i
			break
		}
	}
	if srcIdx < 0 || len(order) < 2 {
		return nil
	}

	without := make([]string, 0, len(order)-1)
	without = append(without, order[:srcIdx]...)
	without = append(without, order[srcIdx+1:]...)

	if slot < 0 {
		slot = 0
	}
	if slot > len(without) {
		slot = len(without)
	}

	reordered := make([]string, 0, len(order))
	reordered = append(reordered, without[:slot]...)
	reordered = append(reordered, dragKey)
	reordered = append(reordered, without[slot:]...)

	for i := range order {
		if order[i] != reordered[i] {
			return reordered
		}
	}
	return nil
}
