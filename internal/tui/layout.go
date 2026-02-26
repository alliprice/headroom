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
