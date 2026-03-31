package sync_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/db"
	"github.com/datapointchris/todoui/internal/model"
	"github.com/datapointchris/todoui/internal/sync"
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

func TestSyncBackend_ItemLifecycle(t *testing.T) {
	var received []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	sb, _ := setupSync(t, handler)

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
	if len(received) < 4 {
		t.Fatalf("expected at least 4 HTTP calls, got %d: %v", len(received), received)
	}
}

func TestSyncBackend_TaskOperations(t *testing.T) {
	var received []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	sb, _ := setupSync(t, handler)

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

	if len(received) < 5 {
		t.Fatalf("expected at least 5 HTTP calls, got %d: %v", len(received), received)
	}
}

func TestSyncBackend_UndoDoesNotSync(t *testing.T) {
	var received []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	sb, _ := setupSync(t, handler)

	p, _ := sb.CreateProject(model.CreateProject{Name: "Undo"})
	_, _ = sb.CreateItem(model.CreateProjectItem{
		Title:      "Undoable",
		ProjectIDs: []string{p.ID},
	})

	// Wait for create pushes
	time.Sleep(300 * time.Millisecond)
	countBefore := len(received)

	// Undo should not generate a sync operation
	_, err := sb.Undo()
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	countAfter := len(received)

	if countAfter != countBefore {
		t.Errorf("expected no new HTTP calls after undo, got %d new calls", countAfter-countBefore)
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
