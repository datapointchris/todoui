package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/model"
)

// App is the top-level Bubble Tea model for the TUI.
type App struct {
	backend backend.Backend
	mode    string

	projects     []model.ProjectWithItemCount
	items        []model.ProjectItemInProject
	blockedSet   map[int64]bool
	itemBlockers map[int64][]model.ProjectItem

	activePane    pane
	projectCursor int
	itemCursor    int

	width  int
	height int

	err error
}

// NewApp creates a new TUI application backed by the given Backend.
func NewApp(b backend.Backend, mode string) *App {
	return &App{
		backend:      b,
		mode:         mode,
		blockedSet:   make(map[int64]bool),
		itemBlockers: make(map[int64][]model.ProjectItem),
	}
}

// --- Messages ---

type projectsMsg []model.ProjectWithItemCount

type itemsMsg struct {
	items    []model.ProjectItemInProject
	blocked  map[int64]bool
	blockers map[int64][]model.ProjectItem
}

type errMsg struct{ error }

// --- Commands ---

func fetchProjectsCmd(b backend.Backend) tea.Cmd {
	return func() tea.Msg {
		projects, err := b.ListProjects()
		if err != nil {
			return errMsg{err}
		}
		return projectsMsg(projects)
	}
}

func fetchItemsCmd(b backend.Backend, projectID int64) tea.Cmd {
	return func() tea.Msg {
		items, err := b.ListItemsByProject(projectID)
		if err != nil {
			return errMsg{err}
		}

		blockedItems, err := b.ListBlocked()
		if err != nil {
			return errMsg{err}
		}
		blockedSet := make(map[int64]bool)
		for _, bi := range blockedItems {
			blockedSet[bi.ID] = true
		}

		blockers := make(map[int64][]model.ProjectItem)
		for _, item := range items {
			if blockedSet[item.ID] {
				bs, err := b.GetBlockers(item.ID)
				if err != nil {
					return errMsg{err}
				}
				blockers[item.ID] = bs
			}
		}

		return itemsMsg{items: items, blocked: blockedSet, blockers: blockers}
	}
}

// --- Bubble Tea interface ---

func (m *App) Init() tea.Cmd {
	return fetchProjectsCmd(m.backend)
}

func (m *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case projectsMsg:
		m.projects = msg
		m.projectCursor = 0
		if len(m.projects) > 0 {
			return m, fetchItemsCmd(m.backend, m.projects[0].ID)
		}
		return m, nil

	case itemsMsg:
		m.items = msg.items
		m.blockedSet = msg.blocked
		m.itemBlockers = msg.blockers
		m.itemCursor = 0
		return m, nil

	case errMsg:
		m.err = msg.error
		return m, nil
	}

	return m, nil
}

func (m *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab", "h", "l", "left", "right":
		if m.activePane == projectPane {
			m.activePane = itemPane
		} else {
			m.activePane = projectPane
		}
		return m, nil

	case "j", "down":
		cmd := m.cursorDown()
		return m, cmd

	case "k", "up":
		cmd := m.cursorUp()
		return m, cmd

	case "enter":
		if m.activePane == projectPane && len(m.projects) > 0 {
			m.activePane = itemPane
			return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID)
		}
		return m, nil
	}

	return m, nil
}

func (m *App) cursorDown() tea.Cmd {
	if m.activePane == projectPane {
		if m.projectCursor < len(m.projects)-1 {
			m.projectCursor++
			return fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID)
		}
	} else {
		if m.itemCursor < len(m.items)-1 {
			m.itemCursor++
		}
	}
	return nil
}

func (m *App) cursorUp() tea.Cmd {
	if m.activePane == projectPane {
		if m.projectCursor > 0 {
			m.projectCursor--
			return fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID)
		}
	} else {
		if m.itemCursor > 0 {
			m.itemCursor--
		}
	}
	return nil
}

// --- View ---

func (m *App) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.err != nil {
		return fmt.Sprintf("Error: %v\nPress q to quit.", m.err)
	}

	statusBar := m.renderStatusBar()
	statusHeight := lipgloss.Height(statusBar)

	// Available height for panes (subtract borders + status bar)
	paneHeight := m.height - statusHeight - 2 // 2 for top/bottom border
	if paneHeight < 3 {
		paneHeight = 3
	}

	projectPaneWidth := 24
	itemPaneWidth := m.width - projectPaneWidth - 2 // 2 for border overlap
	if itemPaneWidth < 20 {
		itemPaneWidth = 20
	}

	leftPane := m.renderProjectPane(projectPaneWidth-2, paneHeight)
	rightPane := m.renderItemPane(itemPaneWidth-2, paneHeight)

	// Apply borders based on active pane
	var leftBorder, rightBorder lipgloss.Style
	if m.activePane == projectPane {
		leftBorder = activeBorderStyle.Width(projectPaneWidth - 2).Height(paneHeight)
		rightBorder = inactiveBorderStyle.Width(itemPaneWidth - 2).Height(paneHeight)
	} else {
		leftBorder = inactiveBorderStyle.Width(projectPaneWidth - 2).Height(paneHeight)
		rightBorder = activeBorderStyle.Width(itemPaneWidth - 2).Height(paneHeight)
	}

	left := leftBorder.Render(leftPane)
	right := rightBorder.Render(rightPane)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return lipgloss.JoinVertical(lipgloss.Left, panes, statusBar)
}

func (m *App) renderProjectPane(width, height int) string {
	title := paneTitleStyle.Render("Projects")
	var lines []string
	lines = append(lines, title)

	if len(m.projects) == 0 {
		lines = append(lines, emptyStyle.Render("No projects"))
		return strings.Join(lines, "\n")
	}

	for i, p := range m.projects {
		name := p.Name
		count := projectCountStyle.Render(fmt.Sprintf("(%d)", p.ItemCount))
		line := fmt.Sprintf("%s %s", name, count)

		if i == m.projectCursor {
			line = projectSelectedStyle.Render("> " + line)
		} else {
			line = projectNormalStyle.Render("  " + line)
		}

		// Truncate to pane width
		if lipgloss.Width(line) > width {
			line = line[:width]
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m *App) renderItemPane(width, height int) string {
	var titleText string
	if len(m.projects) > 0 && m.projectCursor < len(m.projects) {
		titleText = fmt.Sprintf("Items - %s", m.projects[m.projectCursor].Name)
	} else {
		titleText = "Items"
	}
	title := paneTitleStyle.Render(titleText)
	var lines []string
	lines = append(lines, title)

	if len(m.items) == 0 {
		lines = append(lines, emptyStyle.Render("No items"))
		return strings.Join(lines, "\n")
	}

	for i, item := range m.items {
		line := m.renderItemLine(item, i == m.itemCursor, width)
		lines = append(lines, line)

		// Show blockers under blocked items
		if blockers, ok := m.itemBlockers[item.ID]; ok && len(blockers) > 0 {
			for _, b := range blockers {
				blockerLine := blockerStyle.Render(
					fmt.Sprintf("└─ blocked by: %s (#%d)", b.Title, b.ID),
				)
				lines = append(lines, blockerLine)
			}
		}
	}

	return strings.Join(lines, "\n")
}

func (m *App) renderItemLine(item model.ProjectItemInProject, selected bool, width int) string {
	// Status indicator
	status := "○"
	if item.Completed {
		status = "✓"
	}

	// Multi-project indicator
	multiProject := ""
	if item.ProjectCount > 1 {
		multiProject = multiProjectStyle.Render(" ◈")
	}

	// Item ID
	id := itemIDStyle.Render(fmt.Sprintf("#%d", item.ID))

	// Title
	title := item.Title

	// Build the line
	content := fmt.Sprintf("%s %s%s  %s", status, title, multiProject, id)

	if item.Completed {
		content = itemCompletedStyle.Render(content)
	}

	if selected {
		return itemSelectedStyle.Render("> " + content)
	}
	return itemNormalStyle.Render("  " + content)
}

func (m *App) renderStatusBar() string {
	hints := "[a]dd [d]one [x]archive [n]otes [u]ndo [/]search [?]help"

	var modeStr string
	if m.mode == "remote" {
		modeStr = modeRemoteStyle.Render("REMOTE")
	} else {
		modeStr = modeLocalStyle.Render("LOCAL")
	}

	hintsWidth := lipgloss.Width(hints)
	modeWidth := lipgloss.Width(modeStr)
	padding := m.width - hintsWidth - modeWidth - 4
	if padding < 1 {
		padding = 1
	}

	bar := fmt.Sprintf(" %s%s%s ", hints, strings.Repeat(" ", padding), modeStr)
	return statusBarStyle.Render(bar)
}
