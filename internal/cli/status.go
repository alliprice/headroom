package cli

import "fmt"

// RunStatus fetches usage data from all providers and prints a human-readable
// budget-headroom summary to stdout.
func RunStatus() error {
	hr, err := fetchHeadroom()
	if err != nil {
		return err
	}

	for _, cat := range hr.Categories {
		var pace string
		switch {
		case cat.Status == "conserve" && cat.AdjustedHeadroomPct > 0 && cat.UsagePct < 80:
			pace = fmt.Sprintf("%.0f%% over sleep-adjusted pace, conserve", cat.AdjustedHeadroomPct)
		case cat.Status == "conserve":
			pace = fmt.Sprintf("%+.0f%% vs pace, conserve usage", cat.HeadroomPct)
		case cat.Status == "slow down":
			pace = fmt.Sprintf("%.0f%% over pace (sleep will recover), slow down", cat.HeadroomPct)
		default: // "plenty of room"
			pace = fmt.Sprintf("%.0f%% under pace, plenty of room", -cat.HeadroomPct)
		}
		fmt.Printf("%s: %.0f%% used | %s | %s\n", cat.Name, cat.UsagePct, pace, cat.Resets)
	}

	if hr.Extra != nil {
		fmt.Printf("Extra usage: %.0f%% used ($%.2f / $%.2f) | %s\n",
			hr.Extra.UsagePct, hr.Extra.UsedDollars, hr.Extra.LimitDollars, hr.Extra.Status)
	}

	return nil
}
