package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/model"
)

// App is the top-level Bubble Tea model for the TUI.
type App struct {
	backend backend.Backend
	mode    string // "local" or "remote"

	projects     []model.ProjectWithItemCount
	items        []model.ProjectItemInProject
	blockedSet   map[int64]bool
	itemBlockers map[int64][]model.ProjectItem

	activePane    pane
	projectCursor int
	itemCursor    int

	width  int
	height int

	// Modal state
	appMode      appMode
	titleInput   textinput.Model
	picker       projectPicker
	pendingTitle string // for A flow: title input → project picker
	statusMsg    string // flash feedback in status bar

	err error
}

// NewApp creates a new TUI application backed by the given Backend.
func NewApp(b backend.Backend, mode string) *App {
	ti := textinput.New()
	ti.CharLimit = 200

	return &App{
		backend:      b,
		mode:         mode,
		blockedSet:   make(map[int64]bool),
		itemBlockers: make(map[int64][]model.ProjectItem),
		titleInput:   ti,
	}
}

// --- Messages ---

type projectsMsg []model.ProjectWithItemCount

type itemsMsg struct {
	items    []model.ProjectItemInProject
	blocked  map[int64]bool
	blockers map[int64][]model.ProjectItem
}

type (
	itemCreatedMsg       struct{}
	itemUpdatedMsg       struct{}
	projectCreatedMsg    struct{}
	undoResultMsg        string
	itemProjectsMsg      []model.Project
	membershipUpdatedMsg struct{}
)

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

func createItemCmd(b backend.Backend, input model.CreateProjectItem) tea.Cmd {
	return func() tea.Msg {
		_, err := b.CreateItem(input)
		if err != nil {
			return errMsg{err}
		}
		return itemCreatedMsg{}
	}
}

func updateItemCmd(b backend.Backend, id int64, input model.UpdateProjectItem) tea.Cmd {
	return func() tea.Msg {
		_, err := b.UpdateItem(id, input)
		if err != nil {
			return errMsg{err}
		}
		return itemUpdatedMsg{}
	}
}

func createProjectCmd(b backend.Backend, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := b.CreateProject(name)
		if err != nil {
			return errMsg{err}
		}
		return projectCreatedMsg{}
	}
}

func undoCmd(b backend.Backend) tea.Cmd {
	return func() tea.Msg {
		desc, err := b.Undo()
		if err != nil {
			return errMsg{err}
		}
		return undoResultMsg(desc)
	}
}

func fetchItemProjectsCmd(b backend.Backend, itemID int64) tea.Cmd {
	return func() tea.Msg {
		projects, err := b.GetItemProjects(itemID)
		if err != nil {
			return errMsg{err}
		}
		return itemProjectsMsg(projects)
	}
}

func updateMembershipCmd(b backend.Backend, itemID int64, toAdd, toRemove []int64) tea.Cmd {
	return func() tea.Msg {
		for _, pid := range toAdd {
			if err := b.AddToProject(itemID, pid); err != nil {
				return errMsg{err}
			}
		}
		for _, pid := range toRemove {
			if err := b.RemoveFromProject(itemID, pid); err != nil {
				return errMsg{err}
			}
		}
		return membershipUpdatedMsg{}
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
		if m.projectCursor >= len(m.projects) {
			m.projectCursor = max(0, len(m.projects)-1)
		}
		if len(m.projects) > 0 {
			return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID)
		}
		m.items = nil
		return m, nil

	case itemsMsg:
		m.items = msg.items
		m.blockedSet = msg.blocked
		m.itemBlockers = msg.blockers
		if m.itemCursor >= len(m.items) {
			m.itemCursor = max(0, len(m.items)-1)
		}
		return m, nil

	case itemCreatedMsg:
		m.statusMsg = "Item created"
		return m, fetchProjectsCmd(m.backend)

	case itemUpdatedMsg:
		m.statusMsg = "Item updated"
		return m, fetchProjectsCmd(m.backend)

	case projectCreatedMsg:
		m.statusMsg = "Project created"
		return m, fetchProjectsCmd(m.backend)

	case undoResultMsg:
		m.statusMsg = fmt.Sprintf("Undo: %s", string(msg))
		return m, fetchProjectsCmd(m.backend)

	case itemProjectsMsg:
		if len(m.items) > 0 && m.itemCursor < len(m.items) {
			item := m.items[m.itemCursor]
			m.picker = newPickerForManage(m.projects, msg, item)
			m.appMode = modeProjectPicker
		}
		return m, nil

	case membershipUpdatedMsg:
		m.statusMsg = "Project membership updated"
		return m, fetchProjectsCmd(m.backend)

	case errMsg:
		m.err = msg.error
		return m, nil
	}

	return m, nil
}

// --- Key handling ---

func (m *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.appMode {
	case modeNormal:
		m.statusMsg = ""
		return m.handleNormalKey(msg)
	case modeAddItem, modeAddItemMulti, modeAddProject, modeEditTitle:
		return m.handleInputKey(msg)
	case modeProjectPicker:
		return m.handlePickerKey(msg)
	}
	return m, nil
}

func (m *App) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
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

	// --- Phase 4 actions ---

	case "a":
		if m.activePane == projectPane {
			m.titleInput.SetValue("")
			m.titleInput.Placeholder = "Enter project name..."
			cmd := m.titleInput.Focus()
			m.appMode = modeAddProject
			return m, cmd
		}
		if len(m.projects) > 0 {
			m.titleInput.SetValue("")
			m.titleInput.Placeholder = "Enter item title..."
			cmd := m.titleInput.Focus()
			m.appMode = modeAddItem
			return m, cmd
		}
		return m, nil

	case "A":
		if len(m.projects) > 0 {
			m.titleInput.SetValue("")
			m.titleInput.Placeholder = "Enter item title..."
			cmd := m.titleInput.Focus()
			m.appMode = modeAddItemMulti
			return m, cmd
		}
		return m, nil

	case " ":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			if m.blockedSet[item.ID] && !item.Completed {
				m.statusMsg = "Cannot complete: item has unresolved blockers"
				return m, nil
			}
			toggled := !item.Completed
			return m, updateItemCmd(m.backend, item.ID, model.UpdateProjectItem{Completed: &toggled})
		}
		return m, nil

	case "x":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			archived := true
			return m, updateItemCmd(m.backend, item.ID, model.UpdateProjectItem{Archived: &archived})
		}
		return m, nil

	case "e":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			m.titleInput.SetValue(item.Title)
			m.titleInput.Placeholder = ""
			cmd := m.titleInput.Focus()
			m.appMode = modeEditTitle
			return m, cmd
		}
		return m, nil

	case "p":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			return m, fetchItemProjectsCmd(m.backend, item.ID)
		}
		return m, nil

	case "u":
		return m, undoCmd(m.backend)
	}

	return m, nil
}

func (m *App) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		value := strings.TrimSpace(m.titleInput.Value())
		if value == "" {
			return m, nil
		}

		switch m.appMode {
		case modeAddItem:
			m.appMode = modeNormal
			m.titleInput.Blur()
			projectID := m.projects[m.projectCursor].ID
			return m, createItemCmd(m.backend, model.CreateProjectItem{
				Title:      value,
				ProjectIDs: []int64{projectID},
			})

		case modeAddItemMulti:
			// Transition to project picker
			m.pendingTitle = value
			m.titleInput.Blur()
			currentProjectID := m.projects[m.projectCursor].ID
			m.picker = newPickerForCreate(m.projects, currentProjectID, value)
			m.appMode = modeProjectPicker
			return m, nil

		case modeAddProject:
			m.appMode = modeNormal
			m.titleInput.Blur()
			return m, createProjectCmd(m.backend, value)

		case modeEditTitle:
			m.appMode = modeNormal
			m.titleInput.Blur()
			item := m.items[m.itemCursor]
			return m, updateItemCmd(m.backend, item.ID, model.UpdateProjectItem{Title: &value})
		}

	case "esc":
		m.appMode = modeNormal
		m.titleInput.Blur()
		m.pendingTitle = ""
		return m, nil

	default:
		var cmd tea.Cmd
		m.titleInput, cmd = m.titleInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *App) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.picker.down()
		return m, nil

	case "k", "up":
		m.picker.up()
		return m, nil

	case " ":
		m.picker.toggle()
		return m, nil

	case "enter":
		m.appMode = modeNormal

		switch m.picker.intent {
		case pickerCreate:
			selectedIDs := m.picker.selectedIDs()
			m.pendingTitle = ""
			return m, createItemCmd(m.backend, model.CreateProjectItem{
				Title:      m.picker.itemTitle,
				ProjectIDs: selectedIDs,
			})

		case pickerManage:
			toAdd := m.picker.toAdd()
			toRemove := m.picker.toRemove()
			if len(toAdd) == 0 && len(toRemove) == 0 {
				return m, nil
			}
			return m, updateMembershipCmd(m.backend, m.picker.itemID, toAdd, toRemove)
		}
		return m, nil

	case "esc":
		m.appMode = modeNormal
		m.pendingTitle = ""
		return m, nil
	}

	return m, nil
}

// --- Cursor movement ---

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

	switch m.appMode {
	case modeAddItem, modeAddItemMulti, modeAddProject, modeEditTitle:
		overlay := m.renderInputOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeProjectPicker:
		overlay := m.picker.view(m.width)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	}

	// Normal two-pane view
	statusBar := m.renderStatusBar()
	statusHeight := lipgloss.Height(statusBar)

	paneHeight := m.height - statusHeight - 2
	if paneHeight < 3 {
		paneHeight = 3
	}

	projectPaneWidth := 24
	itemPaneWidth := m.width - projectPaneWidth - 2
	if itemPaneWidth < 20 {
		itemPaneWidth = 20
	}

	leftPane := m.renderProjectPane(projectPaneWidth-2, paneHeight)
	rightPane := m.renderItemPane(itemPaneWidth-2, paneHeight)

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

func (m *App) renderInputOverlay() string {
	var prompt string
	switch m.appMode {
	case modeAddItem:
		prompt = "New item"
	case modeAddItemMulti:
		prompt = "New item (multi-project)"
	case modeAddProject:
		prompt = "New project"
	case modeEditTitle:
		prompt = "Edit title"
	}

	var lines []string
	lines = append(lines, overlayTitleStyle.Render(prompt))
	lines = append(lines, "")
	lines = append(lines, "  "+m.titleInput.View())
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("  [Enter] Confirm  [Esc] Cancel"))

	content := strings.Join(lines, "\n")

	boxWidth := m.width - 4
	if boxWidth > 60 {
		boxWidth = 60
	}
	if boxWidth < 30 {
		boxWidth = 30
	}

	return overlayBoxStyle.Width(boxWidth).Render(content)
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
	status := "○"
	if item.Completed {
		status = "✓"
	}

	multiProject := ""
	if item.ProjectCount > 1 {
		multiProject = " ◈"
	}

	idText := fmt.Sprintf("#%d", item.ID)

	// Build completed lines from plain text to avoid nested ANSI escapes
	var content string
	if item.Completed {
		content = itemCompletedStyle.Render(
			fmt.Sprintf("%s %s%s  %s", status, item.Title, multiProject, idText),
		)
	} else {
		id := itemIDStyle.Render(idText)
		mp := ""
		if multiProject != "" {
			mp = multiProjectStyle.Render(multiProject)
		}
		content = fmt.Sprintf("%s %s%s  %s", status, item.Title, mp, id)
	}

	if selected {
		return itemSelectedStyle.Render("> " + content)
	}
	return itemNormalStyle.Render("  " + content)
}

func (m *App) renderStatusBar() string {
	var left string
	if m.statusMsg != "" {
		left = statusMsgStyle.Render(m.statusMsg)
	} else {
		left = "[space]done [a]dd [x]archive [e]dit [p]rojects [u]ndo"
	}

	var modeStr string
	if m.mode == "remote" {
		modeStr = modeRemoteStyle.Render("REMOTE")
	} else {
		modeStr = modeLocalStyle.Render("LOCAL")
	}

	leftWidth := lipgloss.Width(left)
	modeWidth := lipgloss.Width(modeStr)
	padding := m.width - leftWidth - modeWidth - 4
	if padding < 1 {
		padding = 1
	}

	bar := fmt.Sprintf(" %s%s%s ", left, strings.Repeat(" ", padding), modeStr)
	return statusBarStyle.Render(bar)
}
