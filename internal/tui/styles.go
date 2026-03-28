package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Pane borders
	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62"))

	inactiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240"))

	// Project list
	projectNormalStyle   = lipgloss.NewStyle().PaddingLeft(1)
	projectSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(0).
				Foreground(lipgloss.Color("170")).
				Bold(true)
	projectCountStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))

	// Item list
	itemNormalStyle   = lipgloss.NewStyle().PaddingLeft(1)
	itemSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(0).
				Foreground(lipgloss.Color("170")).
				Bold(true)
	itemIDStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
	itemCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Strikethrough(true)
	multiProjectStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214"))
	blockerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			PaddingLeft(5)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			PaddingLeft(1).
			PaddingRight(1)
	modeLocalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true)
	modeRemoteStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("69")).
			Bold(true)

	// Titles
	paneTitleStyle = lipgloss.NewStyle().
			Bold(true).
			PaddingLeft(1)

	// Empty state
	emptyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true).
			PaddingLeft(2)

	// Overlay
	overlayBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2)

	overlayTitleStyle = lipgloss.NewStyle().
				Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	// Project picker
	pickerSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("170")).
				Bold(true)

	pickerNormalStyle = lipgloss.NewStyle()

	// Status flash
	statusMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))
)
