package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alliprice/headroom/internal/parse"
	"github.com/alliprice/headroom/internal/provider"
)

// doFetch returns a tea.Cmd that fetches data from all available providers
// and combines them into a single fetchResultMsg.
func doFetch(available map[string]bool) tea.Cmd {
	return func() tea.Msg {
		var (
			allCats       []parse.Category
			providerExtra = make(map[string]*parse.ExtraUsage)
			extra         *parse.ExtraUsage
			errMsg        string
			isAuth        bool
		)

		for _, p := range provider.All {
			if !available[p.ID] {
				continue
			}

			res, authErr, err := p.Fetch()
			if err != nil {
				if errMsg != "" {
					errMsg += "; " + err.Error()
				} else {
					errMsg = err.Error()
				}
				if authErr {
					isAuth = true
				}
				continue
			}
			if res == nil {
				continue
			}

			allCats = append(allCats, res.Categories...)
			if res.Extra != nil {
				providerExtra[p.ID] = res.Extra
				extra = res.Extra // keep last non-nil for backward compat
			}
		}

		msg := fetchResultMsg{
			categories:    allCats,
			extra:         extra,
			providerExtra: providerExtra,
			errorMsg:      errMsg,
			isAuthErr:     isAuth,
		}
		if len(allCats) > 0 {
			msg.fetchTime = time.Now()
		}
		return msg
	}
}

// tickCmd returns a tea.Cmd that sends a tickMsg after 1 second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// mockFetch returns a tea.Cmd that produces a fetchResultMsg with randomized
// mock data. Each provider supplies its own plausible demo data.
func mockFetch() tea.Cmd {
	return func() tea.Msg {
		var (
			allCats       []parse.Category
			providerExtra = make(map[string]*parse.ExtraUsage)
			extra         *parse.ExtraUsage
		)

		for _, p := range provider.All {
			if p.Demo == nil {
				continue
			}
			res := p.Demo()
			if res == nil {
				continue
			}
			allCats = append(allCats, res.Categories...)
			if res.Extra != nil {
				providerExtra[p.ID] = res.Extra
				extra = res.Extra
			}
		}

		return fetchResultMsg{
			categories:    allCats,
			extra:         extra,
			providerExtra: providerExtra,
			fetchTime:     time.Now(),
		}
	}
}
