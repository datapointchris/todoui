package tui

import "github.com/charmbracelet/lipgloss"

// All colors use ANSI 0-15 so the TUI adapts to the terminal's theme.
//
//	0=black  1=red     2=green   3=yellow  4=blue    5=magenta  6=cyan    7=white
//	8=bright black (dim)  9-15=bright variants of 1-7
var (
	// Pane borders
	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()).
				BorderForeground(lipgloss.Color("4"))

	inactiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("8"))

	// Project list
	projectNormalStyle   = lipgloss.NewStyle().PaddingLeft(1)
	projectSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(0).
				Foreground(lipgloss.Color("2")).
				Bold(true)

	// Item list
	itemNormalStyle   = lipgloss.NewStyle().PaddingLeft(1)
	itemSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(0).
				Foreground(lipgloss.Color("2")).
				Bold(true)
	itemIDStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))
	itemCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Strikethrough(true)
	multiProjectStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("3"))
	notesIndicatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("6"))
	notesPreviewStyle = lipgloss.NewStyle().
				PaddingLeft(16)
	notesConnectorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("4"))
	blockerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			PaddingLeft(5)
	blockerProjectStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("3")).
				Bold(true)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			PaddingLeft(1).
			PaddingRight(1)
	modeLocalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2")).
			Bold(true)
	syncOKStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2")).
			Bold(true)
	syncingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("3")).
			Bold(true)
	syncPendingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("3")).
				Bold(true)
	syncErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true)

	// Titles
	paneTitleStyle = lipgloss.NewStyle().
			Bold(true).
			PaddingLeft(1)

	// Empty state
	emptyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Italic(true).
			PaddingLeft(2)

	// Overlay
	overlayBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("4")).
			Padding(1, 2)

	overlayTitleStyle = lipgloss.NewStyle().
				Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	// Project picker
	pickerSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("2")).
				Bold(true)

	pickerNormalStyle = lipgloss.NewStyle()

	// Status flash
	statusMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2"))

	errorMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true)

	// Filter indicator
	filterIndicatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("3")).
				Bold(true)

	// Move mode
	moveIndicatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("3")).
				Bold(true)

	// Tasks
	taskNormalStyle = lipgloss.NewStyle().
			PaddingLeft(5)
	taskSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(4).
				Foreground(lipgloss.Color("2")).
				Bold(true)
	taskCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Strikethrough(true)

	// Search results
	searchResultSelectedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("2")).
					Bold(true)
	searchResultNormalStyle = lipgloss.NewStyle()
)
