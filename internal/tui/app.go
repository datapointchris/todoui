package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/model"
	"github.com/datapointchris/todoui/internal/sync"
)

// App is the top-level Bubble Tea model for the TUI.
type App struct {
	backend backend.Backend
	mode    string // "local" or "remote"

	projects     []model.ProjectWithItemCount
	items        []model.ProjectItemInProject
	blockedSet   map[string]bool
	itemBlockers map[string][]model.ProjectItem

	activePane    pane
	projectCursor int
	itemCursor    int
	projectScroll int // viewport scroll offset for projects
	itemScroll    int // viewport scroll offset for items

	width  int
	height int

	// Modal state
	appMode      appMode
	returnMode   appMode // mode to return to after overlay closes
	titleInput   textinput.Model
	notesInput   textarea.Model
	picker       projectPicker
	pendingTitle string // for A flow: title input → project picker
	statusMsg    string // flash feedback in status bar

	// Item detail view
	itemDetail     *model.ProjectItemDetail
	detailBlockers []model.ProjectItem

	// Filters
	filter filterMode

	// Search
	searchResults []model.ProjectItem
	searchCursor  int
	searchFocused bool // true = typing in input, false = browsing results

	// Move/reorder
	moveOrigPos int

	// Dependency linking
	depItems    []model.ProjectItem
	depCursor   int
	depItemID   string
	depItemName string

	// Navigation
	pendingItemID string // after fetch, select this item

	errorMsg string // transient error shown in status bar
	loading  bool   // true while an async operation is in-flight

	// Sync
	syncEngine *sync.Engine
	syncStatus sync.SyncStatus
}

// NewApp creates a new TUI application backed by the given Backend.
// syncEngine may be nil when sync is disabled.
func NewApp(b backend.Backend, mode string, syncEngine *sync.Engine) *App {
	ti := textinput.New()
	ti.CharLimit = 200

	ta := textarea.New()
	ta.CharLimit = 5000
	ta.ShowLineNumbers = false

	return &App{
		backend:      b,
		mode:         mode,
		blockedSet:   make(map[string]bool),
		itemBlockers: make(map[string][]model.ProjectItem),
		titleInput:   ti,
		notesInput:   ta,
		syncEngine:   syncEngine,
	}
}

// --- Messages ---

type projectsMsg []model.ProjectWithItemCount

type itemsMsg struct {
	items    []model.ProjectItemInProject
	blocked  map[string]bool
	blockers map[string][]model.ProjectItem
}

type (
	itemCreatedMsg       struct{}
	itemUpdatedMsg       struct{}
	projectCreatedMsg    struct{}
	undoResultMsg        string
	itemProjectsMsg      []model.Project
	membershipUpdatedMsg struct{}
	reorderDoneMsg       struct{}
	depLinkedMsg         struct{}
	depUnlinkedMsg       struct{}
)

type itemDetailMsg struct {
	detail   *model.ProjectItemDetail
	blockers []model.ProjectItem
}

type searchResultsMsg []model.ProjectItem

type searchNavigateMsg struct {
	itemID   string
	projects []model.Project
}

type depCandidatesMsg []model.ProjectItem

type depBlockersForUnlinkMsg []model.ProjectItem

type errMsg struct{ error }

// Sync messages
type (
	syncStatusMsg   sync.SyncStatus
	syncPullDoneMsg struct{}
	syncPullErrMsg  struct{ error }
)

// shortID returns the first 8 characters of a UUID for display.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// --- Sync commands ---

func syncPullCmd(e *sync.Engine) tea.Cmd {
	return func() tea.Msg {
		if err := e.Pull(context.Background()); err != nil {
			return syncPullErrMsg{err}
		}
		return syncPullDoneMsg{}
	}
}

func syncStatusTickCmd(e *sync.Engine) tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
		return syncStatusMsg(e.Status())
	})
}

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

func fetchItemsCmd(b backend.Backend, projectID string, filter filterMode) tea.Cmd {
	return func() tea.Msg {
		items, err := b.ListItemsByProject(projectID)
		if err != nil {
			return errMsg{err}
		}

		if filter == filterAll {
			archived, err := b.ListArchived(projectID)
			if err != nil {
				return errMsg{err}
			}
			items = append(items, archived...)
		}

		blockedItems, err := b.ListBlocked()
		if err != nil {
			return errMsg{err}
		}
		blockedSet := make(map[string]bool)
		for _, bi := range blockedItems {
			blockedSet[bi.ID] = true
		}

		blockers := make(map[string][]model.ProjectItem)
		for _, item := range items {
			if blockedSet[item.ID] {
				bs, err := b.GetBlockers(item.ID)
				if err != nil {
					return errMsg{err}
				}
				blockers[item.ID] = bs
			}
		}

		if filter == filterBlocked {
			var filtered []model.ProjectItemInProject
			for _, item := range items {
				if blockedSet[item.ID] {
					filtered = append(filtered, item)
				}
			}
			items = filtered
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

func updateItemCmd(b backend.Backend, id string, input model.UpdateProjectItem) tea.Cmd {
	return func() tea.Msg {
		_, err := b.UpdateItem(id, input)
		if err != nil {
			return errMsg{err}
		}
		return itemUpdatedMsg{}
	}
}

func createProjectCmd(b backend.Backend, input model.CreateProject) tea.Cmd {
	return func() tea.Msg {
		_, err := b.CreateProject(input)
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

func fetchItemProjectsCmd(b backend.Backend, itemID string) tea.Cmd {
	return func() tea.Msg {
		projects, err := b.GetItemProjects(itemID)
		if err != nil {
			return errMsg{err}
		}
		return itemProjectsMsg(projects)
	}
}

func updateMembershipCmd(b backend.Backend, itemID string, toAdd, toRemove []string) tea.Cmd {
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

func fetchItemDetailCmd(b backend.Backend, itemID string, isBlocked bool) tea.Cmd {
	return func() tea.Msg {
		detail, err := b.GetItem(itemID)
		if err != nil {
			return errMsg{err}
		}
		var blockers []model.ProjectItem
		if isBlocked {
			blockers, err = b.GetBlockers(itemID)
			if err != nil {
				return errMsg{err}
			}
		}
		return itemDetailMsg{detail: detail, blockers: blockers}
	}
}

func searchCmd(b backend.Backend, query string) tea.Cmd {
	return func() tea.Msg {
		results, err := b.Search(query)
		if err != nil {
			return errMsg{err}
		}
		return searchResultsMsg(results)
	}
}

func searchNavigateCmd(b backend.Backend, itemID string) tea.Cmd {
	return func() tea.Msg {
		detail, err := b.GetItem(itemID)
		if err != nil {
			return errMsg{err}
		}
		return searchNavigateMsg{itemID: itemID, projects: detail.Projects}
	}
}

func reorderItemCmd(b backend.Backend, itemID, projectID string, newPos int) tea.Cmd {
	return func() tea.Msg {
		if err := b.ReorderItem(itemID, projectID, newPos); err != nil {
			return errMsg{err}
		}
		return reorderDoneMsg{}
	}
}

func fetchDepCandidatesCmd(b backend.Backend) tea.Cmd {
	return func() tea.Msg {
		items, err := b.ListAllItems()
		if err != nil {
			return errMsg{err}
		}
		return depCandidatesMsg(items)
	}
}

func fetchDepBlockersCmd(b backend.Backend, itemID string) tea.Cmd {
	return func() tea.Msg {
		blockers, err := b.GetBlockers(itemID)
		if err != nil {
			return errMsg{err}
		}
		return depBlockersForUnlinkMsg(blockers)
	}
}

func addDependencyCmd(b backend.Backend, itemID, dependsOn string) tea.Cmd {
	return func() tea.Msg {
		if err := b.AddDependency(itemID, dependsOn); err != nil {
			return errMsg{err}
		}
		return depLinkedMsg{}
	}
}

func removeDependencyCmd(b backend.Backend, itemID, dependsOn string) tea.Cmd {
	return func() tea.Msg {
		if err := b.RemoveDependency(itemID, dependsOn); err != nil {
			return errMsg{err}
		}
		return depUnlinkedMsg{}
	}
}

// --- Bubble Tea interface ---

func (m *App) Init() tea.Cmd {
	if m.syncEngine != nil {
		return tea.Batch(
			fetchProjectsCmd(m.backend),
			syncPullCmd(m.syncEngine),
			syncStatusTickCmd(m.syncEngine),
		)
	}
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
			return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
		}
		m.items = nil
		return m, nil

	case itemsMsg:
		m.items = msg.items
		m.blockedSet = msg.blocked
		m.itemBlockers = msg.blockers
		if m.pendingItemID != "" {
			for i, item := range m.items {
				if item.ID == m.pendingItemID {
					m.itemCursor = i
					break
				}
			}
			m.pendingItemID = ""
		} else if m.itemCursor >= len(m.items) {
			m.itemCursor = max(0, len(m.items)-1)
		}
		return m, nil

	case itemCreatedMsg:
		m.statusMsg = "Item created"
		return m, fetchProjectsCmd(m.backend)

	case itemUpdatedMsg:
		if m.statusMsg == "" {
			m.statusMsg = "Item updated"
		}
		if m.appMode == modeItemDetail && m.itemDetail != nil {
			return m, tea.Batch(
				fetchProjectsCmd(m.backend),
				fetchItemDetailCmd(m.backend, m.itemDetail.ID, m.blockedSet[m.itemDetail.ID]),
			)
		}
		return m, fetchProjectsCmd(m.backend)

	case projectCreatedMsg:
		m.statusMsg = "Project created"
		return m, fetchProjectsCmd(m.backend)

	case undoResultMsg:
		m.statusMsg = fmt.Sprintf("Undo: %s", string(msg))
		return m, fetchProjectsCmd(m.backend)

	case itemDetailMsg:
		m.itemDetail = msg.detail
		m.detailBlockers = msg.blockers
		m.appMode = modeItemDetail
		return m, nil

	case itemProjectsMsg:
		if len(m.items) > 0 && m.itemCursor < len(m.items) {
			item := m.items[m.itemCursor]
			m.picker = newPickerForManage(m.projects, msg, item)
			m.appMode = modeProjectPicker
		}
		return m, nil

	case membershipUpdatedMsg:
		m.statusMsg = "Project membership updated"
		if m.returnMode == modeItemDetail && m.itemDetail != nil {
			m.appMode = modeItemDetail
			return m, tea.Batch(
				fetchProjectsCmd(m.backend),
				fetchItemDetailCmd(m.backend, m.itemDetail.ID, m.blockedSet[m.itemDetail.ID]),
			)
		}
		return m, fetchProjectsCmd(m.backend)

	case searchResultsMsg:
		m.searchResults = msg
		m.searchCursor = 0
		m.searchFocused = false
		return m, nil

	case searchNavigateMsg:
		m.appMode = modeNormal
		m.searchResults = nil
		m.filter = filterNone
		for i, p := range m.projects {
			for _, sp := range msg.projects {
				if p.ID == sp.ID {
					m.projectCursor = i
					m.pendingItemID = msg.itemID
					m.activePane = itemPane
					return m, fetchItemsCmd(m.backend, p.ID, filterNone)
				}
			}
		}
		return m, nil

	case reorderDoneMsg:
		m.statusMsg = "Item reordered"
		if len(m.projects) > 0 {
			return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
		}
		return m, nil

	case depCandidatesMsg:
		var filtered []model.ProjectItem
		for _, item := range msg {
			if item.ID != m.depItemID {
				filtered = append(filtered, item)
			}
		}
		m.depItems = filtered
		m.depCursor = 0
		m.appMode = modeDepLink
		return m, nil

	case depBlockersForUnlinkMsg:
		if len(msg) == 0 {
			m.statusMsg = "No dependencies to unlink"
			return m, nil
		}
		m.depItems = msg
		m.depCursor = 0
		m.appMode = modeDepUnlink
		return m, nil

	case depLinkedMsg:
		m.statusMsg = "Dependency linked"
		return m, fetchProjectsCmd(m.backend)

	case depUnlinkedMsg:
		m.statusMsg = "Dependency unlinked"
		return m, fetchProjectsCmd(m.backend)

	case errMsg:
		m.loading = false
		m.errorMsg = msg.Error()
		return m, nil

	case syncPullDoneMsg:
		m.statusMsg = "Synced with server"
		return m, fetchProjectsCmd(m.backend)

	case syncPullErrMsg:
		m.errorMsg = fmt.Sprintf("Sync: %v", msg.error)
		return m, nil

	case syncStatusMsg:
		m.syncStatus = sync.SyncStatus(msg)
		if m.syncEngine != nil {
			return m, syncStatusTickCmd(m.syncEngine)
		}
		return m, nil
	}

	return m, nil
}

// --- Key handling ---

func (m *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	// Any keypress clears transient messages
	m.errorMsg = ""

	switch m.appMode {
	case modeNormal:
		m.statusMsg = ""
		return m.handleNormalKey(msg)
	case modeAddItem, modeAddItemMulti, modeAddProject, modeEditTitle:
		return m.handleInputKey(msg)
	case modeProjectPicker:
		return m.handlePickerKey(msg)
	case modeItemDetail:
		return m.handleDetailKey(msg)
	case modeEditNotes:
		return m.handleNotesKey(msg)
	case modeHelp:
		return m.handleHelpKey(msg)
	case modeSearch:
		return m.handleSearchKey(msg)
	case modeMove:
		return m.handleMoveKey(msg)
	case modeDepLink, modeDepUnlink:
		return m.handleDepLinkKey(msg)
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

	case "g":
		if m.activePane == projectPane {
			if m.projectCursor > 0 {
				m.projectCursor = 0
				return m, fetchItemsCmd(m.backend, m.projects[0].ID, m.filter)
			}
		} else {
			m.itemCursor = 0
		}
		return m, nil

	case "G":
		if m.activePane == projectPane {
			last := len(m.projects) - 1
			if last >= 0 && m.projectCursor != last {
				m.projectCursor = last
				return m, fetchItemsCmd(m.backend, m.projects[last].ID, m.filter)
			}
		} else {
			if last := len(m.items) - 1; last >= 0 {
				m.itemCursor = last
			}
		}
		return m, nil

	case "ctrl+d":
		half := m.paneContentHeight() / 2
		if half < 1 {
			half = 1
		}
		if m.activePane == projectPane {
			m.projectCursor = min(m.projectCursor+half, max(0, len(m.projects)-1))
			if len(m.projects) > 0 {
				return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
			}
		} else {
			m.itemCursor = min(m.itemCursor+half, max(0, len(m.items)-1))
		}
		return m, nil

	case "ctrl+u":
		half := m.paneContentHeight() / 2
		if half < 1 {
			half = 1
		}
		if m.activePane == projectPane {
			m.projectCursor = max(m.projectCursor-half, 0)
			if len(m.projects) > 0 {
				return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
			}
		} else {
			m.itemCursor = max(m.itemCursor-half, 0)
		}
		return m, nil

	case "enter":
		if m.activePane == projectPane && len(m.projects) > 0 {
			m.activePane = itemPane
			return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
		}
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			return m, fetchItemDetailCmd(m.backend, item.ID, m.blockedSet[item.ID])
		}
		return m, nil

	// --- Actions ---

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
			if toggled {
				m.statusMsg = "Marked done"
			} else {
				m.statusMsg = "Marked incomplete"
			}
			return m, updateItemCmd(m.backend, item.ID, model.UpdateProjectItem{Completed: &toggled})
		}
		return m, nil

	case "x":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			archived := true
			m.statusMsg = "Archived"
			return m, updateItemCmd(m.backend, item.ID, model.UpdateProjectItem{Archived: &archived})
		}
		return m, nil

	case "e":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			m.titleInput.SetValue(item.Title)
			m.titleInput.Placeholder = ""
			cmd := m.titleInput.Focus()
			m.returnMode = modeNormal
			m.appMode = modeEditTitle
			return m, cmd
		}
		return m, nil

	case "n":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			notes := ""
			if item.Notes != nil {
				notes = *item.Notes
			}
			m.notesInput.SetValue(notes)
			cmd := m.notesInput.Focus()
			m.returnMode = modeNormal
			m.appMode = modeEditNotes
			return m, cmd
		}
		return m, nil

	case "p":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			m.returnMode = modeNormal
			return m, fetchItemProjectsCmd(m.backend, item.ID)
		}
		return m, nil

	case "u":
		return m, undoCmd(m.backend)

	// --- Phase 6: Advanced ---

	case "?":
		m.appMode = modeHelp
		return m, nil

	case "1":
		if m.filter == filterBlocked {
			m.filter = filterNone
		} else {
			m.filter = filterBlocked
		}
		if len(m.projects) > 0 {
			return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
		}
		return m, nil

	case "2":
		if m.filter == filterAll {
			m.filter = filterNone
		} else {
			m.filter = filterAll
		}
		if len(m.projects) > 0 {
			return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
		}
		return m, nil

	case "0":
		m.filter = filterNone
		if len(m.projects) > 0 {
			return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
		}
		return m, nil

	case "/":
		m.titleInput.SetValue("")
		m.titleInput.Placeholder = "Search..."
		cmd := m.titleInput.Focus()
		m.searchResults = nil
		m.searchCursor = 0
		m.searchFocused = true
		m.appMode = modeSearch
		return m, cmd

	case "m":
		if m.activePane == itemPane && len(m.items) > 1 {
			m.moveOrigPos = m.itemCursor
			m.appMode = modeMove
		}
		return m, nil

	case "b":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			m.depItemID = item.ID
			m.depItemName = item.Title
			return m, fetchDepCandidatesCmd(m.backend)
		}
		return m, nil

	case "B":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			m.depItemID = item.ID
			m.depItemName = item.Title
			return m, fetchDepBlockersCmd(m.backend, item.ID)
		}
		return m, nil
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
				ProjectIDs: []string{projectID},
			})

		case modeAddItemMulti:
			m.pendingTitle = value
			m.titleInput.Blur()
			currentProjectID := m.projects[m.projectCursor].ID
			m.picker = newPickerForCreate(m.projects, currentProjectID, value)
			m.appMode = modeProjectPicker
			return m, nil

		case modeAddProject:
			m.appMode = modeNormal
			m.titleInput.Blur()
			return m, createProjectCmd(m.backend, model.CreateProject{Name: value})

		case modeEditTitle:
			m.appMode = m.returnMode
			m.titleInput.Blur()
			item := m.items[m.itemCursor]
			return m, updateItemCmd(m.backend, item.ID, model.UpdateProjectItem{Title: &value})
		}

	case "esc":
		m.appMode = m.returnMode
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

	case "g":
		m.picker.cursor = 0
		return m, nil

	case "G":
		if last := len(m.picker.projects) - 1; last >= 0 {
			m.picker.cursor = last
		}
		return m, nil

	case " ":
		m.picker.toggle()
		return m, nil

	case "enter":
		m.appMode = m.returnMode

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
		m.appMode = m.returnMode
		m.pendingTitle = ""
		return m, nil
	}

	return m, nil
}

func (m *App) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.appMode = modeNormal
		m.itemDetail = nil
		m.detailBlockers = nil
		return m, nil

	case " ":
		if m.itemDetail != nil {
			if m.blockedSet[m.itemDetail.ID] && !m.itemDetail.Completed {
				m.statusMsg = "Cannot complete: item has unresolved blockers"
				return m, nil
			}
			toggled := !m.itemDetail.Completed
			if toggled {
				m.statusMsg = "Marked done"
			} else {
				m.statusMsg = "Marked incomplete"
			}
			return m, updateItemCmd(m.backend, m.itemDetail.ID, model.UpdateProjectItem{Completed: &toggled})
		}
		return m, nil

	case "x":
		if m.itemDetail != nil {
			archived := true
			id := m.itemDetail.ID
			m.statusMsg = "Archived"
			m.appMode = modeNormal
			m.itemDetail = nil
			return m, updateItemCmd(m.backend, id, model.UpdateProjectItem{Archived: &archived})
		}
		return m, nil

	case "e":
		if m.itemDetail != nil {
			m.titleInput.SetValue(m.itemDetail.Title)
			m.titleInput.Placeholder = ""
			cmd := m.titleInput.Focus()
			m.returnMode = modeItemDetail
			m.appMode = modeEditTitle
			return m, cmd
		}
		return m, nil

	case "n":
		if m.itemDetail != nil {
			notes := ""
			if m.itemDetail.Notes != nil {
				notes = *m.itemDetail.Notes
			}
			m.notesInput.SetValue(notes)
			cmd := m.notesInput.Focus()
			m.returnMode = modeItemDetail
			m.appMode = modeEditNotes
			return m, cmd
		}
		return m, nil

	case "p":
		if m.itemDetail != nil {
			m.returnMode = modeItemDetail
			return m, fetchItemProjectsCmd(m.backend, m.itemDetail.ID)
		}
		return m, nil

	case "u":
		return m, undoCmd(m.backend)
	}

	return m, nil
}

func (m *App) handleNotesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		notes := m.notesInput.Value()
		m.appMode = m.returnMode
		m.notesInput.Blur()
		itemID := m.items[m.itemCursor].ID
		if m.itemDetail != nil {
			itemID = m.itemDetail.ID
		}
		return m, updateItemCmd(m.backend, itemID, model.UpdateProjectItem{Notes: &notes})

	case "esc":
		m.appMode = m.returnMode
		m.notesInput.Blur()
		return m, nil

	default:
		var cmd tea.Cmd
		m.notesInput, cmd = m.notesInput.Update(msg)
		return m, cmd
	}
}

func (m *App) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "?", "q":
		m.appMode = modeNormal
	}
	return m, nil
}

func (m *App) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchFocused {
		switch msg.String() {
		case "enter":
			query := strings.TrimSpace(m.titleInput.Value())
			if query == "" {
				return m, nil
			}
			return m, searchCmd(m.backend, query)

		case "esc":
			m.appMode = modeNormal
			m.titleInput.Blur()
			m.searchResults = nil
			return m, nil

		case "down", "tab":
			if len(m.searchResults) > 0 {
				m.searchFocused = false
				m.searchCursor = 0
			}
			return m, nil

		default:
			var cmd tea.Cmd
			m.titleInput, cmd = m.titleInput.Update(msg)
			return m, cmd
		}
	}

	// Browsing results
	switch msg.String() {
	case "j", "down":
		if m.searchCursor < len(m.searchResults)-1 {
			m.searchCursor++
		}
		return m, nil

	case "k", "up":
		if m.searchCursor > 0 {
			m.searchCursor--
		} else {
			m.searchFocused = true
		}
		return m, nil

	case "g":
		m.searchCursor = 0
		return m, nil

	case "G":
		if last := len(m.searchResults) - 1; last >= 0 {
			m.searchCursor = last
		}
		return m, nil

	case "enter":
		if len(m.searchResults) > 0 {
			item := m.searchResults[m.searchCursor]
			m.titleInput.Blur()
			return m, searchNavigateCmd(m.backend, item.ID)
		}
		return m, nil

	case "esc", "/":
		m.searchFocused = true
		return m, nil
	}

	return m, nil
}

func (m *App) handleMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.itemCursor < len(m.items)-1 {
			m.items[m.itemCursor], m.items[m.itemCursor+1] = m.items[m.itemCursor+1], m.items[m.itemCursor]
			m.itemCursor++
		}
		return m, nil

	case "k", "up":
		if m.itemCursor > 0 {
			m.items[m.itemCursor], m.items[m.itemCursor-1] = m.items[m.itemCursor-1], m.items[m.itemCursor]
			m.itemCursor--
		}
		return m, nil

	case "g":
		for m.itemCursor > 0 {
			m.items[m.itemCursor], m.items[m.itemCursor-1] = m.items[m.itemCursor-1], m.items[m.itemCursor]
			m.itemCursor--
		}
		return m, nil

	case "G":
		for m.itemCursor < len(m.items)-1 {
			m.items[m.itemCursor], m.items[m.itemCursor+1] = m.items[m.itemCursor+1], m.items[m.itemCursor]
			m.itemCursor++
		}
		return m, nil

	case "enter":
		m.appMode = modeNormal
		item := m.items[m.itemCursor]
		projectID := m.projects[m.projectCursor].ID
		return m, reorderItemCmd(m.backend, item.ID, projectID, m.itemCursor)

	case "esc":
		m.appMode = modeNormal
		if len(m.projects) > 0 {
			return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
		}
		return m, nil
	}

	return m, nil
}

func (m *App) handleDepLinkKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.depCursor < len(m.depItems)-1 {
			m.depCursor++
		}
		return m, nil

	case "k", "up":
		if m.depCursor > 0 {
			m.depCursor--
		}
		return m, nil

	case "g":
		m.depCursor = 0
		return m, nil

	case "G":
		if last := len(m.depItems) - 1; last >= 0 {
			m.depCursor = last
		}
		return m, nil

	case "enter":
		if len(m.depItems) > 0 {
			selected := m.depItems[m.depCursor]
			wasUnlink := m.appMode == modeDepUnlink
			m.appMode = modeNormal
			if wasUnlink {
				return m, removeDependencyCmd(m.backend, m.depItemID, selected.ID)
			}
			return m, addDependencyCmd(m.backend, m.depItemID, selected.ID)
		}
		return m, nil

	case "esc":
		m.appMode = modeNormal
		return m, nil
	}

	return m, nil
}

// --- Cursor movement ---

func (m *App) cursorDown() tea.Cmd {
	if m.activePane == projectPane {
		if m.projectCursor < len(m.projects)-1 {
			m.projectCursor++
			return fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
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
			return fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
		}
	} else {
		if m.itemCursor > 0 {
			m.itemCursor--
		}
	}
	return nil
}

// syncScroll adjusts scroll offset so the cursor stays visible within the viewport.
func syncScroll(cursor, scroll, viewHeight int) int {
	if viewHeight <= 0 {
		return 0
	}
	if cursor < scroll {
		return cursor
	}
	if cursor >= scroll+viewHeight {
		return cursor - viewHeight + 1
	}
	return scroll
}

// paneContentHeight returns how many list items fit in a pane (minus title and status bar).
func (m *App) paneContentHeight() int {
	paneHeight := m.height - 3 // status bar (~1 line) + borders (2)
	if paneHeight < 3 {
		paneHeight = 3
	}
	return paneHeight - 1 // subtract title line
}

// --- View ---

func (m *App) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	switch m.appMode {
	case modeAddItem, modeAddItemMulti, modeAddProject, modeEditTitle:
		overlay := m.renderInputOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeProjectPicker:
		overlay := m.picker.view(m.width)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeItemDetail:
		overlay := m.renderDetailOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeEditNotes:
		overlay := m.renderNotesOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeHelp:
		overlay := m.renderHelpOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeSearch:
		overlay := m.renderSearchOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeDepLink, modeDepUnlink:
		overlay := m.renderDepLinkOverlay()
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

func (m *App) renderDetailOverlay() string {
	d := m.itemDetail
	if d == nil {
		return ""
	}

	header := overlayTitleStyle.Render(fmt.Sprintf("Item %s", shortID(d.ID)))

	status := "○ incomplete"
	if d.Completed {
		status = "✓ completed"
	}
	if d.Archived {
		status = "▪ archived"
	}

	var projectNames []string
	for _, p := range d.Projects {
		projectNames = append(projectNames, p.Name)
	}

	var lines []string
	lines = append(lines, header)
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s    %s", d.Title, dimStyle.Render(status)))
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  Projects: %s", strings.Join(projectNames, ", "))))
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  Created: %s", d.CreatedAt.Format("Jan 2, 2006"))))

	if d.Notes != nil && *d.Notes != "" {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("  ─── Notes ─────────────────────────"))
		for _, noteLine := range strings.Split(*d.Notes, "\n") {
			lines = append(lines, "  "+noteLine)
		}
	}

	if len(m.detailBlockers) > 0 {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("  ─── Blocked by ────────────────────"))
		for _, b := range m.detailBlockers {
			lines = append(lines, blockerStyle.Render(
				fmt.Sprintf("○ %s (%s)", b.Title, shortID(b.ID)),
			))
		}
	}

	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("  [e]dit  [n]otes  [p]rojects  [space]done  [x]archive  [Esc] close"))

	content := strings.Join(lines, "\n")

	boxWidth := m.width - 4
	if boxWidth > 70 {
		boxWidth = 70
	}
	if boxWidth < 40 {
		boxWidth = 40
	}

	return overlayBoxStyle.Width(boxWidth).Render(content)
}

func (m *App) renderNotesOverlay() string {
	var itemTitle string
	if m.itemDetail != nil {
		itemTitle = fmt.Sprintf("%s (%s)", m.itemDetail.Title, shortID(m.itemDetail.ID))
	} else if len(m.items) > 0 && m.itemCursor < len(m.items) {
		item := m.items[m.itemCursor]
		itemTitle = fmt.Sprintf("%s (%s)", item.Title, shortID(item.ID))
	}

	var lines []string
	lines = append(lines, overlayTitleStyle.Render("Edit Notes"))
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  Item: %s", itemTitle)))
	lines = append(lines, "")
	lines = append(lines, m.notesInput.View())
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("  [Ctrl+S] Save  [Esc] Cancel"))

	content := strings.Join(lines, "\n")

	boxWidth := m.width - 4
	if boxWidth > 70 {
		boxWidth = 70
	}
	if boxWidth < 40 {
		boxWidth = 40
	}

	return overlayBoxStyle.Width(boxWidth).Render(content)
}

func (m *App) renderHelpOverlay() string {
	var lines []string
	lines = append(lines, overlayTitleStyle.Render("Keybindings"))
	lines = append(lines, "")

	nav := `  Navigation
  j/k ↑/↓       Navigate items
  g/G            Jump to top/bottom
  Ctrl+d/u       Half-page down/up
  h/l ←/→       Switch panes
  Tab            Toggle pane focus
  Enter          Select / item detail`

	lines = append(lines, nav)

	if m.activePane == projectPane {
		actions := `
  Project Pane
  a              Add project`
		lines = append(lines, actions)
	} else {
		actions := `
  Item Actions
  space          Toggle done
  a              Add item
  A              Add item to multiple projects
  e              Edit title
  n              Edit notes
  x              Archive item
  p              Manage project membership
  b              Link dependency (blocked by)
  B              Unlink dependency
  m              Move/reorder item`
		lines = append(lines, actions)
	}

	global := `
  Global
  u              Undo last action
  /              Search
  1              Filter: blocked only (toggle)
  2              Filter: all + archived (toggle)
  0              Filter: reset
  q  Ctrl+C      Quit
  ?              Toggle this help`

	lines = append(lines, global)
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("  [Esc] or [?] Close"))

	content := strings.Join(lines, "\n")
	return overlayBoxStyle.Width(50).Render(content)
}

func (m *App) renderSearchOverlay() string {
	var lines []string
	lines = append(lines, overlayTitleStyle.Render("Search"))
	lines = append(lines, "")
	lines = append(lines, "  "+m.titleInput.View())
	lines = append(lines, "")

	if len(m.searchResults) == 0 && !m.searchFocused {
		lines = append(lines, dimStyle.Render("  No results"))
	}

	for i, item := range m.searchResults {
		status := "○"
		if item.Completed {
			status = "✓"
		}
		line := fmt.Sprintf("%s %s  %s", status, item.Title, itemIDStyle.Render(shortID(item.ID)))
		if !m.searchFocused && i == m.searchCursor {
			line = searchResultSelectedStyle.Render("> " + line)
		} else {
			line = searchResultNormalStyle.Render("  " + line)
		}
		lines = append(lines, line)
	}

	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("  [Enter] Search/Go  [↓] Results  [Esc] Close"))

	content := strings.Join(lines, "\n")

	boxWidth := m.width - 4
	if boxWidth > 70 {
		boxWidth = 70
	}
	if boxWidth < 40 {
		boxWidth = 40
	}

	return overlayBoxStyle.Width(boxWidth).Render(content)
}

func (m *App) renderDepLinkOverlay() string {
	var header string
	if m.appMode == modeDepUnlink {
		header = fmt.Sprintf("Unlink dependency from: %s (%s)", m.depItemName, shortID(m.depItemID))
	} else {
		header = fmt.Sprintf("Link dependency for: %s (%s)", m.depItemName, shortID(m.depItemID))
	}

	var lines []string
	lines = append(lines, overlayTitleStyle.Render(header))
	lines = append(lines, "")

	if len(m.depItems) == 0 {
		lines = append(lines, dimStyle.Render("  No items available"))
	}

	for i, item := range m.depItems {
		status := "○"
		if item.Completed {
			status = "✓"
		}
		line := fmt.Sprintf("%s %s  %s", status, item.Title, itemIDStyle.Render(shortID(item.ID)))
		if i == m.depCursor {
			line = pickerSelectedStyle.Render("> " + line)
		} else {
			line = pickerNormalStyle.Render("  " + line)
		}
		lines = append(lines, line)
	}

	lines = append(lines, "")
	if m.appMode == modeDepUnlink {
		lines = append(lines, dimStyle.Render("  [Enter] Unlink  [Esc] Cancel"))
	} else {
		lines = append(lines, dimStyle.Render("  [Enter] Link  [Esc] Cancel"))
	}

	content := strings.Join(lines, "\n")

	boxWidth := m.width - 4
	if boxWidth > 70 {
		boxWidth = 70
	}
	if boxWidth < 40 {
		boxWidth = 40
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

	viewHeight := height - 1 // subtract title
	m.projectScroll = syncScroll(m.projectCursor, m.projectScroll, viewHeight)

	end := m.projectScroll + viewHeight
	if end > len(m.projects) {
		end = len(m.projects)
	}

	for i := m.projectScroll; i < end; i++ {
		p := m.projects[i]
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

	if len(m.projects) > viewHeight {
		scrollInfo := dimStyle.Render(fmt.Sprintf(" %d/%d", m.projectCursor+1, len(m.projects)))
		lines = append(lines, scrollInfo)
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

	if m.filter != filterNone {
		var filterLabel string
		switch m.filter {
		case filterBlocked:
			filterLabel = " [BLOCKED]"
		case filterAll:
			filterLabel = " [ALL]"
		}
		titleText += filterIndicatorStyle.Render(filterLabel)
	}

	title := paneTitleStyle.Render(titleText)
	var lines []string
	lines = append(lines, title)

	if len(m.items) == 0 {
		if m.filter != filterNone {
			lines = append(lines, emptyStyle.Render("No matching items"))
		} else {
			lines = append(lines, emptyStyle.Render("No items"))
		}
		return strings.Join(lines, "\n")
	}

	viewHeight := height - 1 // subtract title
	m.itemScroll = syncScroll(m.itemCursor, m.itemScroll, viewHeight)

	end := m.itemScroll + viewHeight
	if end > len(m.items) {
		end = len(m.items)
	}

	linesUsed := 0
	for i := m.itemScroll; i < end; i++ {
		item := m.items[i]
		isMoving := m.appMode == modeMove && i == m.itemCursor
		line := m.renderItemLine(item, i == m.itemCursor, width, isMoving)
		lines = append(lines, line)
		linesUsed++

		if blockers, ok := m.itemBlockers[item.ID]; ok && len(blockers) > 0 {
			for _, b := range blockers {
				if linesUsed >= viewHeight {
					break
				}
				blockerLine := blockerStyle.Render(
					fmt.Sprintf("└─ blocked by: %s (%s)", b.Title, shortID(b.ID)),
				)
				lines = append(lines, blockerLine)
				linesUsed++
			}
		}

		if item.Notes != nil && *item.Notes != "" && linesUsed < viewHeight {
			preview := strings.SplitN(*item.Notes, "\n", 2)[0]
			maxLen := width - 6
			if maxLen > 0 && len(preview) > maxLen {
				preview = preview[:maxLen] + "…"
			}
			lines = append(lines, notesPreviewStyle.Render(preview))
			linesUsed++
		}

		if linesUsed >= viewHeight {
			break
		}
	}

	if len(m.items) > viewHeight {
		scrollInfo := dimStyle.Render(fmt.Sprintf(" %d/%d", m.itemCursor+1, len(m.items)))
		lines = append(lines, scrollInfo)
	}

	return strings.Join(lines, "\n")
}

func (m *App) renderItemLine(item model.ProjectItemInProject, selected bool, width int, moving bool) string {
	status := "○"
	if item.Completed {
		status = "✓"
	}

	multiProject := ""
	if item.ProjectCount > 1 {
		multiProject = " ◈"
	}

	hasNotes := ""
	if item.Notes != nil && *item.Notes != "" {
		hasNotes = " ≡"
	}

	idText := shortID(item.ID)

	var content string
	if item.Completed {
		content = itemCompletedStyle.Render(
			fmt.Sprintf("%s %s%s%s  %s", status, item.Title, multiProject, hasNotes, idText),
		)
	} else {
		id := itemIDStyle.Render(idText)
		mp := ""
		if multiProject != "" {
			mp = multiProjectStyle.Render(multiProject)
		}
		notes := ""
		if hasNotes != "" {
			notes = notesIndicatorStyle.Render(hasNotes)
		}
		content = fmt.Sprintf("%s %s%s%s  %s", status, item.Title, mp, notes, id)
	}

	if moving {
		return moveIndicatorStyle.Render("▶ " + content + " ◀ MOVING")
	}
	if selected {
		return itemSelectedStyle.Render("> " + content)
	}
	return itemNormalStyle.Render("  " + content)
}

func (m *App) statusBarHints() string {
	switch {
	case m.statusMsg != "":
		return statusMsgStyle.Render(m.statusMsg)
	case m.appMode == modeMove:
		return moveIndicatorStyle.Render("[j/k] Move  [g/G] Top/Bottom  [Enter] Confirm  [Esc] Cancel")
	case m.activePane == projectPane:
		hints := "[a]dd project [Enter]select [Tab]items [/]search [?]help"
		if m.filter != filterNone {
			hints += " [0]reset filter"
		}
		return hints
	default:
		hints := "[space]done [a]dd [x]archive [e]dit [n]otes [b]lock [/]search [?]help"
		if m.filter != filterNone {
			hints += " [0]reset"
		}
		return hints
	}
}

func (m *App) renderStatusBar() string {
	var left string
	switch {
	case m.errorMsg != "":
		left = errorMsgStyle.Render("Error: " + m.errorMsg)
	case m.loading:
		left = dimStyle.Render("Loading...")
	case m.statusMsg != "":
		left = statusMsgStyle.Render(m.statusMsg)
	default:
		left = m.statusBarHints()
	}

	var modeStr string
	switch {
	case m.syncEngine != nil:
		s := m.syncStatus
		switch {
		case s.Syncing:
			modeStr = syncingStyle.Render("SYNCING...")
		case s.LastError != "":
			modeStr = syncErrorStyle.Render("SYNC ERR")
		case s.PendingCount > 0:
			modeStr = syncPendingStyle.Render(fmt.Sprintf("PENDING (%d)", s.PendingCount))
		default:
			modeStr = syncOKStyle.Render("SYNCED")
		}
	case m.mode == "remote":
		modeStr = modeRemoteStyle.Render("REMOTE")
	default:
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
