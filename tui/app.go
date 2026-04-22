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

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/model"
	"github.com/datapointchris/todoui/sync"
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

	// Unified item-pane cursor. The pane shows items and their sub-tasks
	// as one scrollable list; m.rows is the navigable view of that list
	// and m.rowCursor indexes into it. Task rows carry their owning
	// item's index, so item-scoped actions still have a clear target.
	rows           []paneRow
	itemAddingTask bool

	activePane    pane
	projectCursor int
	rowCursor     int
	projectScroll int // viewport scroll offset for projects
	rowScroll     int // viewport scroll offset for item pane rows

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
	// detailTaskCursor is the unified detail-view cursor:
	//   -1 = cursor is on the item (its fields/meta header)
	//    0..N-1 = cursor is on that task
	detailTaskCursor int
	addingTask       bool // true when typing a new task title

	// Project detail view
	projectDetail *model.ProjectWithItemCount

	// Filters
	filter filterMode

	// Search
	searchResults []model.ProjectItem
	searchCursor  int
	searchFocused bool // true = typing in input, false = browsing results

	// Move/reorder — the item-space index of the item being moved.
	// Maintained independently of rowCursor since move mode only
	// operates on items.
	moveItemPos int

	// Project move — index into m.projects of the project being moved.
	moveProjectPos int

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
	projectUpdatedMsg    struct{}
	undoResultMsg        string
	itemProjectsMsg      []model.Project
	membershipUpdatedMsg struct{}
	itemReorderedMsg     struct{}
	projectReorderedMsg  struct{}
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

func updateProjectCmd(b backend.Backend, id string, input model.UpdateProject) tea.Cmd {
	return func() tea.Msg {
		_, err := b.UpdateProject(id, input)
		if err != nil {
			return errMsg{err}
		}
		return projectUpdatedMsg{}
	}
}

func fetchProjectDetailCmd(b backend.Backend, projectID string) tea.Cmd {
	return func() tea.Msg {
		p, err := b.GetProject(projectID)
		if err != nil {
			return errMsg{err}
		}
		return projectDetailMsg{project: p}
	}
}

type projectDetailMsg struct {
	project *model.ProjectWithItemCount
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
		return itemReorderedMsg{}
	}
}

func reorderProjectCmd(b backend.Backend, projectID string, newPos int) tea.Cmd {
	return func() tea.Msg {
		if err := b.ReorderProject(projectID, newPos); err != nil {
			return errMsg{err}
		}
		return projectReorderedMsg{}
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
			m.rowCursor = 0
			return m, fetchAllItemsCmd(m.backend, m.projects, m.filter)
		}
		// Select all
		for _, p := range m.projects {
			m.selectedProjects[p.ID] = true
		}
		m.rowCursor = 0
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

	m.rowCursor = 0
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
		m.rows = nil
		m.rowCursor = 0
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
		m.rebuildRows()
		return m, nil

	case itemsMsg:
		m.items = msg.items
		m.blockedSet = msg.blocked
		m.itemBlockers = msg.blockers
		m.itemProjectNames = msg.projectNames
		m.hasIncompleteTasks = msg.hasIncompleteTasks
		m.taskCounts = msg.taskCounts
		m.itemTasks = msg.itemTasks
		m.rebuildRows()
		if m.pendingItemID != "" {
			for i, item := range m.items {
				if item.ID == m.pendingItemID {
					m.rowCursor = m.firstRowForItem(i)
					break
				}
			}
			m.pendingItemID = ""
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

	case projectUpdatedMsg:
		flashCmd := m.flash("Project updated")
		if m.appMode == modeProjectDetail && m.projectDetail != nil {
			return m, tea.Batch(
				fetchProjectsCmd(m.backend),
				fetchProjectDetailCmd(m.backend, m.projectDetail.ID),
				flashCmd,
			)
		}
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

	case projectDetailMsg:
		m.projectDetail = msg.project
		m.appMode = modeProjectDetail
		return m, nil

	case undoResultMsg:
		flashCmd := m.flash(fmt.Sprintf("Undo: %s", string(msg)))
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

	case itemDetailMsg:
		// Preserve the cursor when the overlay is already open (so
		// toggling/deleting a task doesn't snap focus back to the item
		// header). Only reset when entering the overlay fresh.
		opening := m.appMode != modeItemDetail
		m.itemDetail = msg.detail
		m.detailBlockers = msg.blockers
		m.detailBlockerProjects = msg.projectNames
		m.detailTasks = msg.tasks
		if opening {
			m.detailTaskCursor = -1 // start on the item; tasks are >= 0
			m.addingTask = false
		} else if m.detailTaskCursor >= len(m.detailTasks) {
			// Tasks shortened (e.g. delete) — clamp so we don't index
			// past the end. Falling back to the item header is safe.
			m.detailTaskCursor = len(m.detailTasks) - 1
			if m.detailTaskCursor < -1 {
				m.detailTaskCursor = -1
			}
		}
		m.appMode = modeItemDetail
		return m, nil

	case itemProjectsMsg:
		if item := m.currentItem(); item != nil {
			m.picker = newPickerForManage(m.projects, msg, *item)
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

	case itemReorderedMsg:
		flashCmd := m.flash("Item reordered")
		if cmd := m.fetchItems(); cmd != nil {
			return m, tea.Batch(cmd, flashCmd)
		}
		return m, flashCmd

	case projectReorderedMsg:
		flashCmd := m.flash("Project reordered")
		return m, tea.Batch(fetchProjectsCmd(m.backend), flashCmd)

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
	case modeEditNotes, modeEditProjectDesc:
		return m.handleNotesKey(msg)
	case modeProjectDetail:
		return m.handleProjectDetailKey(msg)
	case modeEditProjectName:
		return m.handleInputKey(msg)
	case modeHelp:
		return m.handleHelpKey(msg)
	case modeSearch:
		return m.handleSearchKey(msg)
	case modeMove:
		return m.handleMoveKey(msg)
	case modeMoveProject:
		return m.handleMoveProjectKey(msg)
	case modeDepLink, modeDepUnlink:
		return m.handleDepLinkKey(msg)
	}
	return m, nil
}

func (m *App) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Adding a task in item pane — target item is the owning item of the
	// row the user pressed `t` on (works whether on item or task row).
	if m.itemAddingTask {
		switch msg.String() {
		case "enter":
			title := m.titleInput.Value()
			m.itemAddingTask = false
			m.titleInput.Blur()
			if title == "" {
				return m, nil
			}
			item := m.currentItem()
			if item == nil {
				return m, nil
			}
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

	row := m.currentRow() // nil when item pane is empty

	switch msg.String() {
	case "q":
		return m, tea.Quit

	case "esc":
		if len(m.selectedProjects) > 0 {
			m.selectedProjects = nil
			m.rowCursor = 0
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

	case "J":
		if m.activePane == itemPane {
			m.rowCursor = m.nextItemRow(m.rowCursor, +1)
		}
		return m, nil

	case "K":
		if m.activePane == itemPane {
			m.rowCursor = m.nextItemRow(m.rowCursor, -1)
		}
		return m, nil

	case "g":
		if m.activePane == projectPane {
			hasSelections := len(m.selectedProjects) > 0
			if !m.showingAll {
				m.showingAll = true
				if !hasSelections {
					m.rowCursor = 0
					return m, fetchAllItemsCmd(m.backend, m.projects, m.filter)
				}
			}
		} else {
			m.rowCursor = 0
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
					m.rowCursor = 0
					return m, fetchItemsCmd(m.backend, m.projects[last].ID, m.filter)
				}
			}
		} else {
			if last := len(m.rows) - 1; last >= 0 {
				m.rowCursor = last
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
				m.rowCursor = 0
				if len(m.projects) > 0 {
					return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
				}
			}
		} else {
			m.rowCursor = min(m.rowCursor+half, max(0, len(m.rows)-1))
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
					m.rowCursor = 0
					return m, fetchAllItemsCmd(m.backend, m.projects, m.filter)
				}
				return m, nil
			}
			m.projectCursor = newCursor
			if !hasSelections {
				m.rowCursor = 0
				if len(m.projects) > 0 {
					return m, fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
				}
			}
		} else {
			m.rowCursor = max(m.rowCursor-half, 0)
		}
		return m, nil

	case "enter":
		if m.activePane == projectPane {
			if !m.showingAll && m.projectCursor < len(m.projects) {
				return m, fetchProjectDetailCmd(m.backend, m.projects[m.projectCursor].ID)
			}
		}
		if m.activePane == itemPane && row != nil && row.kind == rowItem {
			item := m.currentItem()
			return m, fetchItemDetailCmd(m.backend, item.ID, m.blockedSet[item.ID])
		}
		// Task row: enter is inert — users press K to jump to the item first.
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
		if m.activePane != itemPane || row == nil {
			return m, nil
		}
		if row.kind == rowTask {
			tasks := m.itemTasks[m.items[row.itemIdx].ID]
			return m, toggleTaskCmd(m.backend, m.items[row.itemIdx].ID, tasks[row.taskIdx])
		}
		item := m.currentItem()
		if !item.Completed && m.hasIncompleteTasks[item.ID] {
			flashCmd := m.flash("Cannot complete: item has incomplete tasks")
			return m, flashCmd
		}
		if m.blockedSet[item.ID] && !item.Completed {
			flashCmd := m.flash("Cannot complete: item has unresolved blockers")
			return m, flashCmd
		}
		toggled := !item.Completed
		var flashCmd tea.Cmd
		if toggled {
			flashCmd = m.flash("Marked done")
		} else {
			flashCmd = m.flash("Marked incomplete")
		}
		return m, tea.Batch(updateItemCmd(m.backend, item.ID, model.UpdateProjectItem{Completed: &toggled}), flashCmd)

	case "d":
		if m.activePane == itemPane && row != nil && row.kind == rowTask {
			tasks := m.itemTasks[m.items[row.itemIdx].ID]
			task := tasks[row.taskIdx]
			// If this was the last row, nudge the cursor up so we don't
			// end up past the end after the row list rebuilds.
			if m.rowCursor >= len(m.rows)-1 && m.rowCursor > 0 {
				m.rowCursor--
			}
			return m, deleteTaskCmd(m.backend, m.items[row.itemIdx].ID, task.ID)
		}
		return m, nil

	case "x":
		if m.activePane == itemPane && row != nil {
			if row.kind == rowTask {
				flashCmd := m.flash("Archive is only for items")
				return m, flashCmd
			}
			item := m.currentItem()
			archived := true
			flashCmd := m.flash("Archived")
			return m, tea.Batch(updateItemCmd(m.backend, item.ID, model.UpdateProjectItem{Archived: &archived}), flashCmd)
		}
		return m, nil

	case "e":
		if m.activePane == itemPane && row != nil {
			if row.kind == rowTask {
				flashCmd := m.flash("Edit is only for items")
				return m, flashCmd
			}
			item := m.currentItem()
			m.titleInput.SetValue(item.Title)
			m.titleInput.Placeholder = ""
			cmd := m.titleInput.Focus()
			m.returnMode = modeNormal
			m.appMode = modeEditTitle
			return m, cmd
		}
		return m, nil

	case "n":
		if m.activePane == itemPane && row != nil {
			if row.kind == rowTask {
				flashCmd := m.flash("Notes are only for items")
				return m, flashCmd
			}
			item := m.currentItem()
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
		if m.activePane == itemPane && row != nil {
			if row.kind == rowTask {
				flashCmd := m.flash("Project membership is only for items")
				return m, flashCmd
			}
			item := m.currentItem()
			m.returnMode = modeNormal
			return m, fetchItemProjectsCmd(m.backend, item.ID)
		}
		return m, nil

	case "t":
		if m.activePane == itemPane && row != nil {
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
		if m.activePane == projectPane {
			if m.showingAll {
				flashCmd := m.flash("Reorder is only for real projects")
				return m, flashCmd
			}
			if len(m.projects) > 1 {
				m.moveProjectPos = m.projectCursor
				m.appMode = modeMoveProject
			}
			return m, nil
		}
		if m.isGroupedView() {
			cmd := m.flash("Reorder not available in multi-project view")
			return m, cmd
		}
		if m.activePane == itemPane && row != nil {
			if row.kind == rowTask {
				flashCmd := m.flash("Reorder is only for items")
				return m, flashCmd
			}
			if len(m.items) > 1 {
				m.moveItemPos = row.itemIdx
				m.appMode = modeMove
			}
		}
		return m, nil

	case "b":
		if m.activePane == itemPane && row != nil {
			if row.kind == rowTask {
				flashCmd := m.flash("Dependency linking is only for items")
				return m, flashCmd
			}
			item := m.currentItem()
			m.depItemID = item.ID
			m.depItemName = item.Title
			return m, fetchDepCandidatesCmd(m.backend, m.projects)
		}
		return m, nil

	case "B":
		if m.activePane == itemPane && row != nil {
			if row.kind == rowTask {
				flashCmd := m.flash("Dependency unlinking is only for items")
				return m, flashCmd
			}
			item := m.currentItem()
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
			// Edit can be entered from either the main pane (currentItem)
			// or the detail overlay (itemDetail).
			var itemID string
			if m.returnMode == modeItemDetail && m.itemDetail != nil {
				itemID = m.itemDetail.ID
			} else if item := m.currentItem(); item != nil {
				itemID = item.ID
			} else {
				return m, nil
			}
			return m, updateItemCmd(m.backend, itemID, model.UpdateProjectItem{Title: &value})

		case modeEditProjectName:
			m.appMode = m.returnMode
			m.titleInput.Blur()
			if m.projectDetail != nil {
				return m, updateProjectCmd(m.backend, m.projectDetail.ID, model.UpdateProject{Name: &value})
			}
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

	// Adding a new task — route to text input. Applies regardless of
	// where the detail cursor is, since `t` always creates under the
	// overlay's single item.
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

	// detailTaskCursor encodes position:
	//   -1 = on the item
	//    0..len(detailTasks)-1 = on that task
	onTask := m.detailTaskCursor >= 0 && m.detailTaskCursor < len(m.detailTasks)

	switch msg.String() {
	case "esc", "q":
		m.appMode = modeNormal
		m.itemDetail = nil
		m.detailBlockers = nil
		m.detailTasks = nil
		return m, nil

	case "j", "down":
		// Walk -1 → 0 → 1 → … → len-1 (clamped).
		if m.detailTaskCursor < len(m.detailTasks)-1 {
			m.detailTaskCursor++
		}
		return m, nil

	case "k", "up":
		// Walk toward the item header, clamped at -1.
		if m.detailTaskCursor > -1 {
			m.detailTaskCursor--
		}
		return m, nil

	case "tab":
		// Toggle between item zone and first task; a quick keyboard jump
		// without having to j/k through any tasks in between.
		if onTask {
			m.detailTaskCursor = -1
		} else if len(m.detailTasks) > 0 {
			m.detailTaskCursor = 0
		}
		return m, nil

	case " ":
		if onTask {
			return m, toggleTaskCmd(m.backend, m.itemDetail.ID, m.detailTasks[m.detailTaskCursor])
		}
		// Item-level: same incomplete-task/blocked guards as main pane.
		for _, task := range m.detailTasks {
			if !task.Completed {
				flashCmd := m.flash("Cannot complete: item has incomplete tasks")
				return m, flashCmd
			}
		}
		if m.blockedSet[m.itemDetail.ID] && !m.itemDetail.Completed {
			flashCmd := m.flash("Cannot complete: item has unresolved blockers")
			return m, flashCmd
		}
		toggled := !m.itemDetail.Completed
		var flashCmd tea.Cmd
		if toggled {
			flashCmd = m.flash("Marked done")
		} else {
			flashCmd = m.flash("Marked incomplete")
		}
		return m, tea.Batch(updateItemCmd(m.backend, m.itemDetail.ID, model.UpdateProjectItem{Completed: &toggled}), flashCmd)

	case "d":
		if onTask {
			task := m.detailTasks[m.detailTaskCursor]
			// Nudge cursor up if we're deleting the last task row so the
			// post-refresh cursor doesn't land past the end.
			if m.detailTaskCursor >= len(m.detailTasks)-1 && m.detailTaskCursor > 0 {
				m.detailTaskCursor--
			} else if m.detailTaskCursor >= len(m.detailTasks)-1 {
				// Deleting the only task — return to the item.
				m.detailTaskCursor = -1
			}
			return m, deleteTaskCmd(m.backend, m.itemDetail.ID, task.ID)
		}
		return m, nil

	case "enter":
		// Inert on task rows (matches main pane).
		return m, nil

	case "x":
		if onTask {
			flashCmd := m.flash("Archive is only for items")
			return m, flashCmd
		}
		archived := true
		id := m.itemDetail.ID
		flashCmd := m.flash("Archived")
		m.appMode = modeNormal
		m.itemDetail = nil
		return m, tea.Batch(updateItemCmd(m.backend, id, model.UpdateProjectItem{Archived: &archived}), flashCmd)

	case "e":
		if onTask {
			flashCmd := m.flash("Edit is only for items")
			return m, flashCmd
		}
		m.titleInput.SetValue(m.itemDetail.Title)
		m.titleInput.Placeholder = ""
		cmd := m.titleInput.Focus()
		m.returnMode = modeItemDetail
		m.appMode = modeEditTitle
		return m, cmd

	case "n":
		if onTask {
			flashCmd := m.flash("Notes are only for items")
			return m, flashCmd
		}
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
		if onTask {
			flashCmd := m.flash("Project membership is only for items")
			return m, flashCmd
		}
		m.returnMode = modeItemDetail
		return m, fetchItemProjectsCmd(m.backend, m.itemDetail.ID)

	case "b":
		if onTask {
			flashCmd := m.flash("Dependency linking is only for items")
			return m, flashCmd
		}
		m.depItemID = m.itemDetail.ID
		m.depItemName = m.itemDetail.Title
		return m, fetchDepCandidatesCmd(m.backend, m.projects)

	case "B":
		if onTask {
			flashCmd := m.flash("Dependency unlinking is only for items")
			return m, flashCmd
		}
		m.depItemID = m.itemDetail.ID
		m.depItemName = m.itemDetail.Title
		return m, fetchDepBlockersCmd(m.backend, m.itemDetail.ID)

	case "t":
		// Always creates a task under the overlay's item, regardless of
		// whether the cursor is on the item or on an existing task.
		m.addingTask = true
		m.titleInput.SetValue("")
		m.titleInput.Placeholder = "New task..."
		return m, m.titleInput.Focus()

	case "u":
		return m, undoCmd(m.backend)
	}

	return m, nil
}

func (m *App) handleNotesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		value := m.notesInput.Value()
		m.appMode = m.returnMode
		m.notesInput.Blur()

		if m.returnMode == modeProjectDetail && m.projectDetail != nil {
			return m, updateProjectCmd(m.backend, m.projectDetail.ID, model.UpdateProject{Description: &value})
		}

		var itemID string
		if m.itemDetail != nil {
			itemID = m.itemDetail.ID
		} else if item := m.currentItem(); item != nil {
			itemID = item.ID
		} else {
			return m, nil
		}
		return m, updateItemCmd(m.backend, itemID, model.UpdateProjectItem{Notes: &value})

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

func (m *App) handleProjectDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.projectDetail == nil {
		return m, nil
	}

	switch msg.String() {
	case "esc", "q":
		m.appMode = modeNormal
		m.projectDetail = nil
		return m, nil

	case "e":
		m.titleInput.SetValue(m.projectDetail.Name)
		m.titleInput.Placeholder = ""
		cmd := m.titleInput.Focus()
		m.returnMode = modeProjectDetail
		m.appMode = modeEditProjectName
		return m, cmd

	case "d":
		desc := ""
		if m.projectDetail.Description != nil {
			desc = *m.projectDetail.Description
		}
		m.notesInput.SetValue(desc)
		cmd := m.notesInput.Focus()
		m.returnMode = modeProjectDetail
		m.appMode = modeEditProjectDesc
		return m, cmd
	}

	return m, nil
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
	// moveItemPos is the item-space index of the item being moved.
	// After every swap, rebuild m.rows and pin m.rowCursor to that
	// item's new header row so the visible cursor follows the item.
	switch msg.String() {
	case "j", "down":
		if m.moveItemPos < len(m.items)-1 {
			m.items[m.moveItemPos], m.items[m.moveItemPos+1] = m.items[m.moveItemPos+1], m.items[m.moveItemPos]
			m.moveItemPos++
			m.rebuildRows()
			m.rowCursor = m.firstRowForItem(m.moveItemPos)
		}
		return m, nil

	case "k", "up":
		if m.moveItemPos > 0 {
			m.items[m.moveItemPos], m.items[m.moveItemPos-1] = m.items[m.moveItemPos-1], m.items[m.moveItemPos]
			m.moveItemPos--
			m.rebuildRows()
			m.rowCursor = m.firstRowForItem(m.moveItemPos)
		}
		return m, nil

	case "g":
		for m.moveItemPos > 0 {
			m.items[m.moveItemPos], m.items[m.moveItemPos-1] = m.items[m.moveItemPos-1], m.items[m.moveItemPos]
			m.moveItemPos--
		}
		m.rebuildRows()
		m.rowCursor = m.firstRowForItem(m.moveItemPos)
		return m, nil

	case "G":
		for m.moveItemPos < len(m.items)-1 {
			m.items[m.moveItemPos], m.items[m.moveItemPos+1] = m.items[m.moveItemPos+1], m.items[m.moveItemPos]
			m.moveItemPos++
		}
		m.rebuildRows()
		m.rowCursor = m.firstRowForItem(m.moveItemPos)
		return m, nil

	case "enter":
		m.appMode = modeNormal
		item := m.items[m.moveItemPos]
		projectID := m.projects[m.projectCursor].ID
		return m, reorderItemCmd(m.backend, item.ID, projectID, m.moveItemPos)

	case "esc":
		m.appMode = modeNormal
		if cmd := m.fetchItems(); cmd != nil {
			return m, cmd
		}
		return m, nil
	}

	return m, nil
}

// handleMoveProjectKey mirrors handleMoveKey but operates on m.projects.
// Each swap updates projectCursor so the highlighted project follows its
// new position. Items are not refetched on swap (project reorder doesn't
// change the current project's item list). On enter, the new position
// is persisted via reorderProjectCmd. On esc, the project list is
// refetched to revert any in-memory reordering.
func (m *App) handleMoveProjectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.moveProjectPos < len(m.projects)-1 {
			m.projects[m.moveProjectPos], m.projects[m.moveProjectPos+1] = m.projects[m.moveProjectPos+1], m.projects[m.moveProjectPos]
			m.moveProjectPos++
			m.projectCursor = m.moveProjectPos
		}
		return m, nil

	case "k", "up":
		if m.moveProjectPos > 0 {
			m.projects[m.moveProjectPos], m.projects[m.moveProjectPos-1] = m.projects[m.moveProjectPos-1], m.projects[m.moveProjectPos]
			m.moveProjectPos--
			m.projectCursor = m.moveProjectPos
		}
		return m, nil

	case "g":
		for m.moveProjectPos > 0 {
			m.projects[m.moveProjectPos], m.projects[m.moveProjectPos-1] = m.projects[m.moveProjectPos-1], m.projects[m.moveProjectPos]
			m.moveProjectPos--
		}
		m.projectCursor = m.moveProjectPos
		return m, nil

	case "G":
		for m.moveProjectPos < len(m.projects)-1 {
			m.projects[m.moveProjectPos], m.projects[m.moveProjectPos+1] = m.projects[m.moveProjectPos+1], m.projects[m.moveProjectPos]
			m.moveProjectPos++
		}
		m.projectCursor = m.moveProjectPos
		return m, nil

	case "enter":
		m.appMode = modeNormal
		project := m.projects[m.moveProjectPos]
		return m, reorderProjectCmd(m.backend, project.ID, m.moveProjectPos)

	case "esc":
		m.appMode = modeNormal
		return m, fetchProjectsCmd(m.backend)
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

// --- Row model helpers ---

// rebuildRows regenerates m.rows from m.items and m.itemTasks. Call this
// whenever either changes. Rows are: one item row per item, followed by
// one task row per task belonging to that item (in task order).
func (m *App) rebuildRows() {
	rows := make([]paneRow, 0, len(m.items))
	for i, item := range m.items {
		rows = append(rows, paneRow{kind: rowItem, itemIdx: i, taskIdx: -1})
		for ti := range m.itemTasks[item.ID] {
			rows = append(rows, paneRow{kind: rowTask, itemIdx: i, taskIdx: ti})
		}
	}
	m.rows = rows
	if m.rowCursor >= len(m.rows) {
		m.rowCursor = max(0, len(m.rows)-1)
	}
}

// currentRow returns the row under the cursor, or nil if the item pane
// is empty.
func (m *App) currentRow() *paneRow {
	if m.rowCursor < 0 || m.rowCursor >= len(m.rows) {
		return nil
	}
	return &m.rows[m.rowCursor]
}

// currentItem returns the item owned by the row under the cursor — for
// a task row this is the task's parent item. Returns nil when the pane
// is empty.
func (m *App) currentItem() *model.ProjectItemInProject {
	row := m.currentRow()
	if row == nil {
		return nil
	}
	return &m.items[row.itemIdx]
}

// firstRowForItem returns the row index of the item row for the item at
// itemIdx. Returns 0 when no match (shouldn't happen for a valid item).
func (m *App) firstRowForItem(itemIdx int) int {
	for i, r := range m.rows {
		if r.kind == rowItem && r.itemIdx == itemIdx {
			return i
		}
	}
	return 0
}

// nextItemRow returns the row index of the next (dir == +1) or previous
// (dir == -1) item row relative to fromRow. When no such row exists, it
// returns fromRow unchanged — J/K stays put at list ends.
func (m *App) nextItemRow(fromRow, dir int) int {
	for i := fromRow + dir; i >= 0 && i < len(m.rows); i += dir {
		if m.rows[i].kind == rowItem {
			return i
		}
	}
	return fromRow
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
					m.rowCursor = 0
					return fetchItemsCmd(m.backend, m.projects[0].ID, m.filter)
				}
			}
		} else if m.projectCursor < len(m.projects)-1 {
			m.projectCursor++
			if !hasSelections {
				m.rowCursor = 0
				return fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
			}
		}
	} else if m.rowCursor < len(m.rows)-1 {
		m.rowCursor++
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
				m.rowCursor = 0
				return fetchItemsCmd(m.backend, m.projects[m.projectCursor].ID, m.filter)
			}
			return nil
		}
		// At first project, move up to All
		m.showingAll = true
		if !hasSelections {
			m.rowCursor = 0
			return fetchAllItemsCmd(m.backend, m.projects, m.filter)
		}
	} else if m.rowCursor > 0 {
		m.rowCursor--
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
	case modeAddItem, modeAddItemMulti, modeAddProject, modeEditTitle, modeEditProjectName:
		overlay := m.renderInputOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeProjectPicker:
		overlay := m.picker.view(m.width)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeItemDetail:
		overlay := m.renderDetailOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeProjectDetail:
		overlay := m.renderProjectDetailOverlay()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	case modeEditNotes, modeEditProjectDesc:
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
	case modeEditProjectName:
		prompt = "Edit project name"
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
	itemPrefix := "  "
	if m.detailTaskCursor == -1 {
		itemPrefix = itemSelectedStyle.Render("> ")
	}
	lines = append(lines, fmt.Sprintf("%s%s    %s", itemPrefix, d.Title, dimStyle.Render(status)))
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
			if i == m.detailTaskCursor {
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
	onTask := m.detailTaskCursor >= 0 && m.detailTaskCursor < len(m.detailTasks)
	switch {
	case m.addingTask:
		hints = "  [Enter]create  [Esc]cancel"
	case onTask:
		hints = "  [space]toggle  [d]elete  [t]add task  [Tab]item  [j/k]navigate  [Esc]close"
	default:
		hints = "  [e]dit  [n]otes  [p]rojects  [t]ask  [b]lock  [B]unblock  [space]done  [x]archive  [Tab/j]tasks  [Esc]close"
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

func (m *App) renderProjectDetailOverlay() string {
	p := m.projectDetail
	if p == nil {
		return ""
	}

	header := overlayTitleStyle.Render(fmt.Sprintf("Project %s", shortID(p.ID)))

	var lines []string
	lines = append(lines, header)
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s    %s",
		p.Name,
		dimStyle.Render(fmt.Sprintf("%d items", p.ItemCount)),
	))
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  Created: %s", p.CreatedAt.Format("Jan 2, 2006"))))

	if p.Description != nil && *p.Description != "" {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("  ─── Description ───────────────────"))
		for _, line := range strings.Split(*p.Description, "\n") {
			lines = append(lines, "  "+line)
		}
	}

	lines = append(lines, "")
	lines = append(lines, dimStyle.Render("  [e]dit name  [d]escription  [Esc]close"))

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
	var title, subtitle string
	if m.appMode == modeEditProjectDesc && m.projectDetail != nil {
		title = "Edit Description"
		subtitle = fmt.Sprintf("  Project: %s", m.projectDetail.Name)
	} else {
		title = "Edit Notes"
		if m.itemDetail != nil {
			subtitle = fmt.Sprintf("  Item: %s (%s)", m.itemDetail.Title, shortID(m.itemDetail.ID))
		} else if item := m.currentItem(); item != nil {
			subtitle = fmt.Sprintf("  Item: %s (%s)", item.Title, shortID(item.ID))
		}
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
	lines = append(lines, overlayTitleStyle.Render(title))
	lines = append(lines, dimStyle.Render(subtitle))
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
  j/k ↑/↓       Navigate rows (items & tasks)
  J/K            Jump to next/prev item (skip tasks)
  g/G            Jump to top/bottom
  Ctrl+d/u       Half-page down/up
  h/l ←/→       Switch panes
  Tab            Toggle pane focus
  Enter          Select / item detail`

	lines = append(lines, nav)

	if m.activePane == projectPane {
		actions := `
  Project Pane
  Enter          Project detail
  a              Add project
  m              Move/reorder project
  space          Toggle multi-select
  Esc            Clear selections`
		lines = append(lines, actions)
	} else {
		actions := `
  On an item row
  space          Toggle done
  a              Add item
  A              Add item to multiple projects
  e              Edit title
  n              Edit notes
  t              Add a task
  x              Archive item
  p              Manage project membership
  b              Link dependency (blocked by)
  B              Unlink dependency
  m              Move/reorder item

  On a task row
  space          Toggle task done
  d              Delete task
  t              Add a sibling task`
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
			isMoving := m.appMode == modeMoveProject && pi == m.moveProjectPos
			name := p.Name
			if isSelected {
				name = "● " + name
			}
			body := fmt.Sprintf("%s (%d)", name, p.ItemCount)

			var line string
			if isMoving {
				line = moveIndicatorStyle.Render("▶ " + body + " ◀ MOVING")
			} else {
				prefix := "  "
				if isCursor {
					prefix = "> "
				}
				line = prefix + body
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
	m.rowScroll = syncScroll(m.rowCursor, m.rowScroll, viewHeight)

	end := m.rowScroll + viewHeight
	if end > len(m.rows) {
		end = len(m.rows)
	}

	curRow := m.currentRow()
	linesUsed := 0
	for i := m.rowScroll; i < end; i++ {
		if linesUsed >= viewHeight {
			break
		}
		row := m.rows[i]
		item := m.items[row.itemIdx]

		switch row.kind {
		case rowItem:
			// Group headers live on item rows only.
			if m.isGroupedView() {
				if groupName := m.groupHeaderAt(row.itemIdx); groupName != "" {
					if row.itemIdx > 0 && linesUsed < viewHeight {
						lines = append(lines, "")
						linesUsed++
					}
					if linesUsed < viewHeight {
						header := groupHeaderStyle.Render(fmt.Sprintf("── %s ──", groupName))
						lines = append(lines, header)
						linesUsed++
					}
					if linesUsed >= viewHeight {
						break
					}
				}
			}
			isMoving := m.appMode == modeMove && row.itemIdx == m.moveItemPos
			lines = append(lines, m.renderItemLine(item, i == m.rowCursor, width, isMoving))
			linesUsed++

		case rowTask:
			tasks := m.itemTasks[item.ID]
			t := tasks[row.taskIdx]
			check := "○"
			title := t.Title
			if t.Completed {
				check = "✓"
				title = taskCompletedStyle.Render(title)
			}
			taskLine := fmt.Sprintf("%s %s", check, title)
			if i == m.rowCursor {
				lines = append(lines, taskSelectedStyle.Render("> "+taskLine))
			} else {
				lines = append(lines, taskNormalStyle.Render(taskLine))
			}
			linesUsed++
		}

		// Emit per-item trailers (add-task input, blockers, notes) after
		// the *last* row for this item. A row is the last of its item
		// when the next row belongs to a different item or we're at the
		// end of the rows slice.
		lastRowOfItem := i == len(m.rows)-1 || m.rows[i+1].itemIdx != row.itemIdx
		if !lastRowOfItem {
			continue
		}
		if linesUsed >= viewHeight {
			break
		}

		if m.itemAddingTask && curRow != nil && curRow.itemIdx == row.itemIdx && linesUsed < viewHeight {
			lines = append(lines, "     "+m.titleInput.View())
			linesUsed++
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
				lines = append(lines, blockerStyle.Render(
					fmt.Sprintf("└─ blocked by: %s%s (%s)", prefix, b.Title, shortID(b.ID)),
				))
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

	if len(m.rows) > viewHeight {
		scrollInfo := dimStyle.Render(fmt.Sprintf(" %d/%d", m.rowCursor+1, len(m.rows)))
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
		return moveIndicatorStyle.Render("[j/k] Move item  [g/G] Top/Bottom  [Enter] Confirm  [Esc] Cancel")
	case m.appMode == modeMoveProject:
		return moveIndicatorStyle.Render("[j/k] Move project  [g/G] Top/Bottom  [Enter] Confirm  [Esc] Cancel")
	case m.activePane == projectPane:
		hints := "[a]dd project [Enter]select [m]ove [Tab]items [/]search [?]help"
		if m.filter != filterNone {
			hints += " [0]reset filter"
		}
		return hints
	case m.itemAddingTask:
		return "[Enter]create task  [Esc]cancel"
	}
	// Hints depend on whether the cursor is on an item or a task row.
	if row := m.currentRow(); row != nil && row.kind == rowTask {
		return "[space]toggle [d]elete [t]ask [J/K]next/prev item [/]search [?]help"
	}
	hints := "[Enter]detail [space]done [a]dd [x]archive [e]dit [n]otes [t]ask [J/K]item [m]ove [b]lock [B]unblock [/]search [?]help"
	if m.filter != filterNone {
		hints += " [0]reset"
	}
	return hints
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
