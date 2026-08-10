package sync_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	goSync "sync"
	"testing"
	"time"

	"github.com/datapointchris/todoui/v2/backend"
	"github.com/datapointchris/todoui/v2/db"
	"github.com/datapointchris/todoui/v2/model"
	"github.com/datapointchris/todoui/v2/sync"
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

// A pull takes seconds — 2+2N requests — and the user keeps working through it.
// Anything created in that window postdates the server response, so the
// reconciliation must not read it as "deleted upstream" and erase it, nor drop
// the queued operation that is the only record of it.
func TestPull_KeepsWorkDoneWhileItWasRunning(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	local := backend.NewLocalBackend(database)

	// The mid-pull create needs a project to attach to, and the server has to
	// report that same project or the pull deletes it and cascades the item away.
	project, err := local.CreateProject(model.CreateProject{Name: "Server"})
	if err != nil {
		t.Fatalf("seeding project: %v", err)
	}

	var sb *sync.SyncBackend
	var once goSync.Once
	raced := make(chan string, 1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every write fails, so the op queued below is guaranteed to still be
		// pending when the assertion runs.
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/":
			_ = json.NewEncoder(w).Encode([]model.Project{
				{ID: project.ID, Name: "Server", Position: 0, CreatedAt: time.Now()},
			})
		case "/project-items/":
			_ = json.NewEncoder(w).Encode([]model.ProjectItem{
				{ID: "item-1", Title: "Server Item", CreatedAt: time.Now(), UpdatedAt: time.Now()},
			})
		case "/project-items/item-1/":
			// Mid-pull: the user adds an item the server response cannot know about.
			once.Do(func() {
				created, err := sb.CreateItem(model.CreateProjectItem{
					Title:      "Typed During Pull",
					ProjectIDs: []string{project.ID},
				})
				if err != nil {
					t.Errorf("creating item mid-pull: %v", err)
					return
				}
				raced <- created.ID
			})
			_ = json.NewEncoder(w).Encode(model.ProjectItemDetail{
				ProjectItem: model.ProjectItem{ID: "item-1", Title: "Server Item", CreatedAt: time.Now(), UpdatedAt: time.Now()},
				Projects:    []model.Project{{ID: project.ID, Name: "Server"}},
			})
		default:
			_ = json.NewEncoder(w).Encode([]model.ProjectItemTask{})
		}
	})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	engine := sync.New(database, ts.URL, "")
	t.Cleanup(engine.Stop)
	sb = sync.NewSyncBackend(local, engine)

	if err := engine.Pull(t.Context()); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	racedID := <-raced
	if _, err := local.GetItem(racedID); err != nil {
		t.Fatalf("item created during the pull was erased by it: %v", err)
	}
	if got := engine.Status().PendingCount; got == 0 {
		t.Error("the queued create was dropped, so nothing will ever push it")
	}
}

// Notify only fires on a local mutation. Without a retry ticker a push that
// failed while the API was down stayed queued until the user happened to edit
// something else, which is indistinguishable from sync being broken.
func TestPush_RetriesWithoutAFurtherMutation(t *testing.T) {
	var mu goSync.Mutex
	failing := true
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		down := failing
		mu.Unlock()
		if down {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	engine := sync.New(database, ts.URL, "", sync.WithPushRetryInterval(100*time.Millisecond))
	engine.Start()
	t.Cleanup(engine.Stop)
	sb := sync.NewSyncBackend(backend.NewLocalBackend(database), engine)

	if _, err := sb.CreateProject(model.CreateProject{Name: "Queued"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// The first push fails and backs off; the op must still be waiting.
	time.Sleep(500 * time.Millisecond)
	if engine.Status().PendingCount == 0 {
		t.Fatal("op vanished while the API was failing")
	}

	mu.Lock()
	failing = false
	mu.Unlock()

	// No Notify, no further mutation — only the ticker can clear this.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if engine.Status().PendingCount == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("queue never healed on its own after the API recovered")
}

// pullFixture serves a two-item dataset. When embedded is true the list carries
// dependency_ids and tasks the way the current API does; when false it omits
// them the way a server predating the embed does, which is what forces the
// per-item fallback.
func pullFixture(embedded bool) http.HandlerFunc {
	now := time.Now()
	base := func(id, title string) map[string]any {
		return map[string]any{
			"id": id, "title": title, "notes": nil, "repo": nil,
			"completed": false, "archived": false,
			"created_at": now.Format(time.RFC3339Nano),
			"updated_at": now.Format(time.RFC3339Nano),
			"projects":   []map[string]any{{"id": "proj-1", "name": "Remote", "position": 0, "created_at": now.Format(time.RFC3339Nano)}},
		}
	}
	task := map[string]any{
		"id": "task-1", "item_id": "item-2", "title": "Embedded Task",
		"completed": false, "position": 0, "created_at": now.Format(time.RFC3339Nano),
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/":
			_ = json.NewEncoder(w).Encode([]model.Project{{ID: "proj-1", Name: "Remote", Position: 0, CreatedAt: now}})
		case "/project-items/":
			blocker := base("item-1", "Blocker")
			blocked := base("item-2", "Blocked")
			if embedded {
				blocker["dependency_ids"] = []string{}
				blocker["tasks"] = []map[string]any{}
				blocked["dependency_ids"] = []string{"item-1"}
				blocked["tasks"] = []map[string]any{task}
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{blocker, blocked})
		case "/project-items/item-1/":
			_ = json.NewEncoder(w).Encode(base("item-1", "Blocker"))
		case "/project-items/item-2/":
			detail := base("item-2", "Blocked")
			detail["dependency_ids"] = []string{"item-1"}
			_ = json.NewEncoder(w).Encode(detail)
		case "/project-items/item-2/tasks/":
			_ = json.NewEncoder(w).Encode([]map[string]any{task})
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}
	}
}

func pulledInto(t *testing.T, handler http.HandlerFunc) (*callRecorder, *backend.LocalBackend) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	recorder := &callRecorder{}
	ts := httptest.NewServer(logging(recorder, handler))
	t.Cleanup(ts.Close)

	engine := sync.New(database, ts.URL, "")
	t.Cleanup(engine.Stop)
	if err := engine.Pull(t.Context()); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	return recorder, backend.NewLocalBackend(database)
}

// The whole point of embedding: a reconcile is two requests regardless of how
// many items exist, instead of two more per item.
func TestPull_UsesEmbeddedDetailAndStopsAtTwoRequests(t *testing.T) {
	recorder, local := pulledInto(t, pullFixture(true))

	if got := recorder.snapshot(); len(got) != 2 {
		t.Errorf("expected 2 requests, got %d: %v", len(got), got)
	}

	tasks, err := local.ListTasks("item-2")
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Embedded Task" {
		t.Errorf("embedded task did not reconcile: %v", tasks)
	}
	blockers, err := local.GetBlockers("item-2")
	if err != nil {
		t.Fatalf("GetBlockers: %v", err)
	}
	if len(blockers) != 1 || blockers[0].ID != "item-1" {
		t.Errorf("embedded dependency did not reconcile: %v", blockers)
	}
}

// Deploy order between todoui and ichrisbirch must not matter. Against a server
// that omits the fields, treating absent as empty would delete every task and
// dependency locally, so the pull has to fall back to the per-item endpoints.
func TestPull_FallsBackWhenTheServerOmitsEmbeddedFields(t *testing.T) {
	recorder, local := pulledInto(t, pullFixture(false))

	if got := recorder.count(); got <= 2 {
		t.Errorf("expected per-item fallback requests, got only %d", got)
	}

	tasks, err := local.ListTasks("item-2")
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Embedded Task" {
		t.Errorf("fallback lost the item's tasks: %v", tasks)
	}
	blockers, err := local.GetBlockers("item-2")
	if err != nil {
		t.Fatalf("GetBlockers: %v", err)
	}
	if len(blockers) != 1 || blockers[0].ID != "item-1" {
		t.Errorf("fallback lost the item's dependencies: %v", blockers)
	}
}

// The number is what a caller prints and types, and the create response is the
// first place it exists. Waiting for the next pull would leave the item unnamed
// for up to a full sync interval after the command that made it returned.
func TestPush_RecordsTheNumberTheServerAssigned(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/project-items/" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number": 231}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	sb, engine := setupSync(t, handler)

	p, _ := sb.CreateProject(model.CreateProject{Name: "Numbered"})
	item, err := sb.CreateItem(model.CreateProjectItem{Title: "Earns a number", ProjectIDs: []string{p.ID}})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if item.Number != nil {
		t.Fatalf("Number = %d before the push, want nil", *item.Number)
	}

	engine.Flush()

	pushed, err := sb.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if pushed.Number == nil {
		t.Fatal("Number is still nil after the push — the create response was discarded")
	}
	if *pushed.Number != 231 {
		t.Errorf("Number = %d, want 231", *pushed.Number)
	}
}

// A create that reached the server is applied whether or not its response
// decodes. Failing here would re-queue it and file the item twice.
func TestPush_AnUndecodableCreateResponseIsNotAFailure(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/project-items/" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("not json"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	sb, engine := setupSync(t, handler)

	p, _ := sb.CreateProject(model.CreateProject{Name: "Garbled"})
	item, err := sb.CreateItem(model.CreateProjectItem{Title: "Still filed once", ProjectIDs: []string{p.ID}})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	engine.Flush()

	if pending := engine.Status().PendingCount; pending != 0 {
		t.Errorf("pending ops = %d, want 0 — the create landed and must not be retried", pending)
	}
	stored, err := sb.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if stored.Number != nil {
		t.Errorf("Number = %d, want nil — the next pull supplies it", *stored.Number)
	}
}

// Two drains overlapping pushed the same queued op twice — a 201 followed by a
// 409, and a server-side item number burned on the insert that lost the race.
// Notify wakes the push loop on the same mutation a caller then flushes for, so
// the overlap happens on an ordinary create rather than under contrived load.
func TestPush_AQueuedCreateIsPushedExactlyOnce(t *testing.T) {
	var mu goSync.Mutex
	creates := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/project-items/" {
			mu.Lock()
			creates++
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number": 9}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	sb, engine := setupSync(t, handler)

	p, _ := sb.CreateProject(model.CreateProject{Name: "Once"})
	if _, err := sb.CreateItem(model.CreateProjectItem{Title: "Pushed once", ProjectIDs: []string{p.ID}}); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	// The create already woke the push loop; flushing races a drain that is
	// very likely still in flight.
	engine.Flush()
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if creates != 1 {
		t.Errorf("POST /project-items/ called %d times, want exactly 1", creates)
	}
}

// The server assembles /projects/ and each item's embedded Projects list
// separately, and nothing makes the two agree — the real API returned items
// belonging to projects the projects endpoint omitted. With foreign keys off
// that wrote an orphan membership, and 46 accumulated until the projects
// migration refused to run past them. With foreign keys on it would fail the
// pull outright, which would be worse: one inconsistent link would cost every
// other change in the reconcile. The link is dropped and the pull carries on.
func TestPull_DropsLinksToEntitiesTheServerDidNotSend(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/":
			_ = json.NewEncoder(w).Encode([]model.Project{
				{ID: "proj-1", Name: "Real", Position: 0, CreatedAt: time.Now()},
			})
		case "/project-items/":
			_ = json.NewEncoder(w).Encode([]model.ProjectItemDetail{{
				ProjectItem: model.ProjectItem{ID: "item-1", Title: "Kept", CreatedAt: time.Now(), UpdatedAt: time.Now()},
				Projects: []model.Project{
					{ID: "proj-1", Name: "Real"},
					{ID: "proj-missing", Name: "Never sent by /projects/"},
				},
				DependencyIDs: []string{"item-missing"},
				Tasks: []model.ProjectItemTask{
					{ID: "task-1", ItemID: "item-1", Title: "Kept task", CreatedAt: time.Now()},
				},
			}})
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
	if err := engine.Pull(t.Context()); err != nil {
		t.Fatalf("one unlinkable membership failed the whole pull: %v", err)
	}

	local := backend.NewLocalBackend(database)
	projects, err := local.GetItemProjects("item-1")
	if err != nil {
		t.Fatalf("GetItemProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "proj-1" {
		t.Errorf("expected only the membership that has a project, got %v", projects)
	}

	var violations int
	if err := database.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil {
		t.Fatalf("checking foreign keys: %v", err)
	}
	if violations != 0 {
		t.Errorf("the pull left %d dangling rows behind", violations)
	}

	tasks, err := local.ListTasks("item-1")
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("the good task went with the bad link: %v", tasks)
	}
}

// idx_items_number is unique and the upsert writes row by row, so two items
// swapping numbers upstream means one of them briefly needs a number the other
// still holds. Writing the numbers after every row has been cleared makes the
// pass order-independent — otherwise any renumbering upstream fails the entire
// pull and todoui never syncs again.
func TestPull_SurvivesItemsSwappingNumbers(t *testing.T) {
	numbers := map[string]int{"item-a": 1, "item-b": 2}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/project-items/" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
			return
		}
		out := make([]model.ProjectItemDetail, 0, 2)
		for _, id := range []string{"item-a", "item-b"} {
			n := numbers[id]
			out = append(out, model.ProjectItemDetail{
				ProjectItem: model.ProjectItem{
					ID: id, Number: &n, Title: id,
					CreatedAt: time.Now(), UpdatedAt: time.Now(),
				},
				DependencyIDs: []string{},
				Tasks:         []model.ProjectItemTask{},
			})
		}
		_ = json.NewEncoder(w).Encode(out)
	})

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	defer func() { _ = database.Close() }()

	ts := httptest.NewServer(handler)
	defer ts.Close()

	engine := sync.New(database, ts.URL, "")
	if err := engine.Pull(t.Context()); err != nil {
		t.Fatalf("first pull: %v", err)
	}

	numbers["item-a"], numbers["item-b"] = 2, 1
	if err := engine.Pull(t.Context()); err != nil {
		t.Fatalf("a swap upstream failed the pull: %v", err)
	}

	local := backend.NewLocalBackend(database)
	item, err := local.GetItem("item-a")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if item.Number == nil || *item.Number != 2 {
		t.Errorf("item-a number = %v, want 2", item.Number)
	}
}
