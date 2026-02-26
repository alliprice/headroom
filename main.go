package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/alliprice/headroom/internal/cli"
)

func main() {
	status := flag.Bool("status", false, "Print usage summary and exit (no UI)")
	jsonOut := flag.Bool("json", false, "Print usage as JSON and exit (no UI)")
	debugSleep := flag.Bool("debug-sleep", false, "Start in sleep mode for debugging")
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

	if err := runTUI(*debugSleep); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runTUI(debugSleep bool) error {
	fmt.Println("TODO: TUI mode")
	return nil
}
