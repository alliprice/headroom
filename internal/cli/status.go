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
		switch cat.Status {
		case "conserve":
			pace = fmt.Sprintf("%+.0f%% vs pace, conserve usage", cat.HeadroomPct)
		case "plenty of room":
			pace = fmt.Sprintf("%.0f%% under pace, plenty of room", cat.HeadroomPct)
		default: // "slow down"
			pace = fmt.Sprintf("%.0f%% over pace, slow down", -cat.HeadroomPct)
		}
		fmt.Printf("%s: %.0f%% used | %s | %s\n", cat.Name, cat.UsagePct, pace, cat.Resets)
	}

	if hr.Extra != nil {
		fmt.Printf("Extra usage: %.0f%% used ($%.2f / $%.2f) | %s\n",
			hr.Extra.UsagePct, hr.Extra.UsedDollars, hr.Extra.LimitDollars, hr.Extra.Status)
	}

	return nil
}
