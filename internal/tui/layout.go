package tui

import "image"

// layoutState holds user-customizable layout: panel order, category order
// within panels, and hidden categories. Persists across data refreshes.
type layoutState struct {
	panelOrder     []string        // e.g. ["claude", "codex"] — render order
	claudeCatOrder []string        // category key order within Claude panel
	codexCatOrder  []string        // category key order within Codex panel
	hidden         map[string]bool // category keys hidden by drag-off
}

// barGeom associates a category key with its screen-space bounding rectangle.
type barGeom struct {
	key    string
	bounds image.Rectangle
}

// defaultLayoutState returns a layoutState with the given category keys in
// their natural order, nothing hidden, and Claude on top.
func defaultLayoutState(claudeKeys, codexKeys []string) layoutState {
	return layoutState{
		panelOrder:     []string{"claude", "codex"},
		claudeCatOrder: append([]string(nil), claudeKeys...),
		codexCatOrder:  append([]string(nil), codexKeys...),
		hidden:         make(map[string]bool),
	}
}

// orderedCats returns the category keys for a panel in custom order,
// excluding hidden entries.
func (ls *layoutState) orderedCats(panel string) []string {
	var order []string
	switch panel {
	case "claude":
		order = ls.claudeCatOrder
	case "codex":
		order = ls.codexCatOrder
	default:
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
func (ls *layoutState) syncCategories(claudeKeys, codexKeys []string) {
	ls.claudeCatOrder = mergeOrder(ls.claudeCatOrder, claudeKeys)
	ls.codexCatOrder = mergeOrder(ls.codexCatOrder, codexKeys)
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

// moveKeyBefore reorders a full order slice by moving dragKey so that it
// appears just before targetKey among the visible (non-hidden) entries.
// Hidden entries keep their relative positions. Returns nil if no change.
func moveKeyBefore(order []string, dragKey, targetKey string, hidden map[string]bool) []string {
	// Build visible-only order.
	var visible []string
	for _, k := range order {
		if !hidden[k] {
			visible = append(visible, k)
		}
	}

	// Find positions in visible order.
	srcIdx, tgtIdx := -1, -1
	for i, k := range visible {
		if k == dragKey {
			srcIdx = i
		}
		if k == targetKey {
			tgtIdx = i
		}
	}
	if srcIdx < 0 || tgtIdx < 0 || srcIdx == tgtIdx {
		return nil
	}

	// Remove dragKey from visible order.
	vis := make([]string, 0, len(visible)-1)
	vis = append(vis, visible[:srcIdx]...)
	vis = append(vis, visible[srcIdx+1:]...)

	// Adjust target index after removal.
	if tgtIdx > srcIdx {
		tgtIdx--
	}

	// Insert dragKey before target.
	reordered := make([]string, 0, len(visible))
	reordered = append(reordered, vis[:tgtIdx]...)
	reordered = append(reordered, dragKey)
	reordered = append(reordered, vis[tgtIdx:]...)

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
