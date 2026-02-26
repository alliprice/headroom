package tui

import (
	"charm.land/bubbles/v2/key"
)

type keyMap struct {
	Quit     key.Binding
	Refresh  key.Binding
	Interval key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "Q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r", "R"),
			key.WithHelp("r", "refresh"),
		),
		Interval: key.NewBinding(
			key.WithKeys("t", "T"),
			key.WithHelp("t", "interval"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Refresh, k.Interval}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.Refresh, k.Interval},
	}
}
