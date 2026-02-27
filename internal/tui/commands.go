package tui

import (
	"math/rand"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alliprice/headroom/internal/auth"
	"github.com/alliprice/headroom/internal/fetch"
	"github.com/alliprice/headroom/internal/parse"
)

// doFetch returns a tea.Cmd that fetches both Claude and Codex data and
// combines them into a single fetchResultMsg.
func doFetch(codexAvailable bool) tea.Cmd {
	return func() tea.Msg {
		var (
			claudeCats  []parse.Category
			claudeExtra *parse.ExtraUsage
			codexCats   []parse.Category
			errMsg      string
			isAuth      bool
		)

		// Fetch Claude
		token, err := auth.GetAccessToken()
		if err != nil {
			errMsg = err.Error()
			isAuth = true
		} else {
			data, err := fetch.FetchClaude(token)
			if err != nil {
				errMsg = err.Error()
				isAuth = strings.Contains(errMsg, "expired") || strings.Contains(errMsg, "authenticate")
			} else {
				claudeCats, claudeExtra = parse.ParseClaude(data)
			}
		}

		// Fetch Codex
		if codexAvailable {
			data, err := fetch.FetchCodex()
			if err != nil {
				// Codex error is non-fatal; append to error message
				if errMsg != "" {
					errMsg += "; " + err.Error()
				} else {
					errMsg = err.Error()
				}
			} else if data != nil {
				codexCats = parse.ParseCodex(data)
			}
		}

		// Combine: rename when both present, order as claude core + codex + claude extras
		if len(codexCats) > 0 {
			for i := range codexCats {
				switch codexCats[i].Key {
				case "codex_primary":
					codexCats[i].Name = "Session"
				case "codex_secondary":
					codexCats[i].Name = "Weekly"
				}
			}
		}

		var claudeCore, claudeExtras []parse.Category
		for _, c := range claudeCats {
			if c.Key == "five_hour" || c.Key == "seven_day" {
				claudeCore = append(claudeCore, c)
			} else {
				claudeExtras = append(claudeExtras, c)
			}
		}
		all := append(append(claudeCore, codexCats...), claudeExtras...)

		msg := fetchResultMsg{
			categories: all,
			extra:      claudeExtra,
			errorMsg:   errMsg,
			isAuthErr:  isAuth,
		}
		if len(all) > 0 {
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
		}
		extra := &parse.ExtraUsage{
			MonthlyLimit: 10000,
			UsedCredits:  3500 + rand.Float64()*3000,
		}
		extra.Utilization = extra.UsedCredits / extra.MonthlyLimit * 100

		return fetchResultMsg{
			categories: cats,
			extra:      extra,
			fetchTime:  time.Now(),
		}
	}
}
