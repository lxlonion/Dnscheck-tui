package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"dnscheck/config"
	"dnscheck/tui"
)

func main() {
	cfgPath := flag.String("config", "config.json", "path to JSON config file")
	flag.Parse()

	cfg, err := config.LoadOrCreate(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dnscheck:", err)
		os.Exit(1)
	}

	if _, err := tea.NewProgram(tui.New(cfg), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "dnscheck:", err)
		os.Exit(1)
	}
}
