package tui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	goSync "sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/db"
	"github.com/datapointchris/todoui/model"
	"github.com/datapointchris/todoui/sync"
)

// newTestApp returns an app already showing one project's items, sized to
// width x height. Commands are drained synchronously so the model reaches
// the same state the event loop would put it in.
func newTestApp(t *testing.T, width, height int, itemTitles ...string) *App {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	// AssigningItemNumbers because the harness passes a nil sync engine, which
	// is the sync-off case: nothing upstream will number these items.
	b := backend.NewLocalBackend(database, backend.AssigningItemNumbers())
	project, err := b.CreateProject(model.CreateProject{Name: "work"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	for _, title := range itemTitles {
		if _, err := b.CreateItem(model.CreateProjectItem{
			Title:      title,
			ProjectIDs: []string{project.ID},
		}); err != nil {
			t.Fatalf("creating item %q: %v", title, err)
		}
	}

	app := NewApp(b, nil, ":memory:", 2*time.Minute)
	send(t, app, tea.WindowSizeMsg{Width: width, Height: height})
	send(t, app, app.Init()())
	send(t, app, app.fetchItems()())
	app.activePane = itemPane
	return app
}

// send delivers msg to the app and follows the resulting command chain to
// exhaustion, so a create that triggers a refresh has landed before the
// next assertion. Mirrors what the event loop does.
func send(t *testing.T, app *App, msg tea.Msg) {
	t.Helper()
	if msg == nil {
		return
	}
	_, cmd := app.Update(msg)
	drain(t, app, cmd, 16)
}

func drain(t *testing.T, app *App, cmd tea.Cmd, depth int) {
	t.Helper()
	if cmd == nil || depth == 0 {
		return
	}
	msg := runCmd(cmd)
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drain(t, app, c, depth-1)
		}
		return
	}
	_, next := app.Update(msg)
	drain(t, app, next, depth-1)
}

// runCmd abandons a command that does not produce a message promptly —
// flash and sync-status commands are tea.Tick timers that would otherwise
// block the test for their full duration.
func runCmd(cmd tea.Cmd) tea.Msg {
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case msg := <-result:
		return msg
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

func pressKey(t *testing.T, app *App, key string) {
	t.Helper()
	send(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

// The add-task prompt used to render as a trailer line after the item's
// last row, which syncScroll never reserved room for — on a short window
// the input was dropped while still swallowing every keystroke. It is an
// overlay now, so it must survive a window too short to show the list.
func TestAddTaskPromptVisibleInShortWindow(t *testing.T) {
	app := newTestApp(t, 80, 6, "first", "second", "third")

	pressKey(t, app, "t")

	if app.appMode != modeAddTask {
		t.Fatalf("expected modeAddTask after 't', got %v", app.appMode)
	}
	if !strings.Contains(app.View(), "New task for") {
		t.Errorf("add-task prompt not rendered in a 6-line window:\n%s", app.View())
	}
}

func TestAddTaskPromptNamesTargetItem(t *testing.T) {
	app := newTestApp(t, 80, 24, "ship the thing")

	pressKey(t, app, "t")

	if got := app.addTaskItemTitle; got != "ship the thing" {
		t.Errorf("expected target title %q, got %q", "ship the thing", got)
	}
	if view := app.View(); !strings.Contains(view, "ship the thing") {
		t.Errorf("prompt does not name the target item:\n%s", view)
	}
}

// Escaping must return to normal navigation rather than leaving keystrokes
// captured by an input the user cannot see.
func TestAddTaskEscapeRestoresNavigation(t *testing.T) {
	app := newTestApp(t, 80, 24, "first", "second")

	pressKey(t, app, "t")
	send(t, app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.appMode != modeNormal {
		t.Fatalf("expected modeNormal after esc, got %v", app.appMode)
	}

	before := app.rowCursor
	pressKey(t, app, "j")
	if app.rowCursor == before {
		t.Errorf("cursor did not move after esc; keys still captured by the input")
	}
}

func TestAddTaskCreatesTaskOnTargetItem(t *testing.T) {
	app := newTestApp(t, 80, 24, "first")

	pressKey(t, app, "t")
	for _, r := range "write tests" {
		send(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	if app.appMode != modeNormal {
		t.Fatalf("expected modeNormal after enter, got %v", app.appMode)
	}

	item := app.items[0]
	tasks, err := app.backend.ListTasks(item.ID)
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Title != "write tests" {
		t.Errorf("expected task title %q, got %q", "write tests", tasks[0].Title)
	}
}

// A task created from the list must appear as a row without a manual
// refresh — the reported symptom was the first task never showing up.
func TestCreatedTaskAppearsAsRow(t *testing.T) {
	app := newTestApp(t, 80, 24, "first")

	rowsBefore := len(app.rows)

	pressKey(t, app, "t")
	for _, r := range "subtask" {
		send(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	if len(app.rows) != rowsBefore+1 {
		t.Fatalf("expected a new task row, rows went %d → %d", rowsBefore, len(app.rows))
	}
	if app.rows[len(app.rows)-1].kind != rowTask {
		t.Errorf("expected the new row to be a task row")
	}
	if !strings.Contains(app.View(), "subtask") {
		t.Errorf("new task not rendered:\n%s", app.View())
	}
}

// The auto-pull timer re-arms itself from its own message. A tick that returns
// no command ends background sync for the rest of the session, so the skipped
// ticks have to re-arm too — otherwise opening one modal at the wrong moment
// silently reverts todoui to manual syncing.
func TestSyncTickRearmsEvenWhenItSkipsThePull(t *testing.T) {
	app := newTestApp(t, 80, 24, "Alpha")

	for _, mode := range []appMode{modeNormal, modeAddItem, modeMove, modeHelp} {
		app.appMode = mode
		_, cmd := app.Update(syncPullTickMsg{})
		if cmd == nil {
			t.Errorf("tick in mode %v returned no command: the sync timer is now dead", mode)
		}
	}
}

// safeToAutoPull gates the reconcile, not the timer. A pull rewrites items and
// ordering wholesale, which would move the ground under a grab or a text entry.
func TestAutoPullOnlyRunsInNormalMode(t *testing.T) {
	app := newTestApp(t, 80, 24, "Alpha")

	app.appMode = modeNormal
	if !app.safeToAutoPull() {
		t.Error("normal mode must allow the background reconcile")
	}
	for _, mode := range []appMode{modeAddItem, modeEditNotes, modeMove, modeMoveProject, modeItemDetail} {
		app.appMode = mode
		if app.safeToAutoPull() {
			t.Errorf("mode %v must defer the reconcile to the next tick", mode)
		}
	}
}

// An automatic pull lands every syncInterval. Announcing it would keep the
// status bar permanently flashing and bury the messages the user acted to see.
func TestAutomaticPullIsSilentAndManualPullIsNot(t *testing.T) {
	app := newTestApp(t, 80, 24, "Alpha")

	app.statusMsg = ""
	send(t, app, syncPullDoneMsg{manual: false})
	if app.statusMsg != "" {
		t.Errorf("automatic pull announced itself: %q", app.statusMsg)
	}

	send(t, app, syncPullDoneMsg{manual: true})
	if app.statusMsg == "" {
		t.Error("a pull the user asked for should confirm it happened")
	}
}

// A failing automatic pull is not something the user did. The status bar already
// reads SYNC ERR off the engine; pinning an error banner every interval would
// make a briefly unreachable API look like a broken app.
func TestAutomaticPullFailureDoesNotHijackTheStatusBar(t *testing.T) {
	app := newTestApp(t, 80, 24, "Alpha")

	send(t, app, syncPullErrMsg{error: errors.New("connection refused"), manual: false})
	if app.errorMsg != "" {
		t.Errorf("automatic pull failure took over the status bar: %q", app.errorMsg)
	}

	send(t, app, syncPullErrMsg{error: errors.New("connection refused"), manual: true})
	if app.errorMsg == "" {
		t.Error("a pull the user asked for must report why it failed")
	}
}

// End-to-end for the background timer: a tick in normal mode must actually
// reconcile with the server and re-arm, and a tick in a modal must do neither
// the pull nor lose the timer. This is the behavior that removes the need to
// drop out of the TUI and run `todoui sync` by hand.
func TestSyncTickReconcilesWithTheServer(t *testing.T) {
	var mu goSync.Mutex
	var hits int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/":
			_ = json.NewEncoder(w).Encode([]model.Project{
				{ID: "proj-1", Name: "Remote", Position: 0, CreatedAt: time.Now()},
			})
		case "/project-items/":
			_ = json.NewEncoder(w).Encode([]model.ProjectItem{
				{ID: "item-1", Title: "Added Elsewhere", CreatedAt: time.Now(), UpdatedAt: time.Now()},
			})
		case "/project-items/item-1/":
			_ = json.NewEncoder(w).Encode(model.ProjectItemDetail{
				ProjectItem: model.ProjectItem{ID: "item-1", Title: "Added Elsewhere", CreatedAt: time.Now(), UpdatedAt: time.Now()},
				Projects:    []model.Project{{ID: "proj-1", Name: "Remote"}},
			})
		default:
			_ = json.NewEncoder(w).Encode([]model.ProjectItemTask{})
		}
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	engine := sync.New(database, ts.URL, "")
	t.Cleanup(engine.Stop)
	app := NewApp(backend.NewLocalBackend(database), engine, ":memory:", 20*time.Millisecond)
	send(t, app, tea.WindowSizeMsg{Width: 80, Height: 24})

	countHits := func() int {
		mu.Lock()
		defer mu.Unlock()
		return hits
	}

	app.appMode = modeAddItem
	send(t, app, syncPullTickMsg{})
	if countHits() != 0 {
		t.Fatalf("a tick during text entry reconciled anyway: %d requests", countHits())
	}

	app.appMode = modeNormal
	send(t, app, syncPullTickMsg{})
	if countHits() == 0 {
		t.Fatal("a tick in normal mode never reached the server")
	}

	// An item that only ever existed on the server has to land in the TUI
	// without the user asking for it.
	if _, err := app.backend.GetItem("item-1"); err != nil {
		t.Fatalf("background pull did not bring the remote item local: %v", err)
	}
	found := false
	for _, p := range app.projects {
		if p.Name == "Remote" {
			found = true
		}
	}
	if !found {
		t.Errorf("pulled project never reached the rendered model: %v", app.projects)
	}
}

// The row is where the handle has to be legible: the id column was 8 characters
// of hex tail, which resolves but which nobody can read back off the screen.
func TestItemRowShowsTheNumber(t *testing.T) {
	app := newTestApp(t, 100, 30, "First item", "Second item")

	view := app.View()

	items, err := app.backend.ListAllItems()
	if err != nil {
		t.Fatalf("listing items: %v", err)
	}
	for _, item := range items {
		if item.Number == nil {
			t.Fatalf("item %q has no number in a sync-off database", item.Title)
		}
		if !strings.Contains(view, strconv.Itoa(*item.Number)) {
			t.Errorf("view is missing the handle %d for %q", *item.Number, item.Title)
		}
		if strings.Contains(view, shortID(item.ID)) {
			t.Errorf("view still shows the UUID tail %s for %q", shortID(item.ID), item.Title)
		}
	}
}

// --- Project status ---

func keys(t *testing.T, app *App, presses ...string) {
	t.Helper()
	for _, key := range presses {
		send(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	}
}

func TestCompletedProjectLeavesTheProjectPane(t *testing.T) {
	app := newTestApp(t, 100, 30)
	app.activePane = projectPane

	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	if app.appMode != modeProjectDetail {
		t.Fatalf("expected the project detail overlay, got mode %v", app.appMode)
	}
	keys(t, app, "c")

	if len(app.projects) != 0 {
		t.Errorf("completing a project must take it out of the pane, still have %d", len(app.projects))
	}
}

func TestShowClosedTogglesTheClosedProjectsBackIn(t *testing.T) {
	app := newTestApp(t, 100, 30)
	app.activePane = projectPane
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	keys(t, app, "c")
	send(t, app, tea.KeyMsg{Type: tea.KeyEsc})

	keys(t, app, "C")

	if len(app.projects) != 1 {
		t.Fatalf("C must bring closed projects back, got %d", len(app.projects))
	}
	if app.projects[0].Status != model.StatusDone {
		t.Errorf("status = %q, want done", app.projects[0].Status)
	}
	if !strings.Contains(app.View(), "done") {
		t.Errorf("the row must be labeled with its status:\n%s", app.View())
	}

	keys(t, app, "C")
	if len(app.projects) != 0 {
		t.Errorf("C again must hide them, got %d", len(app.projects))
	}
}

// A reason is required, so dropping is a prompt rather than a keypress.
func TestDroppingAProjectAsksWhy(t *testing.T) {
	app := newTestApp(t, 100, 30)
	app.activePane = projectPane
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	keys(t, app, "x")

	if app.appMode != modeDropProject {
		t.Fatalf("expected the drop prompt, got mode %v", app.appMode)
	}
	if len(app.projects) != 1 {
		t.Error("nothing may be dropped before the reason is given")
	}

	keys(t, app, "Go covers it")
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	if len(app.projects) != 0 {
		t.Fatalf("the drop did not land, still have %d projects", len(app.projects))
	}
	app.showClosed = true
	send(t, app, fetchProjectsCmd(app.backend, app.projectStatusFilter())())
	if got := app.projects[0].StatusReason; got == nil || *got != "Go covers it" {
		t.Errorf("status_reason = %v, want the typed reason", got)
	}
}

func TestCancellingTheDropPromptLeavesTheProjectAlone(t *testing.T) {
	app := newTestApp(t, 100, 30)
	app.activePane = projectPane
	send(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	keys(t, app, "x")

	send(t, app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.appMode != modeProjectDetail {
		t.Errorf("Esc must return to the overlay it came from, got mode %v", app.appMode)
	}
	if len(app.projects) != 1 {
		t.Error("canceling must not drop anything")
	}
}
