package backend_test

import (
	"net/http/httptest"
	"testing"

	"github.com/datapointchris/todoui/internal/api"
	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/db"
	"github.com/datapointchris/todoui/internal/model"
)

// setupRemote creates an in-memory SQLite DB, wraps it in a LocalBackend,
// starts an httptest server with the API, and returns a RemoteBackend
// pointed at that server.
func setupRemote(t *testing.T) *backend.RemoteBackend {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	local := backend.NewLocalBackend(database)
	srv := api.NewServer(local)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	return backend.NewRemoteBackend(ts.URL)
}

func TestRemote_ProjectCRUD(t *testing.T) {
	remote := setupRemote(t)

	// Create
	p, err := remote.CreateProject("TestProject")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.Name != "TestProject" {
		t.Errorf("got name %q, want TestProject", p.Name)
	}

	// List
	projects, err := remote.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1", len(projects))
	}
	if projects[0].Name != "TestProject" {
		t.Errorf("got name %q, want TestProject", projects[0].Name)
	}

	// Get
	got, err := remote.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "TestProject" {
		t.Errorf("got name %q, want TestProject", got.Name)
	}

	// Update
	newName := "Updated"
	updated, err := remote.UpdateProject(p.ID, model.UpdateProject{Name: &newName})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Name != "Updated" {
		t.Errorf("got name %q, want Updated", updated.Name)
	}

	// Delete
	if err := remote.DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	projects, err = remote.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects after delete: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("got %d projects after delete, want 0", len(projects))
	}
}

func TestRemote_ItemLifecycle(t *testing.T) {
	remote := setupRemote(t)

	// Setup: create a project
	p, err := remote.CreateProject("Project1")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Create item
	item, err := remote.CreateItem(model.CreateProjectItem{
		Title:      "Test Item",
		ProjectIDs: []int64{p.ID},
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if item.Title != "Test Item" {
		t.Errorf("got title %q, want Test Item", item.Title)
	}

	// List by project
	items, err := remote.ListItemsByProject(p.ID)
	if err != nil {
		t.Fatalf("ListItemsByProject: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}

	// Get item detail
	detail, err := remote.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if detail.Title != "Test Item" {
		t.Errorf("got title %q, want Test Item", detail.Title)
	}

	// Update: mark done
	done := true
	updated, err := remote.UpdateItem(item.ID, model.UpdateProjectItem{Completed: &done})
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if !updated.Completed {
		t.Error("expected item to be completed")
	}

	// Archive
	archived := true
	_, err = remote.UpdateItem(item.ID, model.UpdateProjectItem{Archived: &archived})
	if err != nil {
		t.Fatalf("UpdateItem archive: %v", err)
	}

	// List archived
	archivedItems, err := remote.ListArchived(p.ID)
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	if len(archivedItems) != 1 {
		t.Errorf("got %d archived items, want 1", len(archivedItems))
	}

	// Search (archived items are excluded from search results)
	results, err := remote.Search("Test")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d search results, want 0 (archived excluded)", len(results))
	}
}

func TestRemote_Dependencies(t *testing.T) {
	remote := setupRemote(t)

	p, _ := remote.CreateProject("Deps")
	item1, _ := remote.CreateItem(model.CreateProjectItem{Title: "Blocker", ProjectIDs: []int64{p.ID}})
	item2, _ := remote.CreateItem(model.CreateProjectItem{Title: "Blocked", ProjectIDs: []int64{p.ID}})

	// Add dependency: item2 depends on item1
	if err := remote.AddDependency(item2.ID, item1.ID); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	// Get blockers
	blockers, err := remote.GetBlockers(item2.ID)
	if err != nil {
		t.Fatalf("GetBlockers: %v", err)
	}
	if len(blockers) != 1 || blockers[0].ID != item1.ID {
		t.Errorf("got blockers %v, want [%d]", blockers, item1.ID)
	}

	// List blocked
	blocked, err := remote.ListBlocked()
	if err != nil {
		t.Fatalf("ListBlocked: %v", err)
	}
	if len(blocked) != 1 || blocked[0].ID != item2.ID {
		t.Errorf("got blocked %v, want [%d]", blocked, item2.ID)
	}

	// Remove dependency
	if err := remote.RemoveDependency(item2.ID, item1.ID); err != nil {
		t.Fatalf("RemoveDependency: %v", err)
	}
	blockers, _ = remote.GetBlockers(item2.ID)
	if len(blockers) != 0 {
		t.Errorf("got %d blockers after remove, want 0", len(blockers))
	}
}

func TestRemote_MultiProject(t *testing.T) {
	remote := setupRemote(t)

	p1, _ := remote.CreateProject("P1")
	p2, _ := remote.CreateProject("P2")
	item, _ := remote.CreateItem(model.CreateProjectItem{Title: "Multi", ProjectIDs: []int64{p1.ID}})

	// Add to second project
	if err := remote.AddToProject(item.ID, p2.ID); err != nil {
		t.Fatalf("AddToProject: %v", err)
	}

	// Get item projects
	projects, err := remote.GetItemProjects(item.ID)
	if err != nil {
		t.Fatalf("GetItemProjects: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("got %d projects, want 2", len(projects))
	}

	// Remove from first project
	if err := remote.RemoveFromProject(item.ID, p1.ID); err != nil {
		t.Fatalf("RemoveFromProject: %v", err)
	}
	projects, _ = remote.GetItemProjects(item.ID)
	if len(projects) != 1 || projects[0].ID != p2.ID {
		t.Errorf("after remove, got projects %v, want [%d]", projects, p2.ID)
	}
}

func TestRemote_Undo(t *testing.T) {
	remote := setupRemote(t)

	// CanUndo when empty
	ok, err := remote.CanUndo()
	if err != nil {
		t.Fatalf("CanUndo: %v", err)
	}
	if ok {
		t.Error("expected CanUndo=false on empty DB")
	}

	// Create something, then undo
	p, _ := remote.CreateProject("Undo")
	_, _ = remote.CreateItem(model.CreateProjectItem{Title: "Undoable", ProjectIDs: []int64{p.ID}})

	desc, err := remote.Undo()
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if desc == "" {
		t.Error("expected undo description, got empty")
	}
}

func TestRemote_ErrorMapping(t *testing.T) {
	remote := setupRemote(t)

	// Not found
	_, err := remote.GetProject(999)
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Duplicate name
	_, _ = remote.CreateProject("Dup")
	_, err = remote.CreateProject("Dup")
	if err != model.ErrDuplicateName {
		t.Errorf("expected ErrDuplicateName, got %v", err)
	}

	// Nothing to undo (on fresh DB)
	setupRemote2 := setupRemote(t)
	_, err = setupRemote2.Undo()
	if err != model.ErrNothingToUndo {
		t.Errorf("expected ErrNothingToUndo, got %v", err)
	}
}
