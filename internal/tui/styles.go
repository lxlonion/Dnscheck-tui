package tui

import "github.com/charmbracelet/lipgloss"

var (
	tabActiveStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Underline(true)
	titleStyle      = lipgloss.NewStyle().Bold(true)
	subtleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	headerCellStyle = lipgloss.NewStyle().Bold(true)
	greenStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	redStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	yellowStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	sepStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)
