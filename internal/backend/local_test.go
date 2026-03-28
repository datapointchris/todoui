package backend

import (
	"testing"

	"github.com/datapointchris/todoui/internal/db"
	"github.com/datapointchris/todoui/internal/model"
)

func newTestBackend(t *testing.T) *LocalBackend {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return NewLocalBackend(database)
}

func mustCreateItem(t *testing.T, b *LocalBackend, input model.CreateProjectItem) *model.ProjectItemDetail {
	t.Helper()
	item, err := b.CreateItem(input)
	if err != nil {
		t.Fatalf("creating item %q: %v", input.Title, err)
	}
	return item
}

func TestCreateAndListProjects(t *testing.T) {
	b := newTestBackend(t)

	p, err := b.CreateProject("work")
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	if p.Name != "work" {
		t.Errorf("expected name 'work', got %q", p.Name)
	}

	projects, err := b.ListProjects()
	if err != nil {
		t.Fatalf("listing projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].Name != "work" {
		t.Errorf("expected 'work', got %q", projects[0].Name)
	}
}

func TestGetProject(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "task 1", ProjectIDs: []int64{p.ID}})
	mustCreateItem(t, b, model.CreateProjectItem{Title: "task 2", ProjectIDs: []int64{p.ID}})

	project, err := b.GetProject(p.ID)
	if err != nil {
		t.Fatalf("getting project: %v", err)
	}
	if project.Name != "work" {
		t.Errorf("expected 'work', got %q", project.Name)
	}
	if project.ItemCount != 2 {
		t.Errorf("expected item_count 2, got %d", project.ItemCount)
	}
}

func TestUpdateProject(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("old name")
	newName := "new name"
	newPos := 5

	updated, err := b.UpdateProject(p.ID, model.UpdateProject{Name: &newName, Position: &newPos})
	if err != nil {
		t.Fatalf("updating project: %v", err)
	}
	if updated.Name != "new name" {
		t.Errorf("expected 'new name', got %q", updated.Name)
	}
	if updated.Position != 5 {
		t.Errorf("expected position 5, got %d", updated.Position)
	}
}

func TestCreateItemRequiresProject(t *testing.T) {
	b := newTestBackend(t)

	_, err := b.CreateItem(model.CreateProjectItem{
		Title:      "orphan task",
		ProjectIDs: []int64{},
	})
	if err != model.ErrLastProject {
		t.Errorf("expected ErrLastProject, got %v", err)
	}
}

func TestCreateAndGetItem(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	notes := "some notes"
	item, err := b.CreateItem(model.CreateProjectItem{
		Title:      "fix bug",
		Notes:      &notes,
		ProjectIDs: []int64{p.ID},
	})
	if err != nil {
		t.Fatalf("creating item: %v", err)
	}
	if item.Title != "fix bug" {
		t.Errorf("expected title 'fix bug', got %q", item.Title)
	}
	if item.Notes == nil || *item.Notes != "some notes" {
		t.Errorf("expected notes 'some notes', got %v", item.Notes)
	}
	if len(item.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(item.Projects))
	}
	if item.Projects[0].Name != "work" {
		t.Errorf("expected project 'work', got %q", item.Projects[0].Name)
	}
	if item.DependencyIDs == nil {
		t.Error("expected non-nil dependency_ids")
	}
	if len(item.DependencyIDs) != 0 {
		t.Errorf("expected 0 dependency_ids, got %d", len(item.DependencyIDs))
	}
}

func TestMultiProjectItem(t *testing.T) {
	b := newTestBackend(t)

	p1, _ := b.CreateProject("work")
	p2, _ := b.CreateProject("personal")

	item, err := b.CreateItem(model.CreateProjectItem{
		Title:      "cross-cutting task",
		ProjectIDs: []int64{p1.ID, p2.ID},
	})
	if err != nil {
		t.Fatalf("creating multi-project item: %v", err)
	}
	if len(item.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(item.Projects))
	}

	workItems, _ := b.ListItemsByProject(p1.ID)
	personalItems, _ := b.ListItemsByProject(p2.ID)
	if len(workItems) != 1 {
		t.Errorf("expected 1 item in work, got %d", len(workItems))
	}
	if len(personalItems) != 1 {
		t.Errorf("expected 1 item in personal, got %d", len(personalItems))
	}
}

func TestListItemsByProjectIncludesPosition(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "first", ProjectIDs: []int64{p.ID}})
	mustCreateItem(t, b, model.CreateProjectItem{Title: "second", ProjectIDs: []int64{p.ID}})

	items, err := b.ListItemsByProject(p.ID)
	if err != nil {
		t.Fatalf("listing items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Position == 0 && items[1].Position == 0 {
		t.Error("expected items to have non-zero positions")
	}
}

func TestListAllItems(t *testing.T) {
	b := newTestBackend(t)

	p1, _ := b.CreateProject("work")
	p2, _ := b.CreateProject("personal")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "work task", ProjectIDs: []int64{p1.ID}})
	mustCreateItem(t, b, model.CreateProjectItem{Title: "personal task", ProjectIDs: []int64{p2.ID}})

	items, err := b.ListAllItems()
	if err != nil {
		t.Fatalf("listing all items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestRemoveFromProjectPreservesLastProject(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("only")
	item := mustCreateItem(t, b, model.CreateProjectItem{
		Title:      "task",
		ProjectIDs: []int64{p.ID},
	})

	err := b.RemoveFromProject(item.ID, p.ID)
	if err != model.ErrLastProject {
		t.Errorf("expected ErrLastProject, got %v", err)
	}
}

func TestDependencyCycleDetection(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	t1 := mustCreateItem(t, b, model.CreateProjectItem{Title: "task 1", ProjectIDs: []int64{p.ID}})
	t2 := mustCreateItem(t, b, model.CreateProjectItem{Title: "task 2", ProjectIDs: []int64{p.ID}})
	t3 := mustCreateItem(t, b, model.CreateProjectItem{Title: "task 3", ProjectIDs: []int64{p.ID}})

	if err := b.AddDependency(t1.ID, t2.ID); err != nil {
		t.Fatalf("adding dep t1->t2: %v", err)
	}
	if err := b.AddDependency(t2.ID, t3.ID); err != nil {
		t.Fatalf("adding dep t2->t3: %v", err)
	}

	err := b.AddDependency(t3.ID, t1.ID)
	if err != model.ErrCyclicDependency {
		t.Errorf("expected ErrCyclicDependency, got %v", err)
	}
}

func TestGetBlockers(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	t1 := mustCreateItem(t, b, model.CreateProjectItem{Title: "blocker", ProjectIDs: []int64{p.ID}})
	t2 := mustCreateItem(t, b, model.CreateProjectItem{Title: "blocked", ProjectIDs: []int64{p.ID}})

	_ = b.AddDependency(t2.ID, t1.ID)

	blockers, err := b.GetBlockers(t2.ID)
	if err != nil {
		t.Fatalf("getting blockers: %v", err)
	}
	if len(blockers) != 1 {
		t.Fatalf("expected 1 blocker, got %d", len(blockers))
	}
	if blockers[0].ID != t1.ID {
		t.Errorf("expected blocker ID %d, got %d", t1.ID, blockers[0].ID)
	}
}

func TestGetItemDependencyIDs(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	t1 := mustCreateItem(t, b, model.CreateProjectItem{Title: "dep 1", ProjectIDs: []int64{p.ID}})
	t2 := mustCreateItem(t, b, model.CreateProjectItem{Title: "dep 2", ProjectIDs: []int64{p.ID}})
	t3 := mustCreateItem(t, b, model.CreateProjectItem{Title: "main", ProjectIDs: []int64{p.ID}})

	_ = b.AddDependency(t3.ID, t1.ID)
	_ = b.AddDependency(t3.ID, t2.ID)

	detail, err := b.GetItem(t3.ID)
	if err != nil {
		t.Fatalf("getting item: %v", err)
	}
	if len(detail.DependencyIDs) != 2 {
		t.Fatalf("expected 2 dependency_ids, got %d", len(detail.DependencyIDs))
	}
}

func TestUpdateItem(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	item := mustCreateItem(t, b, model.CreateProjectItem{Title: "original", ProjectIDs: []int64{p.ID}})

	newTitle := "updated"
	updated, err := b.UpdateItem(item.ID, model.UpdateProjectItem{Title: &newTitle})
	if err != nil {
		t.Fatalf("updating item: %v", err)
	}
	if updated.Title != "updated" {
		t.Errorf("expected title 'updated', got %q", updated.Title)
	}
}

func TestCompleteItem(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	item := mustCreateItem(t, b, model.CreateProjectItem{Title: "task", ProjectIDs: []int64{p.ID}})

	done := true
	updated, err := b.UpdateItem(item.ID, model.UpdateProjectItem{Completed: &done})
	if err != nil {
		t.Fatalf("completing item: %v", err)
	}
	if !updated.Completed {
		t.Error("expected item to be completed")
	}
}

func TestSearch(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "fix auth bug", ProjectIDs: []int64{p.ID}})
	mustCreateItem(t, b, model.CreateProjectItem{Title: "write tests", ProjectIDs: []int64{p.ID}})

	results, err := b.Search("auth")
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "fix auth bug" {
		t.Errorf("expected 'fix auth bug', got %q", results[0].Title)
	}
}

func TestUndoCreateItem(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	item := mustCreateItem(t, b, model.CreateProjectItem{Title: "will undo", ProjectIDs: []int64{p.ID}})

	canUndo, _ := b.CanUndo()
	if !canUndo {
		t.Fatal("expected canUndo to be true")
	}

	desc, err := b.Undo()
	if err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if desc == "" {
		t.Error("expected non-empty undo description")
	}

	_, err = b.GetItem(item.ID)
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound after undo, got %v", err)
	}
}

func TestUndoWhenEmpty(t *testing.T) {
	b := newTestBackend(t)

	_, err := b.Undo()
	if err != model.ErrNothingToUndo {
		t.Errorf("expected ErrNothingToUndo, got %v", err)
	}
}

func TestProjectItemCountExcludesArchived(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "active", ProjectIDs: []int64{p.ID}})
	archived := mustCreateItem(t, b, model.CreateProjectItem{Title: "archived", ProjectIDs: []int64{p.ID}})

	archiveTrue := true
	_, _ = b.UpdateItem(archived.ID, model.UpdateProjectItem{Archived: &archiveTrue})

	project, err := b.GetProject(p.ID)
	if err != nil {
		t.Fatalf("getting project: %v", err)
	}
	if project.ItemCount != 1 {
		t.Errorf("expected item_count 1 (excluding archived), got %d", project.ItemCount)
	}
}
