package tui

import "github.com/charmbracelet/x/ansi"

func lipglossStripAnsi(s string) string { return ansi.Strip(s) }

func visibleWidth(s string) int { return ansi.StringWidth(s) }
