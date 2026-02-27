package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/alliprice/headroom/internal/cli"
	"github.com/alliprice/headroom/internal/tui"
)

func main() {
	status := flag.Bool("status", false, "Print usage summary and exit (no UI)")
	jsonOut := flag.Bool("json", false, "Print usage as JSON and exit (no UI)")
	debugSleep := flag.Bool("debug-sleep", false, "Start in sleep mode for debugging")
	demo := flag.Bool("demo", false, "Auto-play demo sequence and exit")
	flag.Parse()

	if *status {
		if err := cli.RunStatus(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *jsonOut {
		if err := cli.RunJSON(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := runTUI(*debugSleep, *demo); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runTUI(debugSleep, demo bool) error {
	m := tui.NewModel(debugSleep, demo)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
