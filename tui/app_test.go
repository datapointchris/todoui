package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/db"
	"github.com/datapointchris/todoui/model"
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

	b := backend.NewLocalBackend(database)
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

	app := NewApp(b, nil, ":memory:")
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
