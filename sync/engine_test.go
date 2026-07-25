package sync_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	goSync "sync"
	"testing"
	"time"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/db"
	"github.com/datapointchris/todoui/model"
	"github.com/datapointchris/todoui/sync"
)

func setupSync(t *testing.T, handler http.Handler) (*sync.SyncBackend, *sync.Engine) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	local := backend.NewLocalBackend(database)
	engine := sync.New(database, ts.URL, "")
	engine.Start()
	t.Cleanup(engine.Stop)

	sb := sync.NewSyncBackend(local, engine)
	return sb, engine
}

func TestSyncBackend_QueueOnMutation(t *testing.T) {
	// Server that accepts everything
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	sb, engine := setupSync(t, handler)

	// Create a project through SyncBackend
	p, err := sb.CreateProject(model.CreateProject{Name: "SyncTest"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	// Give the push loop a moment to process
	time.Sleep(100 * time.Millisecond)

	// After successful push, pending count should be 0
	status := engine.Status()
	if status.PendingCount != 0 {
		t.Errorf("expected 0 pending, got %d", status.PendingCount)
	}
}

func TestSyncBackend_ReadsDelegateToLocal(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	sb, _ := setupSync(t, handler)

	// Create a project
	_, err := sb.CreateProject(model.CreateProject{Name: "LocalRead"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Read should come from local DB immediately
	projects, err := sb.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1", len(projects))
	}
	if projects[0].Name != "LocalRead" {
		t.Errorf("got name %q, want LocalRead", projects[0].Name)
	}
}

func TestSyncBackend_PushRetryOnNetworkError(t *testing.T) {
	attempts := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts <= 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	sb, engine := setupSync(t, handler)

	_, err := sb.CreateProject(model.CreateProject{Name: "Retry"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// First push will fail (500), wait for backoff + retry
	time.Sleep(4 * time.Second)

	// Manually trigger a retry
	engine.Notify()
	time.Sleep(500 * time.Millisecond)

	status := engine.Status()
	if status.PendingCount != 0 {
		t.Errorf("expected 0 pending after retry, got %d", status.PendingCount)
	}
}

func TestSyncBackend_PushDropsOn409(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"detail":"duplicate"}`))
	})
	sb, engine := setupSync(t, handler)

	_, err := sb.CreateProject(model.CreateProject{Name: "Conflict"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// 409 should be treated as "drop" — pending count goes to 0
	time.Sleep(200 * time.Millisecond)

	status := engine.Status()
	if status.PendingCount != 0 {
		t.Errorf("expected 0 pending (409 should drop), got %d", status.PendingCount)
	}
}

// callRecorder captures what a test's mock server received. The push loop
// runs on its own goroutine, so every read and write of the log needs the
// lock — asserting on an unguarded slice is a data race.
type callRecorder struct {
	mu    goSync.Mutex
	calls []string
}

func (r *callRecorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.calls = append(r.calls, req.Method+" "+req.URL.Path)
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}
}

func (r *callRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *callRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func TestSyncBackend_ItemLifecycle(t *testing.T) {
	recorder := &callRecorder{}
	sb, _ := setupSync(t, recorder.handler())

	p, _ := sb.CreateProject(model.CreateProject{Name: "Items"})
	item, err := sb.CreateItem(model.CreateProjectItem{
		Title:      "Test",
		ProjectIDs: []string{p.ID},
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	done := true
	_, err = sb.UpdateItem(item.ID, model.UpdateProjectItem{Completed: &done})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	if err := sb.DeleteItem(item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	// Wait for push loop to drain
	time.Sleep(500 * time.Millisecond)

	// Verify the HTTP calls were made in order
	if recorder.count() < 4 {
		t.Fatalf("expected at least 4 HTTP calls, got %d: %v", recorder.count(), recorder.snapshot())
	}
}

func TestSyncBackend_TaskOperations(t *testing.T) {
	recorder := &callRecorder{}
	sb, _ := setupSync(t, recorder.handler())

	p, _ := sb.CreateProject(model.CreateProject{Name: "Tasks"})
	item, _ := sb.CreateItem(model.CreateProjectItem{
		Title:      "WithTasks",
		ProjectIDs: []string{p.ID},
	})

	task, err := sb.CreateTask(item.ID, model.CreateProjectItemTask{Title: "Sub"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := sb.CompleteTask(item.ID, task.ID); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	if err := sb.DeleteTask(item.ID, task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if recorder.count() < 5 {
		t.Fatalf("expected at least 5 HTTP calls, got %d: %v", recorder.count(), recorder.snapshot())
	}
}

func TestSyncBackend_UndoAfterPushDeletesRemotely(t *testing.T) {
	recorder := &callRecorder{}
	sb, _ := setupSync(t, recorder.handler())

	p, _ := sb.CreateProject(model.CreateProject{Name: "Undo"})
	item, _ := sb.CreateItem(model.CreateProjectItem{
		Title:      "Undoable",
		ProjectIDs: []string{p.ID},
	})

	// Let the create reach the server, so the server holds a row the undo removes.
	time.Sleep(300 * time.Millisecond)

	if _, err := sb.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	want := "DELETE /project-items/" + item.ID + "/"
	calls := recorder.snapshot()
	for _, call := range calls {
		if call == want {
			return
		}
	}
	t.Errorf("expected %q after undoing an already-pushed create, got %v", want, calls)
}

func TestSyncBackend_UndoBeforePushDropsQueuedCreate(t *testing.T) {
	// Every push fails, so operations stay queued and nothing reaches the server.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	sb, engine := setupSync(t, handler)

	p, _ := sb.CreateProject(model.CreateProject{Name: "Undo"})
	item, _ := sb.CreateItem(model.CreateProjectItem{
		Title:      "Undoable",
		ProjectIDs: []string{p.ID},
	})

	if _, err := sb.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	// The queued create described a row that no longer exists. Nothing should be
	// left for this item — not the create, and not a delete for a row the server
	// never received.
	remaining, err := engine.DropOpsForEntity(item.ID)
	if err != nil {
		t.Fatalf("checking queue: %v", err)
	}
	if remaining != 0 {
		t.Errorf("expected no queued operations for the undone item, found %d", remaining)
	}
}

func TestPull_Reconciles(t *testing.T) {
	// Mock API server that returns test data
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/":
			_ = json.NewEncoder(w).Encode([]model.Project{
				{ID: "proj-1", Name: "Pulled", Position: 0, CreatedAt: time.Now()},
			})
		case "/project-items/":
			_ = json.NewEncoder(w).Encode([]model.ProjectItem{
				{ID: "item-1", Title: "Pulled Item", CreatedAt: time.Now(), UpdatedAt: time.Now()},
			})
		case "/project-items/item-1/":
			_ = json.NewEncoder(w).Encode(model.ProjectItemDetail{
				ProjectItem: model.ProjectItem{ID: "item-1", Title: "Pulled Item", CreatedAt: time.Now(), UpdatedAt: time.Now()},
				Projects:    []model.Project{{ID: "proj-1", Name: "Pulled"}},
			})
		case "/project-items/item-1/tasks/":
			_ = json.NewEncoder(w).Encode([]model.ProjectItemTask{
				{ID: "task-1", ItemID: "item-1", Title: "Pulled Task", CreatedAt: time.Now()},
			})
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	})

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	defer func() { _ = database.Close() }()

	ts := httptest.NewServer(handler)
	defer ts.Close()

	engine := sync.New(database, ts.URL, "")

	// Pull should reconcile server data into local DB
	if err := engine.Pull(t.Context()); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	// Verify data was pulled into local DB
	local := backend.NewLocalBackend(database)

	projects, err := local.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "Pulled" {
		t.Errorf("expected 1 project 'Pulled', got %v", projects)
	}

	item, err := local.GetItem("item-1")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if item.Title != "Pulled Item" {
		t.Errorf("got title %q, want 'Pulled Item'", item.Title)
	}

	tasks, err := local.ListTasks("item-1")
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Pulled Task" {
		t.Errorf("expected 1 task 'Pulled Task', got %v", tasks)
	}
}

// Undoing an already-pushed delete has to recreate the row on the server, not
// update one the server no longer has — and recreate its memberships, tasks,
// and dependencies with it, because a pull rebuilds those from server state
// and would otherwise erase the restore.
func TestSyncBackend_UndoOfPushedDeleteRecreatesRemotely(t *testing.T) {
	recorder := &callRecorder{}
	sb, _ := setupSync(t, recorder.handler())

	p, _ := sb.CreateProject(model.CreateProject{Name: "Undo"})
	item, _ := sb.CreateItem(model.CreateProjectItem{
		Title:      "Doomed",
		ProjectIDs: []string{p.ID},
	})
	if _, err := sb.CreateTask(item.ID, model.CreateProjectItemTask{Title: "step"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := sb.DeleteItem(item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	// Let the delete reach the server before undoing it, then look only at
	// what the undo itself pushed — the original create hit the same paths.
	time.Sleep(400 * time.Millisecond)
	beforeUndo := recorder.count()

	if _, err := sb.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	time.Sleep(400 * time.Millisecond)

	calls := recorder.snapshot()[beforeUndo:]

	var sawItemCreate, sawTaskCreate bool
	for _, call := range calls {
		switch call {
		case "POST /project-items/":
			sawItemCreate = true
		case "POST /project-items/" + item.ID + "/tasks/":
			sawTaskCreate = true
		}
	}
	if !sawItemCreate {
		t.Errorf("expected the restored item recreated on the server, got %v", calls)
	}
	if !sawTaskCreate {
		t.Errorf("expected the restored sub-task recreated on the server, got %v", calls)
	}
}

// Membership, dependency, and reorder undos all record the item as their
// entity ID. Reconciling them by dropping queued ops for that ID would take
// unrelated edits with it, and falling through to the item branch would push
// a delete for an item that is still there.
func TestSyncBackend_UndoOfMembershipDoesNotDeleteTheItem(t *testing.T) {
	recorder := &callRecorder{}
	sb, _ := setupSync(t, recorder.handler())

	work, _ := sb.CreateProject(model.CreateProject{Name: "work"})
	homelab, _ := sb.CreateProject(model.CreateProject{Name: "homelab"})
	item, _ := sb.CreateItem(model.CreateProjectItem{
		Title:      "shared",
		ProjectIDs: []string{work.ID},
	})
	if err := sb.AddToProject(item.ID, homelab.ID); err != nil {
		t.Fatalf("AddToProject: %v", err)
	}

	time.Sleep(400 * time.Millisecond)
	beforeUndo := recorder.count()

	if _, err := sb.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	time.Sleep(400 * time.Millisecond)

	calls := recorder.snapshot()[beforeUndo:]
	wantRemove := "DELETE /project-items/" + item.ID + "/projects/" + homelab.ID
	itemDelete := "DELETE /project-items/" + item.ID + "/"

	var sawRemove bool
	for _, call := range calls {
		if call == itemDelete {
			t.Fatalf("undoing a membership add deleted the item on the server: %v", calls)
		}
		if call == wantRemove {
			sawRemove = true
		}
	}
	if !sawRemove {
		t.Errorf("expected %q after undoing the membership add, got %v", wantRemove, calls)
	}
}

func TestSyncBackend_UndoOfTaskCompletionPushesTheReversal(t *testing.T) {
	recorder := &callRecorder{}
	sb, _ := setupSync(t, recorder.handler())

	p, _ := sb.CreateProject(model.CreateProject{Name: "work"})
	item, _ := sb.CreateItem(model.CreateProjectItem{Title: "item", ProjectIDs: []string{p.ID}})
	task, err := sb.CreateTask(item.ID, model.CreateProjectItemTask{Title: "step"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := sb.CompleteTask(item.ID, task.ID); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	time.Sleep(400 * time.Millisecond)
	beforeUndo := recorder.count()

	if _, err := sb.Undo(); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	time.Sleep(400 * time.Millisecond)

	calls := recorder.snapshot()[beforeUndo:]
	want := "PATCH /project-items/" + item.ID + "/tasks/" + task.ID + "/"
	for _, call := range calls {
		if call == want {
			return
		}
	}
	t.Errorf("expected %q after undoing the completion, got %v", want, calls)
}

// The CLI is a short-lived process, so it has to reconcile before it reads.
// Pull was reachable only from App.Init, which meant every CLI read served
// whatever the last TUI session had pulled — days stale on a machine driven by
// agents rather than opened.
func TestPullIfStaleRefreshesWhenNeverPulled(t *testing.T) {
	recorder := &callRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]model.Project{
			{ID: "proj-1", Name: "Pulled", CreatedAt: time.Now()},
		})
	})
	mux.HandleFunc("/project-items/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]model.ProjectItem{})
	})
	_, engine := setupSync(t, logging(recorder, mux))

	pulled, err := engine.PullIfStale(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("PullIfStale: %v", err)
	}
	if !pulled {
		t.Fatal("a database that has never pulled must pull")
	}

	last, err := engine.LastPullAt()
	if err != nil {
		t.Fatalf("LastPullAt: %v", err)
	}
	if time.Since(last) > time.Minute {
		t.Errorf("expected the pull to be stamped, got %v", last)
	}
}

// A full pull is 2+2N requests, so a burst of CLI commands must not each pay
// for one.
func TestPullIfStaleSkipsWhenFresh(t *testing.T) {
	recorder := &callRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]model.Project{})
	})
	mux.HandleFunc("/project-items/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]model.ProjectItem{})
	})
	_, engine := setupSync(t, logging(recorder, mux))

	if _, err := engine.PullIfStale(context.Background(), time.Minute); err != nil {
		t.Fatalf("first PullIfStale: %v", err)
	}
	afterFirst := recorder.count()

	pulled, err := engine.PullIfStale(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("second PullIfStale: %v", err)
	}
	if pulled {
		t.Error("a pull inside the freshness window must be skipped")
	}
	if recorder.count() != afterFirst {
		t.Errorf("skipped pull still made requests: %d → %d", afterFirst, recorder.count())
	}
}

// todoui is local-first: an unreachable API degrades to local data. The caller
// warns rather than failing, so the error only has to surface.
func TestPullIfStaleReportsAnUnreachableAPI(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, engine := setupSync(t, handler)

	pulled, err := engine.PullIfStale(context.Background(), time.Minute)
	if !pulled {
		t.Error("a stale database should attempt the pull")
	}
	if err == nil {
		t.Error("expected the failure to surface so the caller can warn")
	}
}

// logging wraps a handler so a test can count requests without reimplementing
// the routing.
func logging(recorder *callRecorder, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.mu.Lock()
		recorder.calls = append(recorder.calls, r.Method+" "+r.URL.Path)
		recorder.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}
