package tui

import (
	"context"
	"fmt"
	"strconv"
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
	rows []paneRow

	// Target of modeAddTask, captured on keypress. The cursor cannot move
	// while the overlay is up, but resolving the item once keeps the
	// create independent of any refresh that reorders m.items underneath.
	addTaskItemID    string
	addTaskItemTitle string

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

	// Dependency tree overlay
	depTree depTreeState
	// detailFromTree routes the item detail back to the tree it was opened
	// from. Not returnMode, which the input overlays claim while the detail is
	// still on screen and would overwrite this.
	detailFromTree bool

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
	depItems      []model.ProjectItem // flat list of selectable items (filtered view)
	depAllItems   []depGroup          // all items grouped by project (source of truth)
	depCursor     int
	depItemID     string
	depItemName   string
	depItemHandle string
	depFilter     textinput.Model
	depFiltering  bool // true when typing in filter input

	// All Items view
	showingAll       bool            // true when the "All" pseudo-project is selected
	showClosed       bool            // include completed and dropped projects in the pane
	allGroups        []allItemGroup  // project groups for rendering headers in grouped views
	selectedProjects map[string]bool // multi-select: project IDs toggled via space

	// Navigation
	pendingItemID string // after fetch, select this item

	errorMsg string // transient error shown in status bar
	loading  bool   // true while an async operation is in-flight

	// Sync
	syncEngine   *sync.Engine
	syncStatus   sync.SyncStatus
	syncInterval time.Duration

	// Display info
	dbPath string
}

// NewApp creates a new TUI application backed by the given Backend.
// syncEngine may be nil when sync is disabled.
func NewApp(b backend.Backend, syncEngine *sync.Engine, dbPath string, syncInterval time.Duration) *App {
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
		syncInterval: syncInterval,
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

// Sync messages. A pull carries whether the user asked for it: an automatic one
// runs every syncInterval, so announcing it would flash the status bar forever
// and a transient failure would pin an error banner over a working app.
type (
	syncStatusMsg   sync.SyncStatus
	syncPullTickMsg struct{}
	syncPullDoneMsg struct{ manual bool }
	syncPullErrMsg  struct {
		error
		manual bool
	}
)

const statusFlashDuration = 3 * time.Second

func (m *App) flash(msg string) tea.Cmd {
	m.statusMsg = msg
	return tea.Tick(statusFlashDuration, func(_ time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

// itemHandle names an item the way it is typed on the command line: its number,
// or the UUID tail while it is still waiting for one. An item created here
// while the API was unreachable has no number until its create is pushed, and
// showing nothing in that window would leave the row unnameable.
func itemHandle(item model.ProjectItem) string {
	if item.Number != nil {
		return strconv.Itoa(*item.Number)
	}
	return shortID(item.ID)
}

// shortID returns the last 8 characters of a UUID for display. Projects and
// tasks have no number of their own, so it is still their handle. UUIDv7
// front-loads the 48-bit millisecond timestamp, so any prefix is identical for
// everything created in the same ~65s window. The entropy is in the tail.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[len(id)-8:]
	}
	return id
}

func truncate(s string, maxWidth int) string {
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	if maxWidth <= 1 {
		return "…"
	}
	return string(runes[:maxWidth-1]) + "…"
}

// wrapPoints walks the break positions of a line wrapped to maxWidth, calling
// emit with each piece. Runes rather than bytes throughout: a hard break by byte
// offset splits a multi-byte rune in half, and the notes this wraps are full of
// em dashes and arrows.
func wrapPoints(s string, maxWidth int, emit func(piece []rune)) {
	runes := []rune(s)
	if len(runes) <= maxWidth {
		emit(runes)
		return
	}
	for len(runes) > maxWidth {
		cut := maxWidth
		for cut > 0 && runes[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = maxWidth // no space found, hard break
		}
		emit(runes[:cut])
		runes = runes[cut:]
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		emit(runes)
	}
}

func wrapLine(s string, maxWidth int) []string {
	var result []string
	wrapPoints(s, maxWidth, func(piece []rune) { result = append(result, string(piece)) })
	return result
}

// wrapCount is wrapLine's height without its allocations. The measure pass runs
// over every item on every frame, so building the strings only to count them
// was most of what a render cost on a real database.
func wrapCount(s string, maxWidth int) int {
	n := 0
	wrapPoints(s, maxWidth, func([]rune) { n++ })
	return n
}

// --- Sync commands ---

func syncPullCmd(e *sync.Engine, manual bool) tea.Cmd {
	return func() tea.Msg {
		if err := e.Pull(context.Background()); err != nil {
			return syncPullErrMsg{error: err, manual: manual}
		}
		return syncPullDoneMsg{manual: manual}
	}
}

func syncStatusTickCmd(e *sync.Engine) tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
		return syncStatusMsg(e.Status())
	})
}

func syncPullTickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(_ time.Time) tea.Msg {
		return syncPullTickMsg{}
	})
}

// safeToAutoPull reports whether a background reconcile can run without pulling
// the ground out from under the user. A pull rewrites items, memberships and
// ordering wholesale, which would corrupt a grab in progress or refresh a
// detail overlay into a snapshot of something else. Every other mode defers to
// the next tick rather than skipping the pull outright.
// projectStatusFilter is what the project pane is currently asking for. Closing
// a project is what takes it out of the list, so the default hides the terminal
// ones and `C` is how you get at them.
func (m *App) projectStatusFilter() string {
	if m.showClosed {
		return model.StatusAll
	}
	return model.StatusActive
}

func (m *App) safeToAutoPull() bool {
	return m.appMode == modeNormal
}

// --- Commands ---

// fetchProjectsCmd loads the project pane. The status filter is threaded through
// rather than read from the model because every refresh runs as a command off
// the model's goroutine, and a toggle mid-flight would otherwise decide which
// list a already-issued fetch returns.
func fetchProjectsCmd(b backend.Backend, status string) tea.Cmd {
	return func() tea.Msg {
		projects, err := b.ListProjectsByStatus(status)
		if err != nil {
			return errMsg{err}
		}
		return projectsMsg(projects)
	}
}

// setProjectStatusCmd runs a project transition. A drop without a reason is
// refused by the backend, so the flash carries that message rather than the
// caller pre-checking it.
func setProjectStatusCmd(b backend.Backend, id, status string, reason *string) tea.Cmd {
	return func() tea.Msg {
		if _, err := b.SetProjectStatus(id, status, reason); err != nil {
			return errMsg{err}
		}
		return projectUpdatedMsg{}
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
		result, err := b.Undo()
		if err != nil {
			return errMsg{err}
		}
		return undoResultMsg(result.Description)
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
			fetchProjectsCmd(m.backend, m.projectStatusFilter()),
			syncPullCmd(m.syncEngine, false),
			syncStatusTickCmd(m.syncEngine),
			syncPullTickCmd(m.syncInterval),
		)
	}
	return fetchProjectsCmd(m.backend, m.projectStatusFilter())
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
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), flashCmd)

	case itemUpdatedMsg:
		var flashCmd tea.Cmd
		if m.statusMsg == "" {
			flashCmd = m.flash("Item updated")
		}
		if m.appMode == modeItemDetail && m.itemDetail != nil {
			return m, tea.Batch(
				fetchProjectsCmd(m.backend, m.projectStatusFilter()),
				fetchItemDetailCmd(m.backend, m.itemDetail.ID, m.blockedSet[m.itemDetail.ID]),
				flashCmd,
			)
		}
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), flashCmd)

	case projectCreatedMsg:
		flashCmd := m.flash("Project created")
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), flashCmd)

	case projectUpdatedMsg:
		flashCmd := m.flash("Project updated")
		if m.appMode == modeProjectDetail && m.projectDetail != nil {
			return m, tea.Batch(
				fetchProjectsCmd(m.backend, m.projectStatusFilter()),
				fetchProjectDetailCmd(m.backend, m.projectDetail.ID),
				flashCmd,
			)
		}
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), flashCmd)

	case projectDetailMsg:
		m.projectDetail = msg.project
		m.appMode = modeProjectDetail
		return m, nil

	case undoResultMsg:
		flashCmd := m.flash(fmt.Sprintf("Undo: %s", string(msg)))
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), flashCmd)

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

	case depTreeMsg:
		m.depTree.tree = msg.tree
		m.depTree.lookup = msg.lookup
		m.depTree.rebuildForest()
		// Opening on an item puts the forest cursor there, so the browser lands
		// on what you were looking at. Opening cold starts at the top.
		var missCmd tea.Cmd
		if msg.focusID != "" && !m.depTree.standOn(msg.focusID) {
			missCmd = m.flash(m.depTreeMiss(msg.focusID))
		}
		m.depTree.focusOn(m.depTree.forestID())
		m.appMode = modeDepTree
		return m, missCmd

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
				fetchProjectsCmd(m.backend, m.projectStatusFilter()),
				fetchItemDetailCmd(m.backend, m.itemDetail.ID, m.blockedSet[m.itemDetail.ID]),
				flashCmd,
			)
		}
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), flashCmd)

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
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), flashCmd)

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
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), flashCmd)

	case depUnlinkedMsg:
		flashCmd := m.flash("Dependency unlinked")
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), flashCmd)

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case errMsg:
		m.loading = false
		m.errorMsg = msg.Error()
		return m, nil

	case syncPullTickMsg:
		next := syncPullTickCmd(m.syncInterval)
		if m.syncEngine == nil || !m.safeToAutoPull() {
			return m, next
		}
		return m, tea.Batch(syncPullCmd(m.syncEngine, false), next)

	case syncPullDoneMsg:
		if !msg.manual {
			return m, fetchProjectsCmd(m.backend, m.projectStatusFilter())
		}
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), m.flash("Synced with server"))

	case syncPullErrMsg:
		// An automatic pull failing is not an error the user did anything to
		// cause. The status bar already reads SYNC ERR off the engine status;
		// hijacking the bar every interval would bury real messages.
		if msg.manual {
			m.errorMsg = fmt.Sprintf("Sync: %v", msg.error)
		}
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
	case modeAddItem, modeAddItemMulti, modeAddProject, modeAddTask, modeEditTitle:
		return m.handleInputKey(msg)
	case modeProjectPicker:
		return m.handlePickerKey(msg)
	case modeItemDetail:
		return m.handleDetailKey(msg)
	case modeEditNotes, modeEditProjectDesc:
		return m.handleNotesKey(msg)
	case modeProjectDetail:
		return m.handleProjectDetailKey(msg)
	case modeEditProjectName, modeDropProject:
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
	case modeDepTree:
		return m.handleDepTreeKey(msg)
	}
	return m, nil
}

func (m *App) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

	case "C":
		if m.activePane != projectPane {
			return m, nil
		}
		m.showClosed = !m.showClosed
		// The cursor indexes the list that is about to be replaced, and the
		// shorter list is the one that would panic.
		m.projectCursor = 0
		m.projectScroll = 0
		m.selectedProjects = nil
		note := "Closed projects hidden"
		if m.showClosed {
			note = "Showing closed projects"
		}
		return m, tea.Batch(fetchProjectsCmd(m.backend, m.projectStatusFilter()), m.flash(note))

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
			// Works from an item row or one of its task rows — both
			// resolve to the owning item.
			item := m.currentItem()
			if item == nil {
				return m, nil
			}
			m.addTaskItemID = item.ID
			m.addTaskItemTitle = item.Title
			m.titleInput.SetValue("")
			m.titleInput.Placeholder = "New task..."
			m.returnMode = modeNormal
			m.appMode = modeAddTask
			return m, m.titleInput.Focus()
		}
		return m, nil

	case "u":
		return m, undoCmd(m.backend)

	// --- Phase 6: Advanced ---

	case "R":
		if m.syncEngine == nil {
			flashCmd := m.flash("Sync is disabled — running local-only")
			return m, flashCmd
		}
		return m, syncPullCmd(m.syncEngine, true)

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
			m.depItemHandle = itemHandle(item.ProjectItem)
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
			m.depItemHandle = itemHandle(item.ProjectItem)
			return m, fetchDepBlockersCmd(m.backend, item.ID)
		}
		return m, nil

	case "T":
		// Works from either pane. On an item it opens the browser standing on
		// that item; anywhere else it opens at the top of the forest, so
		// browsing the dependencies never requires finding an item first.
		focus := ""
		if m.activePane == itemPane && row != nil {
			focus = m.currentItem().ID
		}
		treeCmd := m.openDepTree(focus)
		return m, treeCmd
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

		case modeAddTask:
			m.appMode = modeNormal
			m.titleInput.Blur()
			if m.addTaskItemID == "" {
				return m, nil
			}
			return m, createTaskCmd(m.backend, m.addTaskItemID, value)

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

		case modeDropProject:
			m.appMode = m.returnMode
			m.titleInput.Blur()
			if m.projectDetail != nil {
				return m, setProjectStatusCmd(m.backend, m.projectDetail.ID, model.StatusDropped, &value)
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
		m.itemDetail = nil
		m.detailBlockers = nil
		m.detailTasks = nil
		if m.detailFromTree && m.depTree.tree != nil {
			m.detailFromTree = false
			m.appMode = modeDepTree
			return m, nil
		}
		m.appMode = modeNormal
		// The tree holds a snapshot of every item; drop it with the overlay
		// rather than carrying it for the rest of the session.
		m.depTree = depTreeState{}
		m.detailFromTree = false
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
		m.depItemHandle = itemHandle(m.itemDetail.ProjectItem)
		return m, fetchDepCandidatesCmd(m.backend, m.projects)

	case "B":
		if onTask {
			flashCmd := m.flash("Dependency unlinking is only for items")
			return m, flashCmd
		}
		m.depItemID = m.itemDetail.ID
		m.depItemName = m.itemDetail.Title
		m.depItemHandle = itemHandle(m.itemDetail.ProjectItem)
		return m, fetchDepBlockersCmd(m.backend, m.itemDetail.ID)

	case "T":
		// The blockers listed above are one level deep and lead nowhere. This
		// is the same relation with the rest of the chain attached, and it can
		// be walked.
		treeCmd := m.openDepTree(m.itemDetail.ID)
		return m, treeCmd

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

	case "c":
		if model.IsTerminalStatus(m.projectDetail.Status) {
			flashCmd := m.flash("Already closed — r reopens it")
			return m, flashCmd
		}
		return m, setProjectStatusCmd(m.backend, m.projectDetail.ID, model.StatusCompleted, nil)

	case "x":
		if model.IsTerminalStatus(m.projectDetail.Status) {
			flashCmd := m.flash("Already closed — r reopens it")
			return m, flashCmd
		}
		// A reason is required, so dropping is a prompt rather than a keypress:
		// "deferred" invites the same idea back, "dropped, and here is why" does not.
		m.titleInput.SetValue("")
		m.titleInput.Placeholder = "Why dropped rather than deferred?"
		cmd := m.titleInput.Focus()
		m.returnMode = modeProjectDetail
		m.appMode = modeDropProject
		return m, cmd

	case "r":
		if !model.IsTerminalStatus(m.projectDetail.Status) {
			flashCmd := m.flash("Already active")
			return m, flashCmd
		}
		return m, setProjectStatusCmd(m.backend, m.projectDetail.ID, model.StatusActive, nil)
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
		return m, fetchProjectsCmd(m.backend, m.projectStatusFilter())
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

// scrollToBlock places the viewport so that the whole block of lines a row
// renders into is visible. Distinct from syncScroll, which assumes one line per
// entry and is what the project pane can still use.
//
// A block taller than the viewport is pinned to its first line: showing its
// tail instead would hide the row the cursor is actually on.
func scrollToBlock(start, span, scroll, viewHeight int) int {
	if viewHeight <= 0 {
		return 0
	}
	span = min(span, viewHeight)
	if start < scroll {
		return start
	}
	if start+span > scroll+viewHeight {
		return start + span - viewHeight
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
	case modeAddItem, modeAddItemMulti, modeAddProject, modeAddTask, modeEditTitle, modeEditProjectName, modeDropProject:
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
	case modeDepTree:
		// A full screen rather than an overlay: the map and the tree you are
		// standing in are two panes, the same shape as projects and items.
		return m.renderDepTreeView()
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

// projectName names the overlay's project, or nothing when the overlay opened
// without one — a prompt is not worth a nil check at every call site.
func projectName(p *model.ProjectWithItemCount) string {
	if p == nil {
		return ""
	}
	return p.Name
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
	case modeAddTask:
		prompt = fmt.Sprintf("New task for %q", truncate(m.addTaskItemTitle, 40))
	case modeEditTitle:
		prompt = "Edit title"
	case modeEditProjectName:
		prompt = "Edit project name"
	case modeDropProject:
		prompt = fmt.Sprintf("Drop %q — why, rather than deferring it?", truncate(projectName(m.projectDetail), 40))
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

	header := overlayTitleStyle.Render(fmt.Sprintf("Item %s", itemHandle(d.ProjectItem)))

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
	if d.Repo != nil && *d.Repo != "" {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  Repo: %s", *d.Repo)))
	}
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
				fmt.Sprintf("○ %s%s (%s)", prefix, b.Title, itemHandle(b)),
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
		hints = "  [e]dit  [n]otes  [p]rojects  [t]ask  [b]lock  [B]unblock  [T]ree  [space]done  [x]archive  [Tab/j]tasks  [Esc]close"
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
	lines = append(lines, fmt.Sprintf(
		"  %s    %s",
		p.Name,
		dimStyle.Render(fmt.Sprintf("%d items", p.ItemCount)),
	))
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  Created: %s", p.CreatedAt.Format("Jan 2, 2006"))))
	if model.IsTerminalStatus(p.Status) {
		closed := p.Status
		if p.ClosedAt != nil {
			closed += " " + p.ClosedAt.Format("Jan 2, 2006")
		}
		lines = append(lines, dimStyle.Render("  "+closed))
		if p.StatusReason != nil && *p.StatusReason != "" {
			lines = append(lines, dimStyle.Render("  Reason: "+*p.StatusReason))
		}
	}

	if p.Description != nil && *p.Description != "" {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("  ─── Description ───────────────────"))
		for _, line := range strings.Split(*p.Description, "\n") {
			lines = append(lines, "  "+line)
		}
	}

	lines = append(lines, "")
	actions := "  [e]dit name  [d]escription  [c]omplete  dro[x]  [Esc]close"
	if model.IsTerminalStatus(p.Status) {
		actions = "  [e]dit name  [d]escription  [r]eopen  [Esc]close"
	}
	lines = append(lines, dimStyle.Render(actions))

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
			subtitle = fmt.Sprintf("  Item: %s (%s)", m.itemDetail.Title, itemHandle(m.itemDetail.ProjectItem))
		} else if item := m.currentItem(); item != nil {
			subtitle = fmt.Sprintf("  Item: %s (%s)", item.Title, itemHandle(item.ProjectItem))
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
  C              Show/hide closed projects
  space          Toggle multi-select
  Esc            Clear selections

  In project detail
  c              Complete project
  x              Drop project (asks why)
  r              Reopen a closed project`
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
  T              Dependency browser (map + tree)
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
  R              Sync now (runs automatically too)
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
		line := fmt.Sprintf("%s %s  %s", status, item.Title, itemIDStyle.Render(itemHandle(item)))
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
		header = fmt.Sprintf("Unlink dependency from: %s (%s)", m.depItemName, m.depItemHandle)
	} else {
		header = fmt.Sprintf("Link dependency for: %s (%s)", m.depItemName, m.depItemHandle)
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
					line := fmt.Sprintf("%s %s  %s", status, item.Title, itemIDStyle.Render(itemHandle(item)))
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
				line := fmt.Sprintf("%s %s  %s", status, item.Title, itemIDStyle.Render(itemHandle(item)))
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

// projectCounts renders a project's open and done tallies. The glyphs are the
// item pane's own — ○ for open, ✓ for done — so the two panes read as one
// vocabulary, and a zero goes dim rather than disappearing so the column stays
// aligned down the list.
func projectCounts(open, done int) string {
	openText := projectOpenCountStyle.Render(fmt.Sprintf("○%d", open))
	if open == 0 {
		openText = dimStyle.Render(fmt.Sprintf("○%d", open))
	}
	doneText := projectDoneCountStyle.Render(fmt.Sprintf("✓%d", done))
	if done == 0 {
		doneText = dimStyle.Render(fmt.Sprintf("✓%d", done))
	}
	return openText + " " + doneText
}

// projectCountsWidth is the printable width of what projectCounts renders,
// measured off the unstyled text because lipgloss.Width would have to strip the
// color codes back out again.
func projectCountsWidth(open, done int) int {
	return lipgloss.Width(fmt.Sprintf("○%d ✓%d", open, done))
}

// projectRowLabel is the text half of a project row. The width calculation and
// the render both go through it, so a label the pane is not wide enough for
// cannot happen the way the status suffix once did.
func (m *App) projectRowLabel(p model.ProjectWithItemCount) string {
	label := p.Name
	if m.selectedProjects[p.ID] {
		label = "● " + label
	}
	// Only while closed projects are on show: every row reads "active"
	// otherwise, which spends width to say nothing.
	if m.showClosed && model.IsTerminalStatus(p.Status) {
		label = fmt.Sprintf("%s [%s]", label, p.Status)
	}
	return label
}

func (m *App) projectPaneWidth() int {
	const minWidth = 20
	// Account for "All" entry
	totalItems, totalDone := 0, 0
	for _, p := range m.projects {
		totalItems += p.ItemCount
		totalDone += p.CompletedCount
	}
	maxName := lipgloss.Width("> All ") + projectCountsWidth(totalItems-totalDone, totalDone)
	for _, p := range m.projects {
		// "> name ○open ✓done" — 2 prefix + name + space + counts
		w := lipgloss.Width("> "+m.projectRowLabel(p)+" ") + projectCountsWidth(p.OpenCount(), p.CompletedCount)
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
	// The scroll indicator is a line the list does not get. Appending it after
	// filling the viewport is what pushed the pane past the height it was handed.
	showIndicator := totalEntries > viewHeight
	if showIndicator {
		viewHeight--
	}
	if viewHeight < 1 {
		viewHeight = 1
	}
	virtualCursor := m.projectCursor + 1 // offset for All entry
	if m.showingAll {
		virtualCursor = 0
	}
	m.projectScroll = syncScroll(virtualCursor, m.projectScroll, viewHeight)

	end := m.projectScroll + viewHeight
	if end > totalEntries {
		end = totalEntries
	}

	// Only the name is truncated. Slicing the finished line by byte, which is
	// what this did, cuts a multi-byte rune in half as soon as a name is long
	// enough to reach the ● or the count glyphs.
	row := func(prefix, name string, open, done int, style lipgloss.Style) string {
		avail := width - lipgloss.Width(prefix) - style.GetPaddingLeft() - projectCountsWidth(open, done) - 1
		if avail < 1 {
			avail = 1
		}
		return style.Render(prefix+truncate(name, avail)) + " " + projectCounts(open, done)
	}

	hasSelections := len(m.selectedProjects) > 0
	for vi := m.projectScroll; vi < end; vi++ {
		if vi == 0 {
			// "All" entry
			totalItems, totalDone := 0, 0
			for _, p := range m.projects {
				totalItems += p.ItemCount
				totalDone += p.CompletedCount
			}
			isCursor := m.showingAll
			allSelected := hasSelections && len(m.selectedProjects) == len(m.projects)
			prefix := "  "
			if isCursor {
				prefix = "> "
			}
			label := "All"
			if allSelected {
				label = "● All"
			}
			style := projectNormalStyle
			switch {
			case isCursor:
				style = allProjectStyle
			case allSelected:
				style = projectMultiSelectedStyle
			}
			lines = append(lines, row(prefix, label, totalItems-totalDone, totalDone, style))
		} else {
			// Real project at index vi-1
			pi := vi - 1
			p := m.projects[pi]
			isCursor := !m.showingAll && pi == m.projectCursor
			isSelected := m.selectedProjects[p.ID]
			isMoving := m.appMode == modeMoveProject && pi == m.moveProjectPos
			name := m.projectRowLabel(p)

			if isMoving {
				lines = append(lines, moveIndicatorStyle.Render("▶ "+name+" ◀ MOVING"))
				continue
			}
			prefix := "  "
			if isCursor {
				prefix = "> "
			}
			style := projectNormalStyle
			switch {
			case isCursor:
				style = projectSelectedStyle
			case isSelected:
				style = projectMultiSelectedStyle
			}
			lines = append(lines, row(prefix, name, p.OpenCount(), p.CompletedCount, style))
		}
	}

	if showIndicator {
		pos := virtualCursor + 1
		lines = append(lines, dimStyle.Render(fmt.Sprintf(" %d/%d", pos, totalEntries)))
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

	var filterLabel string
	switch m.filter {
	case filterBlocked:
		filterLabel = " [BLOCKED]"
	case filterAll:
		filterLabel = " [ALL]"
	}

	// A project name is free text and routinely longer than the pane. Left
	// whole it wraps at the border and costs the list a line the scroll never
	// counted, which is the same failure as an over-long row.
	titleText = truncate(titleText, max(width-1-lipgloss.Width(filterLabel), 1))
	if filterLabel != "" {
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

	layout := m.itemPaneLayout(width)

	viewHeight := height - 1 // subtract title
	// The scroll indicator is a line the list does not get. Appending it after
	// filling the viewport is what pushed the pane past the height it was handed.
	showIndicator := layout.total > viewHeight
	if showIndicator {
		viewHeight--
	}
	if viewHeight < 1 {
		viewHeight = 1
	}

	if c := m.rowCursor; c >= 0 && c < len(m.rows) {
		blockEnd := layout.blockStart[c] + layout.height[c]
		m.rowScroll = scrollToBlock(layout.anchor[c], blockEnd-layout.anchor[c], m.rowScroll, viewHeight)
	}
	if maxScroll := layout.total - viewHeight; m.rowScroll > maxScroll {
		m.rowScroll = max(0, maxScroll)
	}

	lines = append(lines, m.renderItemWindow(width, layout, m.rowScroll, viewHeight)...)

	if showIndicator {
		lines = append(lines, dimStyle.Render(fmt.Sprintf(" %d/%d", m.rowCursor+1, len(m.rows))))
	}

	return strings.Join(lines, "\n")
}

// paneLayout is where every row of the item pane lands in line space. The pane
// used to scroll by row index while rendering by line, so the two disagreed the
// moment a row was worth more than one line — a group header, a blocker, a
// wrapped note: the scroll offset believed the cursor was on screen while the
// render ran out of lines before reaching it, and the pane stopped following
// the cursor at all.
type paneLayout struct {
	// blockStart is the row's first line, counting any blank separator above a
	// group header. anchor is the first line that has to stay on screen, which
	// skips that blank — scrolling to a header should not spend the top line on
	// nothing.
	blockStart []int
	anchor     []int
	height     []int
	total      int
}

// rowAt returns the row whose block contains the given line.
func (l paneLayout) rowAt(line int) int {
	lo, hi := 0, len(l.blockStart)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if l.blockStart[mid] <= line {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// itemPaneLayout measures every row without drawing it. Measuring and drawing
// are separate passes because the viewport needs every height to place itself
// while only the rows it lands on need their strings built — styling all 250
// items of a real database to show twenty of them cost 50ms on every keystroke.
func (m *App) itemPaneLayout(width int) paneLayout {
	l := paneLayout{
		blockStart: make([]int, len(m.rows)),
		anchor:     make([]int, len(m.rows)),
		height:     make([]int, len(m.rows)),
	}
	for i := range m.rows {
		lead := 0
		if m.rowHasLeadingBlank(i) {
			lead = 1
		}
		body := 1 // the item or task line
		if m.rowGroupHeader(i) != "" {
			body++
		}
		if m.rowIsLastOfItem(i) {
			body += m.itemTrailerHeight(m.items[m.rows[i].itemIdx], width)
		}
		l.blockStart[i] = l.total
		l.anchor[i] = l.total + lead
		l.height[i] = lead + body
		l.total += l.height[i]
	}
	return l
}

// renderItemWindow draws only the rows the viewport lands on.
func (m *App) renderItemWindow(width int, layout paneLayout, from, count int) []string {
	if count <= 0 || from >= layout.total || len(m.rows) == 0 {
		return nil
	}
	i := layout.rowAt(from)
	skip := from - layout.blockStart[i]

	var built []string
	for ; i < len(m.rows) && len(built) < skip+count; i++ {
		built = append(built, m.renderRowBlock(i, width)...)
	}
	if skip >= len(built) {
		return nil
	}
	built = built[skip:]
	if len(built) > count {
		built = built[:count]
	}
	return built
}

// rowGroupHeader is the group heading a row opens, or "" for a row inside one.
// Headers hang off item rows only.
func (m *App) rowGroupHeader(i int) string {
	if m.rows[i].kind != rowItem || !m.isGroupedView() {
		return ""
	}
	return m.groupHeaderAt(m.rows[i].itemIdx)
}

func (m *App) rowHasLeadingBlank(i int) bool {
	return m.rowGroupHeader(i) != "" && m.rows[i].itemIdx > 0
}

// rowIsLastOfItem reports whether an item's trailers hang off this row.
func (m *App) rowIsLastOfItem(i int) bool {
	return i == len(m.rows)-1 || m.rows[i+1].itemIdx != m.rows[i].itemIdx
}

// renderRowBlock draws one row: its group heading if it opens one, the row
// itself, and the item's trailers if it is the last row of that item. Its line
// count must match what itemPaneLayout measured, which TestLayoutMatchesRender
// holds it to.
func (m *App) renderRowBlock(i, width int) []string {
	row := m.rows[i]
	item := m.items[row.itemIdx]

	var lines []string
	if m.rowHasLeadingBlank(i) {
		lines = append(lines, "")
	}
	if groupName := m.rowGroupHeader(i); groupName != "" {
		lines = append(lines, groupHeaderStyle.Render(
			fmt.Sprintf("── %s ──", truncate(groupName, max(width-7, 1))),
		))
	}

	switch row.kind {
	case rowItem:
		isMoving := m.appMode == modeMove && row.itemIdx == m.moveItemPos
		lines = append(lines, m.renderItemLine(item, i == m.rowCursor, width, isMoving))
	case rowTask:
		t := m.itemTasks[item.ID][row.taskIdx]
		check := "○"
		// The deepest indent in the pane, measured on the cursor row, which is
		// the wider of the two: 4 columns of padding, the "> " marker, the
		// glyph and its space.
		title := truncate(t.Title, max(width-8, 1))
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
	}

	if m.rowIsLastOfItem(i) {
		lines = append(lines, m.itemTrailerLines(item, width)...)
	}
	return lines
}

// itemTrailerHeight counts what itemTrailerLines would draw. It walks the same
// wrapping rather than estimating it: a height that disagrees with the render
// by one line is the scroll bug all over again.
func (m *App) itemTrailerHeight(item model.ProjectItemInProject, width int) int {
	height := len(m.itemBlockers[item.ID])
	if item.Notes == nil || *item.Notes == "" {
		return height
	}
	wrapWidth := max(width-16, 1)
	for _, noteLine := range strings.Split(*item.Notes, "\n") {
		height += wrapCount(noteLine, wrapWidth)
	}
	return height
}

// itemTrailerLines renders what hangs below an item: what blocks it, and a
// preview of its notes.
func (m *App) itemTrailerLines(item model.ProjectItemInProject, width int) []string {
	var lines []string

	if blockers := m.itemBlockers[item.ID]; len(blockers) > 0 {
		currentProject := ""
		if !m.isGroupedView() && m.projectCursor < len(m.projects) {
			currentProject = m.projects[m.projectCursor].Name
		}
		for _, b := range blockers {
			// A blocker filed elsewhere is named by its project; one in the
			// project already on screen would only repeat the pane title.
			prefixText := ""
			if names, ok := m.itemProjectNames[b.ID]; ok && len(names) > 0 {
				inCurrent := false
				for _, n := range names {
					if n == currentProject {
						inCurrent = true
						break
					}
				}
				if !inCurrent {
					// Capped rather than given the whole line: a project name
					// can be longer than the pane on its own, and the blocker's
					// own title is the part worth reading.
					prefixText = truncate(names[0], max(width/4, 4)) + ": "
				}
			}
			prefix := ""
			if prefixText != "" {
				prefix = blockerProjectStyle.Render(prefixText)
			}
			handle := itemHandle(b)
			skeleton := func(p string) int {
				return lipgloss.Width(fmt.Sprintf("     └─ blocked by: %s (%s)", p, handle))
			}
			// A narrow pane gives up the owning project before it gives up the
			// blocker's own title, and if even that will not fit the whole line
			// is cut as one — a budget of "at least one character" still
			// overflows when the scaffolding alone is wider than the pane.
			if skeleton(prefixText) >= width {
				prefixText, prefix = "", ""
			}
			if budget := width - skeleton(prefixText); budget >= 1 {
				lines = append(lines, blockerStyle.Render(
					fmt.Sprintf("└─ blocked by: %s%s (%s)", prefix, truncate(b.Title, budget), handle),
				))
			} else {
				lines = append(lines, blockerStyle.Render(truncate(
					fmt.Sprintf("└─ blocked by: %s (%s)", b.Title, handle), max(width-5, 1),
				)))
			}
		}
	}

	if item.Notes == nil || *item.Notes == "" {
		return lines
	}

	notePrefix := "     " + notesConnectorStyle.Render("└─ notes ▸ ")
	// 16 is what the connector and the continuation indent both cost. A floor
	// above that would put the note past the pane border in a narrow window.
	wrapWidth := max(width-16, 1)
	first := true
	for _, noteLine := range strings.Split(*item.Notes, "\n") {
		for _, wrapped := range wrapLine(noteLine, wrapWidth) {
			if first {
				lines = append(lines, notePrefix+wrapped)
				first = false
			} else {
				lines = append(lines, notesPreviewStyle.Render(wrapped))
			}
		}
	}
	return lines
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

	idText := itemHandle(item.ProjectItem)

	// Everything but the title is fixed width, so the title is what gives when
	// the row will not fit. An over-long row is not merely untidy: lipgloss
	// wraps it at the pane border, spending a line the scroll never counted and
	// pushing the pane past the height it was handed. Padding is charged at the
	// widest of the three styles so the budget holds for all of them.
	fixed := lipgloss.Width(fmt.Sprintf("   %s %s%s%s  %s", status, multiProject, hasNotes, taskIndicator, idText))
	if moving {
		fixed += lipgloss.Width("▶  ◀ MOVING")
	}
	title := truncate(item.Title, max(width-fixed, 1))

	var content string
	if item.Completed {
		content = itemCompletedStyle.Render(
			fmt.Sprintf("%s %s%s%s%s  %s", status, title, multiProject, hasNotes, taskIndicator, idText),
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
		content = fmt.Sprintf("%s %s%s%s%s  %s", status, title, mp, notes, tasks, id)
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
		hints := "[a]dd project [Enter]select [m]ove [Tab]items [T]ree [/]search"
		if m.filter != filterNone {
			hints += " [0]reset filter"
		}
		return hints
	}
	// Hints depend on whether the cursor is on an item or a task row.
	if row := m.currentRow(); row != nil && row.kind == rowTask {
		return "[space]toggle [d]elete [t]ask [J/K]next/prev item [T]ree [/]search"
	}
	hints := "[Enter]detail [space]done [T]ree [a]dd [x]archive [e]dit [n]otes [t]ask [J/K]item [m]ove [b]lock [B]unblock [/]search"
	if m.filter != filterNone {
		hints += " [0]reset"
	}
	return hints
}

// clipHints narrows a hint line to fit, dropping whole hints rather than
// cutting one in half: a trailing "[T]re…" reads as a rendering fault, where a
// dropped hint reads as a list that continues. Hints are split on the bracket
// that opens each one, since several carry a space of their own.
func clipHints(hints string, maxWidth int) string {
	if lipgloss.Width(hints) <= maxWidth {
		return hints
	}
	if maxWidth < 4 {
		return ""
	}
	parts := strings.Split(hints, " [")
	kept, width := []string{}, 0
	for i, part := range parts {
		if i > 0 {
			part = "[" + part
		}
		next := width + lipgloss.Width(part)
		if i > 0 {
			next++
		}
		if next+2 > maxWidth {
			break
		}
		width, kept = next, append(kept, part)
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, " ") + " …"
}

func (m *App) renderStatusBar() string {
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

	// [?]help sits on the right, with the things that are always true, rather
	// than at the end of the contextual list where clipping reaches it first.
	// It is the one key that leads to every other key, so it is the last one
	// that should disappear on a narrow window.
	modeStr = "[?]help  " + modeStr
	modeWidth := lipgloss.Width(modeStr)

	var left string
	switch {
	case m.errorMsg != "":
		left = errorMsgStyle.Render("Error: " + m.errorMsg)
	case m.loading:
		left = dimStyle.Render("Loading...")
	case m.statusMsg != "":
		left = statusMsgStyle.Render(m.statusMsg)
	default:
		// The hint line is the one thing here that outgrows the window — it
		// reached 144 columns, so every key past the middle was off screen and
		// the bar wrapped into a second row that pushed the panes up. Clipped
		// rather than wrapped, and truncated before styling so no escape
		// sequence is cut in half. The 5 is the two spaces this function adds,
		// the style's own left and right padding, and the one column of gap
		// that always separates the hints from the sync status.
		left = clipHints(m.statusBarHints(), m.width-modeWidth-5)
	}

	leftWidth := lipgloss.Width(left)
	padding := m.width - leftWidth - modeWidth - 4
	if padding < 1 {
		padding = 1
	}

	bar := fmt.Sprintf(" %s%s%s ", left, strings.Repeat(" ", padding), modeStr)
	return statusBarStyle.Render(bar)
}
