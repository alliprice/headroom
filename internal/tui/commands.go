package tui

import (
	"math/rand"
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

// sleepTickCmd returns a tea.Cmd that sends a sleepTickMsg after 500ms.
func sleepTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return sleepTickMsg(t)
	})
}

// mockFetch returns a tea.Cmd that produces a fetchResultMsg with randomized
// mock data. Used in demo mode to avoid hitting real APIs.
func mockFetch() tea.Cmd {
	return func() tea.Msg {
		now := time.Now().UTC()
		cats := []parse.Category{
			{
				Key:           "five_hour",
				Name:          "Session",
				Utilization:   35 + rand.Float64()*30,
				ResetsAt:      now.Add(time.Duration(1+rand.Intn(4)) * time.Hour).Format(time.RFC3339),
				WindowSeconds: parse.WindowFiveHour,
			},
			{
				Key:           "seven_day",
				Name:          "Weekly",
				Utilization:   55 + rand.Float64()*25,
				ResetsAt:      now.Add(time.Duration(24+rand.Intn(120)) * time.Hour).Format(time.RFC3339),
				WindowSeconds: parse.WindowSevenDay,
			},
			{
				Key:           "codex_primary",
				Name:          "Session",
				Utilization:   20 + rand.Float64()*40,
				ResetsAt:      now.Add(time.Duration(1+rand.Intn(4)) * time.Hour).Format(time.RFC3339),
				WindowSeconds: parse.WindowFiveHour,
			},
			{
				Key:           "codex_secondary",
				Name:          "Weekly",
				Utilization:   40 + rand.Float64()*35,
				ResetsAt:      now.Add(time.Duration(24+rand.Intn(120)) * time.Hour).Format(time.RFC3339),
				WindowSeconds: parse.WindowSevenDay,
			},
			{
				Key:           "gemini_gemini-2.0-flash",
				Name:          "2.0 Flash",
				Utilization:   30 + rand.Float64()*40,
				ResetsAt:      now.Add(time.Duration(12+rand.Intn(12)) * time.Hour).Format(time.RFC3339),
				WindowSeconds: 86400,
			},
		}
		extra := &parse.ExtraUsage{
			MonthlyLimit: 10000,
			UsedCredits:  3500 + rand.Float64()*3000,
		}
		extra.Utilization = extra.UsedCredits / extra.MonthlyLimit * 100

		return fetchResultMsg{
			categories:    cats,
			extra:         extra,
			providerExtra: map[string]*parse.ExtraUsage{"claude": extra},
			fetchTime:     time.Now(),
		}
	}
}
