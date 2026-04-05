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

	projects           []model.ProjectWithItemCount
	items              []model.ProjectItemInProject
	blockedSet         map[string]bool
	itemBlockers       map[string][]model.ProjectItem
	itemProjectNames   map[string][]string // item ID → project names it belongs to
	hasIncompleteTasks map[string]bool
	taskCounts         map[string][2]int // item ID → [completed, total]
	itemTasks          map[string][]model.ProjectItemTask

	// Task focus in item pane
	itemTaskFocused bool
	itemTaskCursor  int
	itemAddingTask  bool

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
	itemDetail            *model.ProjectItemDetail
	detailBlockers        []model.ProjectItem
	detailBlockerProjects map[string][]string
	detailTasks           []model.ProjectItemTask
	detailTaskCursor      int
	detailTaskFocused     bool // true when navigating in the task list
	addingTask            bool // true when typing a new task title

	// Filters
	filter filterMode

	// Search
	searchResults []model.ProjectItem
	searchCursor  int
	searchFocused bool // true = typing in input, false = browsing results

	// Move/reorder
	moveOrigPos int

	// Dependency linking
	depItems     []model.ProjectItem // flat list of selectable items (filtered view)
	depAllItems  []depGroup          // all items grouped by project (source of truth)
	depCursor    int
	depItemID    string
	depItemName  string
	depFilter    textinput.Model
	depFiltering bool // true when typing in filter input

	// All Items view
	showingAll       bool            // true when the "All" pseudo-project is selected
	allGroups        []allItemGroup  // project groups for rendering headers in grouped views
	selectedProjects map[string]bool // multi-select: project IDs toggled via space

	// Navigation
	pendingItemID string // after fetch, select this item

	errorMsg string // transient error shown in status bar
	loading  bool   // true while an async operation is in-flight

	// Sync
	syncEngine *sync.Engine
	syncStatus sync.SyncStatus

	// Display info
	dbPath string
}

// NewApp creates a new TUI application backed by the given Backend.
// syncEngine may be nil when sync is disabled.
func NewApp(b backend.Backend, syncEngine *sync.Engine, dbPath string) *App {
	ti := textinput.New()
	ti.CharLimit = 200

	ta := textarea.New()
	ta.CharLimit = 5000
	ta.ShowLineNumbers = false

	df := textinput.New()
	df.Placeholder = "type to filter..."
	df.CharLimit = 100

	return &App{
		backend:      b,
		blockedSet:   make(map[string]bool),
		itemBlockers: make(map[string][]model.ProjectItem),
		titleInput:   ti,
		notesInput:   ta,
		depFilter:    df,
		syncEngine:   syncEngine,
		dbPath:       dbPath,
	}
}

// depGroup holds items belonging to a single project, for display in the dep overlay.
type depGroup struct {
	projectName string
	items       []model.ProjectItem
}

// allItemGroup holds items belonging to a single project, for display in the All Items view.
type allItemGroup struct {
	projectName string
	projectID   string
	startIndex  int // index into the flat m.items slice where this group starts
}

func flattenDepGroups(groups []depGroup) []model.ProjectItem {
	var flat []model.ProjectItem
	for _, g := range groups {
		flat = append(flat, g.items...)
	}
	return flat
}

func filterDepGroups(groups []depGroup, query string) []depGroup {
	query = strings.ToLower(query)
	var result []depGroup
	for _, g := range groups {
		var items []model.ProjectItem
		for _, item := range g.items {
			if strings.Contains(strings.ToLower(item.Title), query) {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			result = append(result, depGroup{projectName: g.projectName, items: items})
		}
	}
	return result
}

// --- Messages ---

type projectsMsg []model.ProjectWithItemCount

type itemsMsg struct {
	items              []model.ProjectItemInProject
	blocked            map[string]bool
	blockers           map[string][]model.ProjectItem
	projectNames       map[string][]string // blocker item ID → project names
	hasIncompleteTasks map[string]bool
	taskCounts         map[string][2]int // item ID → [completed, total]
	itemTasks          map[string][]model.ProjectItemTask
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
	detail       *model.ProjectItemDetail
	blockers     []model.ProjectItem
	projectNames map[string][]string
	tasks        []model.ProjectItemTask
}

type (
	taskCreatedMsg struct{}
	taskUpdatedMsg struct{}
	taskDeletedMsg struct{}
)

type searchResultsMsg []model.ProjectItem

type searchNavigateMsg struct {
	itemID   string
	projects []model.Project
}

type allItemsMsg struct {
	itemsMsg
	groups []allItemGroup
}

type depCandidatesMsg []depGroup

type depBlockersForUnlinkMsg []model.ProjectItem

type errMsg struct{ error }

// Status flash clear
type clearStatusMsg struct{}

// Sync messages
type (
	syncStatusMsg   sync.SyncStatus
	syncPullDoneMsg struct{}
	syncPullErrMsg  struct{ error }
)

const statusFlashDuration = 3 * time.Second

func (m *App) flash(msg string) tea.Cmd {
	m.statusMsg = msg
	return tea.Tick(statusFlashDuration, func(_ time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

// shortID returns the first 8 characters of a UUID for display.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func wrapLine(s string, maxWidth int) []string {
	if len(s) <= maxWidth {
		return []string{s}
	}
	var result []string
	for len(s) > maxWidth {
		// Try to break at a space
		cut := maxWidth
		for cut > 0 && s[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = maxWidth // no space found, hard break
		}
		result = append(result, s[:cut])
		s = strings.TrimLeft(s[cut:], " ")
	}
	if len(s) > 0 {
		result = append(result, s)
	}
	return result
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
		projectNames := make(map[string][]string)
		for _, item := range items {
			if blockedSet[item.ID] {
				bs, err := b.GetBlockers(item.ID)
				if err != nil {
					return errMsg{err}
				}
				blockers[item.ID] = bs
				for _, blocker := range bs {
					if _, seen := projectNames[blocker.ID]; !seen {
						ps, err := b.GetItemProjects(blocker.ID)
						if err != nil {
							return errMsg{err}
						}
						var names []string
						for _, p := range ps {
							names = append(names, p.Name)
						}
						projectNames[blocker.ID] = names
					}
				}
			}
		}

		hasIncompleteTasks := make(map[string]bool)
		taskCounts := make(map[string][2]int)
		allTasks := make(map[string][]model.ProjectItemTask)
		for _, item := range items {
			tasks, err := b.ListTasks(item.ID)
			if err != nil {
				return errMsg{err}
			}
			if len(tasks) > 0 {
				allTasks[item.ID] = tasks
				done := 0
				for _, t := range tasks {
					if t.Completed {
						done++
					}
				}
				taskCounts[item.ID] = [2]int{done, len(tasks)}
				if done < len(tasks) {
					hasIncompleteTasks[item.ID] = true
				}
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

		return itemsMsg{items: items, blocked: blockedSet, blockers: blockers, projectNames: projectNames, hasIncompleteTasks: hasIncompleteTasks, taskCounts: taskCounts, itemTasks: allTasks}
	}
}

func fetchAllItemsCmd(b backend.Backend, projects []model.ProjectWithItemCount, filter filterMode) tea.Cmd {
	return func() tea.Msg {
		var groups []allItemGroup
		var allItems []model.ProjectItemInProject

		for _, p := range projects {
			items, err := b.ListItemsByProject(p.ID)
			if err != nil {
				return errMsg{err}
			}
			if filter == filterAll {
				archived, err := b.ListArchived(p.ID)
				if err != nil {
					return errMsg{err}
				}
				items = append(items, archived...)
			}
			if len(items) > 0 {
				groups = append(groups, allItemGroup{
					projectName: p.Name,
					projectID:   p.ID,
					startIndex:  len(allItems),
				})
				allItems = append(allItems, items...)
			}
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
		projectNames := make(map[string][]string)
		for _, item := range allItems {
			if blockedSet[item.ID] {
				bs, err := b.GetBlockers(item.ID)
				if err != nil {
					return errMsg{err}
				}
				blockers[item.ID] = bs
				for _, blocker := range bs {
					if _, seen := projectNames[blocker.ID]; !seen {
						ps, err := b.GetItemProjects(blocker.ID)
						if err != nil {
							return errMsg{err}
						}
						var names []string
						for _, pp := range ps {
							names = append(names, pp.Name)
						}
						projectNames[blocker.ID] = names
					}
				}
			}
		}

		hasIncompleteTasks := make(map[string]bool)
		taskCounts := make(map[string][2]int)
		allTasks := make(map[string][]model.ProjectItemTask)
		seen := make(map[string]bool)
		for _, item := range allItems {
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			tasks, err := b.ListTasks(item.ID)
			if err != nil {
				return errMsg{err}
			}
			if len(tasks) > 0 {
				allTasks[item.ID] = tasks
				done := 0
				for _, t := range tasks {
					if t.Completed {
						done++
					}
				}
				taskCounts[item.ID] = [2]int{done, len(tasks)}
				if done < len(tasks) {
					hasIncompleteTasks[item.ID] = true
				}
			}
		}

		if filter == filterBlocked {
			var filteredItems []model.ProjectItemInProject
			var filteredGroups []allItemGroup
			for _, g := range groups {
				start := len(filteredItems)
				end := g.startIndex + groupItemCount(groups, g, len(allItems))
				for i := g.startIndex; i < end; i++ {
					if blockedSet[allItems[i].ID] {
						filteredItems = append(filteredItems, allItems[i])
					}
				}
				if len(filteredItems) > start {
					filteredGroups = append(filteredGroups, allItemGroup{
						projectName: g.projectName,
						projectID:   g.projectID,
						startIndex:  start,
					})
				}
			}
			allItems = filteredItems
			groups = filteredGroups
		}

		return allItemsMsg{
			itemsMsg: itemsMsg{
				items:              allItems,
				blocked:            blockedSet,
				blockers:           blockers,
				projectNames:       projectNames,
				hasIncompleteTasks: hasIncompleteTasks,
				taskCounts:         taskCounts,
				itemTasks:          allTasks,
			},
			groups: groups,
		}
	}
}

// groupItemCount returns the number of items in a group.
func groupItemCount(groups []allItemGroup, g allItemGroup, totalItems int) int {
	for i, gg := range groups {
		if gg.startIndex == g.startIndex {
			if i+1 < len(groups) {
				return groups[i+1].startIndex - g.startIndex
			}
			return totalItems - g.startIndex
		}
	}
	return 0
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
		pnames := make(map[string][]string)
		if isBlocked {
			blockers, err = b.GetBlockers(itemID)
			if err != nil {
				return errMsg{err}
			}
			for _, bl := range blockers {
				ps, err := b.GetItemProjects(bl.ID)
				if err != nil {
					return errMsg{err}
				}
				var names []string
				for _, p := range ps {
					names = append(names, p.Name)
				}
				pnames[bl.ID] = names
			}
		}
		tasks, err := b.ListTasks(itemID)
		if err != nil {
			return errMsg{err}
		}
		return itemDetailMsg{detail: detail, blockers: blockers, projectNames: pnames, tasks: tasks}
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

func createTaskCmd(b backend.Backend, itemID, title string) tea.Cmd {
	return func() tea.Msg {
		_, err := b.CreateTask(itemID, model.CreateProjectItemTask{Title: title})
		if err != nil {
			return errMsg{err}
		}
		return taskCreatedMsg{}
	}
}

func toggleTaskCmd(b backend.Backend, itemID string, task model.ProjectItemTask) tea.Cmd {
	return func() tea.Msg {
		toggled := !task.Completed
		_, err := b.UpdateTask(itemID, task.ID, model.UpdateProjectItemTask{Completed: &toggled})
		if err != nil {
			return errMsg{err}
		}
		return taskUpdatedMsg{}
	}
}

func deleteTaskCmd(b backend.Backend, itemID, taskID string) tea.Cmd {
	return func() tea.Msg {
		if err := b.DeleteTask(itemID, taskID); err != nil {
			return errMsg{err}
		}
		return taskDeletedMsg{}
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

func fetchDepCandidatesCmd(b backend.Backend, projects []model.ProjectWithItemCount) tea.Cmd {
	return func() tea.Msg {
		var groups []depGroup
		for _, p := range projects {
			items, err := b.ListItemsByProject(p.ID)
			if err != nil {
				return errMsg{err}
			}
			var plain []model.ProjectItem
			for _, it := range items {
				plain = append(plain, it.ProjectItem)
			}
			if len(plain) > 0 {
				groups = append(groups, depGroup{projectName: p.Name, items: plain})
			}
		}
		return depCandidatesMsg(groups)
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

// fetchItems returns the appropriate fetch command for the current view.
func (m *App) fetchItems() tea.Cmd {
	if len(m.selectedProjects) > 0 {
		var selected []model.ProjectWithItemCount
		for _, p := range m.projects {
			if m.selectedProjects[p.ID] {
				selected = append(selected, p)
			}
		}
		return fetchAllItemsCmd(m.backend, selected, m.filter)
	}
	if m.showingAll {
		return fetchAllItemsCmd(m.backend, m.projects, m.filter)
	}
	if len(m.projects) > 0 && m.projectCursor < len(m.projects) {
		return fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
	}
	return nil
}

// isGroupedView returns true when the item pane shows items from multiple projects.
func (m *App) isGroupedView() bool {
	return m.showingAll || len(m.selectedProjects) > 0
}

// toggleProjectSelection toggles the current project in/out of the multi-select set.
func (m *App) toggleProjectSelection() (tea.Model, tea.Cmd) {
	if len(m.projects) == 0 {
		return m, nil
	}

	if m.selectedProjects == nil {
		m.selectedProjects = make(map[string]bool)
	}

	if m.showingAll {
		// Space on "All": toggle all projects
		if len(m.selectedProjects) == len(m.projects) {
			// All selected → deselect all
			m.selectedProjects = nil
			m.itemCursor = 0
			return m, fetchAllItemsCmd(m.backend, m.projects, m.filter)
		}
		// Select all
		for _, p := range m.projects {
			m.selectedProjects[p.ID] = true
		}
		m.itemCursor = 0
		cmd := m.fetchItems()
		return m, cmd
	}

	// Toggle individual project
	p := m.projects[m.projectCursor]
	if m.selectedProjects[p.ID] {
		delete(m.selectedProjects, p.ID)
		if len(m.selectedProjects) == 0 {
			m.selectedProjects = nil
		}
	} else {
		m.selectedProjects[p.ID] = true
	}

	m.itemCursor = 0
	cmd := m.fetchItems()
	return m, cmd
}

// groupHeaderAt returns the project name if the item at idx starts a new group in All view.
func (m *App) groupHeaderAt(idx int) string {
	for _, g := range m.allGroups {
		if g.startIndex == idx {
			return g.projectName
		}
	}
	return ""
}

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
			cmd := m.fetchItems()
			return m, cmd
		}
		m.items = nil
		return m, nil

	case allItemsMsg:
		m.allGroups = msg.groups
		m.items = msg.items
		m.blockedSet = msg.blocked
		m.itemBlockers = msg.blockers
		m.itemProjectNames = msg.projectNames
		m.hasIncompleteTasks = msg.hasIncompleteTasks
		m.taskCounts = msg.taskCounts
		m.itemTasks = msg.itemTasks
		if m.itemCursor >= len(m.items) {
			m.itemCursor = max(0, len(m.items)-1)
		}
		return m, nil

	case itemsMsg:
		m.items = msg.items
		m.blockedSet = msg.blocked
		m.itemBlockers = msg.blockers
		m.itemProjectNames = msg.projectNames
		m.hasIncompleteTasks = msg.hasIncompleteTasks
		m.taskCounts = msg.taskCounts
		m.itemTasks = msg.itemTasks
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

	case taskCreatedMsg, taskUpdatedMsg, taskDeletedMsg:
		if m.itemDetail != nil {
			return m, fetchItemDetailCmd(m.backend, m.itemDetail.ID, m.blockedSet[m.itemDetail.ID])
		}
		// Refresh item list to update task counts and inline tasks
		if cmd := m.fetchItems(); cmd != nil {
			return m, cmd
		}
		return m, nil

	case itemCreatedMsg:
		flashCmd := m.flash("Item created")
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

	case itemUpdatedMsg:
		var flashCmd tea.Cmd
		if m.statusMsg == "" {
			flashCmd = m.flash("Item updated")
		}
		if m.appMode == modeItemDetail && m.itemDetail != nil {
			return m, tea.Batch(
				fetchProjectsCmd(m.backend),
				fetchItemDetailCmd(m.backend, m.itemDetail.ID, m.blockedSet[m.itemDetail.ID]),
				flashCmd,
			)
		}
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

	case projectCreatedMsg:
		flashCmd := m.flash("Project created")
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

	case undoResultMsg:
		flashCmd := m.flash(fmt.Sprintf("Undo: %s", string(msg)))
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

	case itemDetailMsg:
		m.itemDetail = msg.detail
		m.detailBlockers = msg.blockers
		m.detailBlockerProjects = msg.projectNames
		m.detailTasks = msg.tasks
		m.detailTaskCursor = 0
		m.detailTaskFocused = false
		m.addingTask = false
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
		flashCmd := m.flash("Project membership updated")
		if m.returnMode == modeItemDetail && m.itemDetail != nil {
			m.appMode = modeItemDetail
			return m, tea.Batch(
				fetchProjectsCmd(m.backend),
				fetchItemDetailCmd(m.backend, m.itemDetail.ID, m.blockedSet[m.itemDetail.ID]),
				flashCmd,
			)
		}
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

	case searchResultsMsg:
		m.searchResults = msg
		m.searchCursor = 0
		m.searchFocused = false
		return m, nil

	case searchNavigateMsg:
		m.appMode = modeNormal
		m.searchResults = nil
		m.filter = filterNone
		m.showingAll = false
		m.allGroups = nil
		m.selectedProjects = nil
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
		flashCmd := m.flash("Item reordered")
		if cmd := m.fetchItems(); cmd != nil {
			return m, tea.Batch(cmd, flashCmd)
		}
		return m, flashCmd

	case depCandidatesMsg:
		// Filter out the item itself from each group
		var groups []depGroup
		for _, g := range msg {
			var items []model.ProjectItem
			for _, item := range g.items {
				if item.ID != m.depItemID {
					items = append(items, item)
				}
			}
			if len(items) > 0 {
				groups = append(groups, depGroup{projectName: g.projectName, items: items})
			}
		}
		m.depAllItems = groups
		m.depFilter.SetValue("")
		m.depFilter.Blur()
		m.depFiltering = false
		m.depItems = flattenDepGroups(groups)
		m.depCursor = 0
		m.appMode = modeDepLink
		return m, nil

	case depBlockersForUnlinkMsg:
		if len(msg) == 0 {
			cmd := m.flash("No dependencies to unlink")
			return m, cmd
		}
		m.depItems = msg
		m.depCursor = 0
		m.appMode = modeDepUnlink
		return m, nil

	case depLinkedMsg:
		flashCmd := m.flash("Dependency linked")
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

	case depUnlinkedMsg:
		flashCmd := m.flash("Dependency unlinked")
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case errMsg:
		m.loading = false
		m.errorMsg = msg.Error()
		return m, nil

	case syncPullDoneMsg:
		flashCmd := m.flash("Synced with server")
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

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
	// Adding a task in item pane
	if m.itemAddingTask {
		switch msg.String() {
		case "enter":
			title := m.titleInput.Value()
			m.itemAddingTask = false
			m.titleInput.Blur()
			if title == "" {
				return m, nil
			}
			item := m.items[m.itemCursor]
			return m, createTaskCmd(m.backend, item.ID, title)
		case "esc":
			m.itemAddingTask = false
			m.titleInput.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.titleInput, cmd = m.titleInput.Update(msg)
			return m, cmd
		}
	}

	// Task list focused in item pane
	if m.itemTaskFocused {
		item := m.items[m.itemCursor]
		tasks := m.itemTasks[item.ID]
		switch msg.String() {
		case "j", "down":
			if m.itemTaskCursor < len(tasks)-1 {
				m.itemTaskCursor++
			}
			return m, nil
		case "k", "up":
			if m.itemTaskCursor > 0 {
				m.itemTaskCursor--
			}
			return m, nil
		case " ":
			if len(tasks) > 0 {
				return m, toggleTaskCmd(m.backend, item.ID, tasks[m.itemTaskCursor])
			}
			return m, nil
		case "d":
			if len(tasks) > 0 {
				task := tasks[m.itemTaskCursor]
				if m.itemTaskCursor >= len(tasks)-1 && m.itemTaskCursor > 0 {
					m.itemTaskCursor--
				}
				return m, deleteTaskCmd(m.backend, item.ID, task.ID)
			}
			return m, nil
		case "t":
			m.itemAddingTask = true
			m.titleInput.SetValue("")
			m.titleInput.Placeholder = "New task..."
			return m, m.titleInput.Focus()
		case "esc", "tab":
			m.itemTaskFocused = false
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case "q":
		return m, tea.Quit

	case "esc":
		if len(m.selectedProjects) > 0 {
			m.selectedProjects = nil
			m.itemCursor = 0
			cmd := m.fetchItems()
			return m, cmd
		}
		return m, nil

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
			hasSelections := len(m.selectedProjects) > 0
			if !m.showingAll {
				m.showingAll = true
				if !hasSelections {
					m.itemCursor = 0
					return m, fetchAllItemsCmd(m.backend, m.projects, m.filter)
				}
			}
		} else {
			m.itemCursor = 0
		}
		return m, nil

	case "G":
		if m.activePane == projectPane {
			hasSelections := len(m.selectedProjects) > 0
			last := len(m.projects) - 1
			if last >= 0 {
				if m.showingAll {
					m.showingAll = false
					if !hasSelections {
						m.allGroups = nil
					}
				}
				m.projectCursor = last
				if !hasSelections {
					m.itemCursor = 0
					return m, fetchItemsCmd(m.backend, m.projects[last].ID, m.filter)
				}
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
			hasSelections := len(m.selectedProjects) > 0
			if m.showingAll {
				m.showingAll = false
				if !hasSelections {
					m.allGroups = nil
				}
				m.projectCursor = min(half-1, max(0, len(m.projects)-1))
			} else {
				m.projectCursor = min(m.projectCursor+half, max(0, len(m.projects)-1))
			}
			if !hasSelections {
				m.itemCursor = 0
				if len(m.projects) > 0 {
					return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
				}
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
			hasSelections := len(m.selectedProjects) > 0
			if m.showingAll {
				return m, nil // already at top
			}
			newCursor := m.projectCursor - half
			if newCursor < 0 {
				m.showingAll = true
				if !hasSelections {
					m.itemCursor = 0
					return m, fetchAllItemsCmd(m.backend, m.projects, m.filter)
				}
				return m, nil
			}
			m.projectCursor = newCursor
			if !hasSelections {
				m.itemCursor = 0
				if len(m.projects) > 0 {
					return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
				}
			}
		} else {
			m.itemCursor = max(m.itemCursor-half, 0)
		}
		return m, nil

	case "enter":
		if m.activePane == projectPane {
			if m.showingAll || len(m.projects) > 0 {
				m.activePane = itemPane
				cmd := m.fetchItems()
				return m, cmd
			}
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
		if m.isGroupedView() {
			cmd := m.flash("Use A to add items in multi-project view")
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
		if m.activePane == projectPane {
			return m.toggleProjectSelection()
		}
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			if !item.Completed && m.hasIncompleteTasks[item.ID] {
				cmd := m.flash("Cannot complete: item has incomplete tasks")
				return m, cmd
			}
			if m.blockedSet[item.ID] && !item.Completed {
				cmd := m.flash("Cannot complete: item has unresolved blockers")
				return m, cmd
			}
			toggled := !item.Completed
			var flashCmd tea.Cmd
			if toggled {
				flashCmd = m.flash("Marked done")
			} else {
				flashCmd = m.flash("Marked incomplete")
			}
			return m, tea.Batch(updateItemCmd(m.backend, item.ID, model.UpdateProjectItem{Completed: &toggled}), flashCmd)
		}
		return m, nil

	case "x":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			archived := true
			flashCmd := m.flash("Archived")
			return m, tea.Batch(updateItemCmd(m.backend, item.ID, model.UpdateProjectItem{Archived: &archived}), flashCmd)
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

	case "t":
		if m.activePane == itemPane && len(m.items) > 0 {
			item := m.items[m.itemCursor]
			if tasks, ok := m.itemTasks[item.ID]; ok && len(tasks) > 0 {
				m.itemTaskFocused = true
				m.itemTaskCursor = 0
				return m, nil
			}
			// No tasks yet — go straight to adding one
			m.itemAddingTask = true
			m.titleInput.SetValue("")
			m.titleInput.Placeholder = "New task..."
			return m, m.titleInput.Focus()
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
		if cmd := m.fetchItems(); cmd != nil {
			return m, cmd
		}
		return m, nil

	case "2":
		if m.filter == filterAll {
			m.filter = filterNone
		} else {
			m.filter = filterAll
		}
		if cmd := m.fetchItems(); cmd != nil {
			return m, cmd
		}
		return m, nil

	case "0":
		m.filter = filterNone
		if cmd := m.fetchItems(); cmd != nil {
			return m, cmd
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
		if m.isGroupedView() {
			cmd := m.flash("Reorder not available in multi-project view")
			return m, cmd
		}
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
			return m, fetchDepCandidatesCmd(m.backend, m.projects)
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
			currentProjectID := ""
			if !m.isGroupedView() && m.projectCursor < len(m.projects) {
				currentProjectID = m.projects[m.projectCursor].ID
			}
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
	if m.itemDetail == nil {
		return m, nil
	}

	// Adding a new task — route to text input
	if m.addingTask {
		switch msg.String() {
		case "enter":
			title := m.titleInput.Value()
			m.addingTask = false
			m.titleInput.Blur()
			if title == "" {
				return m, nil
			}
			return m, createTaskCmd(m.backend, m.itemDetail.ID, title)
		case "esc":
			m.addingTask = false
			m.titleInput.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.titleInput, cmd = m.titleInput.Update(msg)
			return m, cmd
		}
	}

	// Task list focused — navigate and act on tasks
	if m.detailTaskFocused && len(m.detailTasks) > 0 {
		switch msg.String() {
		case "j", "down":
			if m.detailTaskCursor < len(m.detailTasks)-1 {
				m.detailTaskCursor++
			}
			return m, nil
		case "k", "up":
			if m.detailTaskCursor > 0 {
				m.detailTaskCursor--
			}
			return m, nil
		case " ":
			task := m.detailTasks[m.detailTaskCursor]
			return m, toggleTaskCmd(m.backend, m.itemDetail.ID, task)
		case "d":
			task := m.detailTasks[m.detailTaskCursor]
			if m.detailTaskCursor >= len(m.detailTasks)-1 && m.detailTaskCursor > 0 {
				m.detailTaskCursor--
			}
			return m, deleteTaskCmd(m.backend, m.itemDetail.ID, task.ID)
		case "t":
			m.addingTask = true
			m.titleInput.SetValue("")
			m.titleInput.Placeholder = "New task..."
			return m, m.titleInput.Focus()
		case "esc", "tab":
			m.detailTaskFocused = false
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case "esc", "q":
		m.appMode = modeNormal
		m.itemDetail = nil
		m.detailBlockers = nil
		m.detailTasks = nil
		return m, nil

	case " ":
		// Check for incomplete tasks
		for _, task := range m.detailTasks {
			if !task.Completed {
				cmd := m.flash("Cannot complete: item has incomplete tasks")
				return m, cmd
			}
		}
		if m.blockedSet[m.itemDetail.ID] && !m.itemDetail.Completed {
			cmd := m.flash("Cannot complete: item has unresolved blockers")
			return m, cmd
		}
		toggled := !m.itemDetail.Completed
		var flashCmd tea.Cmd
		if toggled {
			flashCmd = m.flash("Marked done")
		} else {
			flashCmd = m.flash("Marked incomplete")
		}
		return m, tea.Batch(updateItemCmd(m.backend, m.itemDetail.ID, model.UpdateProjectItem{Completed: &toggled}), flashCmd)

	case "x":
		archived := true
		id := m.itemDetail.ID
		flashCmd := m.flash("Archived")
		m.appMode = modeNormal
		m.itemDetail = nil
		return m, tea.Batch(updateItemCmd(m.backend, id, model.UpdateProjectItem{Archived: &archived}), flashCmd)

	case "e":
		m.titleInput.SetValue(m.itemDetail.Title)
		m.titleInput.Placeholder = ""
		cmd := m.titleInput.Focus()
		m.returnMode = modeItemDetail
		m.appMode = modeEditTitle
		return m, cmd

	case "n":
		notes := ""
		if m.itemDetail.Notes != nil {
			notes = *m.itemDetail.Notes
		}
		m.notesInput.SetValue(notes)
		cmd := m.notesInput.Focus()
		m.returnMode = modeItemDetail
		m.appMode = modeEditNotes
		return m, cmd

	case "p":
		m.returnMode = modeItemDetail
		return m, fetchItemProjectsCmd(m.backend, m.itemDetail.ID)

	case "b":
		m.depItemID = m.itemDetail.ID
		m.depItemName = m.itemDetail.Title
		return m, fetchDepCandidatesCmd(m.backend, m.projects)

	case "B":
		m.depItemID = m.itemDetail.ID
		m.depItemName = m.itemDetail.Title
		return m, fetchDepBlockersCmd(m.backend, m.itemDetail.ID)

	case "t":
		if len(m.detailTasks) > 0 {
			m.detailTaskFocused = true
			m.detailTaskCursor = 0
			return m, nil
		}
		// No tasks yet — go straight to adding one
		m.addingTask = true
		m.titleInput.SetValue("")
		m.titleInput.Placeholder = "New task..."
		return m, m.titleInput.Focus()

	case "tab":
		if len(m.detailTasks) > 0 {
			m.detailTaskFocused = true
			m.detailTaskCursor = 0
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
		if cmd := m.fetchItems(); cmd != nil {
			return m, cmd
		}
		return m, nil
	}

	return m, nil
}

func (m *App) handleDepLinkKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// When filtering, route most keys to the text input
	if m.depFiltering {
		switch msg.String() {
		case "esc":
			if m.depFilter.Value() != "" {
				m.depFilter.SetValue("")
				m.depFilter.Blur()
				m.depFiltering = false
				m.depItems = flattenDepGroups(m.depAllItems)
				m.depCursor = 0
			} else {
				m.depFilter.Blur()
				m.depFiltering = false
			}
			return m, nil
		case "enter":
			m.depFilter.Blur()
			m.depFiltering = false
			return m, nil
		default:
			var cmd tea.Cmd
			m.depFilter, cmd = m.depFilter.Update(msg)
			query := m.depFilter.Value()
			if query == "" {
				m.depItems = flattenDepGroups(m.depAllItems)
			} else {
				m.depItems = flattenDepGroups(filterDepGroups(m.depAllItems, query))
			}
			m.depCursor = 0
			return m, cmd
		}
	}

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

	case "/":
		m.depFiltering = true
		return m, m.depFilter.Focus()

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
		hasSelections := len(m.selectedProjects) > 0
		if m.showingAll {
			// Move from All to first real project
			if len(m.projects) > 0 {
				m.showingAll = false
				m.projectCursor = 0
				if !hasSelections {
					m.allGroups = nil
					m.itemCursor = 0
					return fetchItemsCmd(m.backend, m.projects[0].ID, m.filter)
				}
			}
		} else if m.projectCursor < len(m.projects)-1 {
			m.projectCursor++
			if !hasSelections {
				m.itemCursor = 0
				return fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
			}
		}
	} else if m.itemCursor < len(m.items)-1 {
		m.itemCursor++
	}
	return nil
}

func (m *App) cursorUp() tea.Cmd {
	if m.activePane == projectPane {
		hasSelections := len(m.selectedProjects) > 0
		if m.showingAll {
			return nil // already at top
		}
		if m.projectCursor > 0 {
			m.projectCursor--
			if !hasSelections {
				m.itemCursor = 0
				return fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
			}
			return nil
		}
		// At first project, move up to All
		m.showingAll = true
		if !hasSelections {
			m.itemCursor = 0
			return fetchAllItemsCmd(m.backend, m.projects, m.filter)
		}
	} else if m.itemCursor > 0 {
		m.itemCursor--
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

	projectPaneWidth := m.projectPaneWidth()
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
		// Determine which projects this item belongs to
		detailProjects := make(map[string]bool)
		for _, p := range d.Projects {
			detailProjects[p.Name] = true
		}
		for _, b := range m.detailBlockers {
			prefix := ""
			if names, ok := m.detailBlockerProjects[b.ID]; ok {
				inSame := false
				for _, n := range names {
					if detailProjects[n] {
						inSame = true
						break
					}
				}
				if !inSame && len(names) > 0 {
					prefix = blockerProjectStyle.Render(names[0] + ": ")
				}
			}
			lines = append(lines, blockerStyle.Render(
				fmt.Sprintf("○ %s%s (%s)", prefix, b.Title, shortID(b.ID)),
			))
		}
	}

	// Tasks section
	if len(m.detailTasks) > 0 || m.addingTask {
		done := 0
		for _, t := range m.detailTasks {
			if t.Completed {
				done++
			}
		}
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  ─── Tasks (%d/%d) ──────────────────", done, len(m.detailTasks))))
		for i, t := range m.detailTasks {
			check := "○"
			title := t.Title
			if t.Completed {
				check = "✓"
				title = taskCompletedStyle.Render(title)
			}
			line := fmt.Sprintf("%s %s", check, title)
			if m.detailTaskFocused && i == m.detailTaskCursor {
				lines = append(lines, taskSelectedStyle.Render("> "+line))
			} else {
				lines = append(lines, taskNormalStyle.Render(line))
			}
		}
		if m.addingTask {
			lines = append(lines, "    "+m.titleInput.View())
		}
	}

	lines = append(lines, "")
	var hints string
	switch {
	case m.detailTaskFocused:
		hints = "  [space]toggle  [t]add task  [d]elete  [Tab/Esc]back"
	case m.addingTask:
		hints = "  [Enter]create  [Esc]cancel"
	default:
		hints = "  [e]dit  [n]otes  [p]rojects  [t]asks  [b]lock  [B]unblock  [space]done  [x]archive  [Esc]close"
	}
	lines = append(lines, dimStyle.Render(hints))

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

	boxWidth := m.width * 3 / 4
	if boxWidth < 40 {
		boxWidth = 40
	}
	boxHeight := m.height * 2 / 3
	if boxHeight < 10 {
		boxHeight = 10
	}

	// Size the textarea to fill the overlay (subtract border, padding, header/footer lines)
	m.notesInput.SetWidth(boxWidth - 6)
	m.notesInput.SetHeight(boxHeight - 7)

	var lines []string
	lines = append(lines, overlayTitleStyle.Render("Edit Notes"))
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  Item: %s", itemTitle)))
	lines = append(lines, "")
	lines = append(lines, m.notesInput.View())
	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("  [Ctrl+S] Save  [Esc] Cancel"))

	content := strings.Join(lines, "\n")

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
  a              Add project
  space          Toggle multi-select
  Esc            Clear selections`
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

	// Show filter input for link mode (not unlink, which has few items)
	if m.appMode == modeDepLink {
		lines = append(lines, "  "+m.depFilter.View())
		lines = append(lines, "")
	}

	if len(m.depItems) == 0 {
		if m.depFilter.Value() != "" {
			lines = append(lines, dimStyle.Render("  No matching items"))
		} else {
			lines = append(lines, dimStyle.Render("  No items available"))
		}
	}

	// Build a set of visible items to figure out which group each belongs to
	// Render grouped by project using depAllItems structure, but only show filtered items
	visibleSet := make(map[string]bool, len(m.depItems))
	for _, item := range m.depItems {
		visibleSet[item.ID] = true
	}

	groups := m.depAllItems
	if m.appMode == modeDepUnlink {
		// Unlink mode doesn't use groups — show flat list
		groups = nil
	}

	flatIdx := 0
	maxVisible := m.height - 12 // leave room for header, filter, hints
	if maxVisible < 5 {
		maxVisible = 5
	}

	// Scroll window around cursor
	scrollStart := 0
	if m.depCursor > maxVisible-3 {
		scrollStart = m.depCursor - maxVisible + 3
	}

	if len(groups) > 0 {
		for _, g := range groups {
			var groupItems []model.ProjectItem
			for _, item := range g.items {
				if visibleSet[item.ID] {
					groupItems = append(groupItems, item)
				}
			}
			if len(groupItems) == 0 {
				continue
			}

			lines = append(lines, dimStyle.Render("  ── "+g.projectName+" ──"))

			for _, item := range groupItems {
				if flatIdx >= scrollStart && flatIdx < scrollStart+maxVisible {
					status := "○"
					if item.Completed {
						status = "✓"
					}
					line := fmt.Sprintf("%s %s  %s", status, item.Title, itemIDStyle.Render(shortID(item.ID)))
					if flatIdx == m.depCursor {
						line = pickerSelectedStyle.Render("> " + line)
					} else {
						line = pickerNormalStyle.Render("    " + line)
					}
					lines = append(lines, line)
				}
				flatIdx++
			}
		}
	} else {
		// Flat list for unlink mode
		for i, item := range m.depItems {
			if i >= scrollStart && i < scrollStart+maxVisible {
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
		}
	}

	lines = append(lines, "")
	if m.appMode == modeDepUnlink {
		lines = append(lines, dimStyle.Render("  [Enter] Unlink  [Esc] Cancel"))
	} else {
		lines = append(lines, dimStyle.Render("  [Enter] Link  [/] Filter  [Esc] Cancel"))
	}

	content := strings.Join(lines, "\n")

	boxWidth := m.width - 4
	if boxWidth > 80 {
		boxWidth = 80
	}
	if boxWidth < 40 {
		boxWidth = 40
	}

	return overlayBoxStyle.Width(boxWidth).Render(content)
}

func (m *App) projectPaneWidth() int {
	const minWidth = 20
	// Account for "All" entry
	totalItems := 0
	for _, p := range m.projects {
		totalItems += p.ItemCount
	}
	maxName := lipgloss.Width(fmt.Sprintf("> All (%d)", totalItems))
	for _, p := range m.projects {
		// "> name (count)" — 2 prefix + space + parens + digits
		w := lipgloss.Width(fmt.Sprintf("> %s (%d)", p.Name, p.ItemCount))
		if w > maxName {
			maxName = w
		}
	}
	// add padding for border
	width := maxName + 4
	if width < minWidth {
		width = minWidth
	}
	// cap at 40% of terminal to protect the item pane
	maxWidth := m.width * 2 / 5
	if width > maxWidth {
		width = maxWidth
	}
	return width
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

	// Build a virtual list: All entry at index 0, real projects at 1..N
	totalEntries := len(m.projects) + 1
	virtualCursor := m.projectCursor + 1 // offset for All entry
	if m.showingAll {
		virtualCursor = 0
	}
	m.projectScroll = syncScroll(virtualCursor, m.projectScroll, viewHeight)

	end := m.projectScroll + viewHeight
	if end > totalEntries {
		end = totalEntries
	}

	hasSelections := len(m.selectedProjects) > 0
	for vi := m.projectScroll; vi < end; vi++ {
		if vi == 0 {
			// "All" entry
			totalItems := 0
			for _, p := range m.projects {
				totalItems += p.ItemCount
			}
			isCursor := m.showingAll
			allSelected := hasSelections && len(m.selectedProjects) == len(m.projects)
			var prefix string
			if isCursor {
				prefix = "> "
			} else {
				prefix = "  "
			}
			label := "All"
			if allSelected {
				label = "● All"
			}
			line := fmt.Sprintf("%s%s (%d)", prefix, label, totalItems)
			if len(line) > width {
				line = line[:width]
			}
			switch {
			case isCursor:
				line = allProjectStyle.Render(line)
			case allSelected:
				line = projectMultiSelectedStyle.Render(line)
			default:
				line = projectNormalStyle.Render(line)
			}
			lines = append(lines, line)
		} else {
			// Real project at index vi-1
			pi := vi - 1
			p := m.projects[pi]
			isCursor := !m.showingAll && pi == m.projectCursor
			isSelected := m.selectedProjects[p.ID]
			var prefix string
			if isCursor {
				prefix = "> "
			} else {
				prefix = "  "
			}
			name := p.Name
			if isSelected {
				name = "● " + name
			}
			line := fmt.Sprintf("%s%s (%d)", prefix, name, p.ItemCount)
			if len(line) > width {
				line = line[:width]
			}
			switch {
			case isCursor:
				line = projectSelectedStyle.Render(line)
			case isSelected:
				line = projectMultiSelectedStyle.Render(line)
			default:
				line = projectNormalStyle.Render(line)
			}
			lines = append(lines, line)
		}
	}

	if totalEntries > viewHeight {
		pos := virtualCursor + 1
		scrollInfo := dimStyle.Render(fmt.Sprintf(" %d/%d", pos, totalEntries))
		lines = append(lines, scrollInfo)
	}

	return strings.Join(lines, "\n")
}

func (m *App) renderItemPane(width, height int) string {
	var titleText string
	switch {
	case len(m.selectedProjects) > 0:
		titleText = fmt.Sprintf("Selected (%d)", len(m.selectedProjects))
	case m.showingAll:
		titleText = "All Items"
	case len(m.projects) > 0 && m.projectCursor < len(m.projects):
		titleText = m.projects[m.projectCursor].Name
	default:
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
		if linesUsed >= viewHeight {
			break
		}

		// In grouped views, insert project group headers with spacing between groups
		if m.isGroupedView() {
			if groupName := m.groupHeaderAt(i); groupName != "" {
				if i > 0 && linesUsed < viewHeight {
					lines = append(lines, "")
					linesUsed++
				}
				header := groupHeaderStyle.Render(fmt.Sprintf("── %s ──", groupName))
				lines = append(lines, header)
				linesUsed++
				if linesUsed >= viewHeight {
					break
				}
			}
		}

		item := m.items[i]
		isMoving := m.appMode == modeMove && i == m.itemCursor
		line := m.renderItemLine(item, i == m.itemCursor, width, isMoving)
		lines = append(lines, line)
		linesUsed++

		if tasks, ok := m.itemTasks[item.ID]; ok && len(tasks) > 0 && linesUsed < viewHeight {
			for ti, t := range tasks {
				if linesUsed >= viewHeight {
					break
				}
				check := "○"
				title := t.Title
				if t.Completed {
					check = "✓"
					title = taskCompletedStyle.Render(title)
				}
				line := fmt.Sprintf("%s %s", check, title)
				if m.itemTaskFocused && i == m.itemCursor && ti == m.itemTaskCursor {
					lines = append(lines, taskSelectedStyle.Render("> "+line))
				} else {
					lines = append(lines, taskNormalStyle.Render(line))
				}
				linesUsed++
			}
			if m.itemAddingTask && i == m.itemCursor && linesUsed < viewHeight {
				lines = append(lines, "     "+m.titleInput.View())
				linesUsed++
			}
		}

		if blockers, ok := m.itemBlockers[item.ID]; ok && len(blockers) > 0 {
			currentProject := ""
			if !m.isGroupedView() && m.projectCursor < len(m.projects) {
				currentProject = m.projects[m.projectCursor].Name
			}
			for _, b := range blockers {
				if linesUsed >= viewHeight {
					break
				}
				// Show "Project: Title" if blocker is NOT in the current project
				prefix := ""
				if names, ok := m.itemProjectNames[b.ID]; ok {
					inCurrent := false
					for _, n := range names {
						if n == currentProject {
							inCurrent = true
							break
						}
					}
					if !inCurrent && len(names) > 0 {
						prefix = blockerProjectStyle.Render(names[0] + ": ")
					}
				}
				blockerLine := blockerStyle.Render(
					fmt.Sprintf("└─ blocked by: %s%s (%s)", prefix, b.Title, shortID(b.ID)),
				)
				lines = append(lines, blockerLine)
				linesUsed++
			}
		}

		if item.Notes != nil && *item.Notes != "" && linesUsed < viewHeight {
			prefix := "     " + notesConnectorStyle.Render("└─ notes ▸ ")
			wrapWidth := width - 16
			if wrapWidth < 10 {
				wrapWidth = 10
			}
			noteLines := strings.Split(*item.Notes, "\n")
			first := true
			for _, noteLine := range noteLines {
				if linesUsed >= viewHeight {
					break
				}
				wrapped := wrapLine(noteLine, wrapWidth)
				for _, wl := range wrapped {
					if linesUsed >= viewHeight {
						break
					}
					if first {
						lines = append(lines, prefix+wl)
						first = false
					} else {
						lines = append(lines, notesPreviewStyle.Render(wl))
					}
					linesUsed++
				}
			}
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

	taskIndicator := ""
	if tc, ok := m.taskCounts[item.ID]; ok {
		taskIndicator = fmt.Sprintf(" [%d/%d]", tc[0], tc[1])
	}

	idText := shortID(item.ID)

	var content string
	if item.Completed {
		content = itemCompletedStyle.Render(
			fmt.Sprintf("%s %s%s%s%s  %s", status, item.Title, multiProject, hasNotes, taskIndicator, idText),
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
		tasks := ""
		if taskIndicator != "" {
			tasks = dimStyle.Render(taskIndicator)
		}
		content = fmt.Sprintf("%s %s%s%s%s  %s", status, item.Title, mp, notes, tasks, id)
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
	case m.itemAddingTask:
		return "[Enter]create task  [Esc]cancel"
	case m.itemTaskFocused:
		return "[space]toggle  [t]add task  [d]elete  [Tab/Esc]back"
	default:
		hints := "[Enter]detail [space]done [a]dd [x]archive [e]dit [n]otes [t]asks [b]lock [B]unblock [/]search [?]help"
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
	if m.syncEngine != nil {
		target := dimStyle.Render(" " + m.syncEngine.APIURL())
		s := m.syncStatus
		switch {
		case s.Syncing:
			modeStr = syncingStyle.Render("SYNCING...") + target
		case s.LastError != "":
			modeStr = syncErrorStyle.Render("SYNC ERR") + target
		case s.PendingCount > 0:
			modeStr = syncPendingStyle.Render(fmt.Sprintf("PENDING (%d)", s.PendingCount)) + target
		default:
			modeStr = syncOKStyle.Render("SYNCED") + target
		}
	} else {
		modeStr = modeLocalStyle.Render("LOCAL") + dimStyle.Render(" "+m.dbPath)
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
