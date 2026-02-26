package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	status := flag.Bool("status", false, "Print usage summary and exit (no UI)")
	jsonOut := flag.Bool("json", false, "Print usage as JSON and exit (no UI)")
	debugSleep := flag.Bool("debug-sleep", false, "Start in sleep mode for debugging")
	flag.Parse()

	if *status {
		if err := runStatus(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *jsonOut {
		if err := runJSON(); err != nil {
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

// Placeholder functions - will be implemented in cli/ and tui/ packages
func runStatus() error {
	fmt.Println("TODO: --status mode")
	return nil
}

func runJSON() error {
	fmt.Println("TODO: --json mode")
	return nil
}

func runTUI(debugSleep bool) error {
	fmt.Println("TODO: TUI mode")
	return nil
}
