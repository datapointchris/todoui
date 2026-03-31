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

func mustCreateProject(t *testing.T, b *LocalBackend, name string) *model.Project {
	t.Helper()
	p, err := b.CreateProject(model.CreateProject{Name: name})
	if err != nil {
		t.Fatalf("creating project %q: %v", name, err)
	}
	return p
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

	p := mustCreateProject(t, b, "work")
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

	p := mustCreateProject(t, b, "work")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "task 1", ProjectIDs: []string{p.ID}})
	mustCreateItem(t, b, model.CreateProjectItem{Title: "task 2", ProjectIDs: []string{p.ID}})

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

	p := mustCreateProject(t, b, "old name")
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

func TestProjectDescription(t *testing.T) {
	b := newTestBackend(t)

	desc := "A project for work tasks"
	p, err := b.CreateProject(model.CreateProject{Name: "work", Description: &desc})
	if err != nil {
		t.Fatalf("creating project with description: %v", err)
	}
	if p.Description == nil || *p.Description != desc {
		t.Errorf("expected description %q, got %v", desc, p.Description)
	}

	project, err := b.GetProject(p.ID)
	if err != nil {
		t.Fatalf("getting project: %v", err)
	}
	if project.Description == nil || *project.Description != desc {
		t.Errorf("expected description %q after get, got %v", desc, project.Description)
	}
}

func TestCreateItemRequiresProject(t *testing.T) {
	b := newTestBackend(t)

	_, err := b.CreateItem(model.CreateProjectItem{
		Title:      "orphan task",
		ProjectIDs: []string{},
	})
	if err != model.ErrLastProject {
		t.Errorf("expected ErrLastProject, got %v", err)
	}
}

func TestCreateAndGetItem(t *testing.T) {
	b := newTestBackend(t)

	p := mustCreateProject(t, b, "work")
	notes := "some notes"
	item, err := b.CreateItem(model.CreateProjectItem{
		Title:      "fix bug",
		Notes:      &notes,
		ProjectIDs: []string{p.ID},
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

	p1 := mustCreateProject(t, b, "work")
	p2 := mustCreateProject(t, b, "personal")

	item, err := b.CreateItem(model.CreateProjectItem{
		Title:      "cross-cutting task",
		ProjectIDs: []string{p1.ID, p2.ID},
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

	p := mustCreateProject(t, b, "work")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "first", ProjectIDs: []string{p.ID}})
	mustCreateItem(t, b, model.CreateProjectItem{Title: "second", ProjectIDs: []string{p.ID}})

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

	p1 := mustCreateProject(t, b, "work")
	p2 := mustCreateProject(t, b, "personal")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "work task", ProjectIDs: []string{p1.ID}})
	mustCreateItem(t, b, model.CreateProjectItem{Title: "personal task", ProjectIDs: []string{p2.ID}})

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

	p := mustCreateProject(t, b, "only")
	item := mustCreateItem(t, b, model.CreateProjectItem{
		Title:      "task",
		ProjectIDs: []string{p.ID},
	})

	err := b.RemoveFromProject(item.ID, p.ID)
	if err != model.ErrLastProject {
		t.Errorf("expected ErrLastProject, got %v", err)
	}
}

func TestDependencyCycleDetection(t *testing.T) {
	b := newTestBackend(t)

	p := mustCreateProject(t, b, "work")
	t1 := mustCreateItem(t, b, model.CreateProjectItem{Title: "task 1", ProjectIDs: []string{p.ID}})
	t2 := mustCreateItem(t, b, model.CreateProjectItem{Title: "task 2", ProjectIDs: []string{p.ID}})
	t3 := mustCreateItem(t, b, model.CreateProjectItem{Title: "task 3", ProjectIDs: []string{p.ID}})

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

	p := mustCreateProject(t, b, "work")
	t1 := mustCreateItem(t, b, model.CreateProjectItem{Title: "blocker", ProjectIDs: []string{p.ID}})
	t2 := mustCreateItem(t, b, model.CreateProjectItem{Title: "blocked", ProjectIDs: []string{p.ID}})

	_ = b.AddDependency(t2.ID, t1.ID)

	blockers, err := b.GetBlockers(t2.ID)
	if err != nil {
		t.Fatalf("getting blockers: %v", err)
	}
	if len(blockers) != 1 {
		t.Fatalf("expected 1 blocker, got %d", len(blockers))
	}
	if blockers[0].ID != t1.ID {
		t.Errorf("expected blocker ID %s, got %s", t1.ID, blockers[0].ID)
	}
}

func TestGetItemDependencyIDs(t *testing.T) {
	b := newTestBackend(t)

	p := mustCreateProject(t, b, "work")
	t1 := mustCreateItem(t, b, model.CreateProjectItem{Title: "dep 1", ProjectIDs: []string{p.ID}})
	t2 := mustCreateItem(t, b, model.CreateProjectItem{Title: "dep 2", ProjectIDs: []string{p.ID}})
	t3 := mustCreateItem(t, b, model.CreateProjectItem{Title: "main", ProjectIDs: []string{p.ID}})

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

	p := mustCreateProject(t, b, "work")
	item := mustCreateItem(t, b, model.CreateProjectItem{Title: "original", ProjectIDs: []string{p.ID}})

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

	p := mustCreateProject(t, b, "work")
	item := mustCreateItem(t, b, model.CreateProjectItem{Title: "task", ProjectIDs: []string{p.ID}})

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

	p := mustCreateProject(t, b, "work")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "fix auth bug", ProjectIDs: []string{p.ID}})
	mustCreateItem(t, b, model.CreateProjectItem{Title: "write tests", ProjectIDs: []string{p.ID}})

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

	p := mustCreateProject(t, b, "work")
	item := mustCreateItem(t, b, model.CreateProjectItem{Title: "will undo", ProjectIDs: []string{p.ID}})

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

	p := mustCreateProject(t, b, "work")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "active", ProjectIDs: []string{p.ID}})
	archived := mustCreateItem(t, b, model.CreateProjectItem{Title: "archived", ProjectIDs: []string{p.ID}})

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

func TestTaskCRUD(t *testing.T) {
	b := newTestBackend(t)

	p := mustCreateProject(t, b, "work")
	item := mustCreateItem(t, b, model.CreateProjectItem{Title: "main item", ProjectIDs: []string{p.ID}})

	// Create tasks
	task1, err := b.CreateTask(item.ID, model.CreateProjectItemTask{Title: "subtask 1"})
	if err != nil {
		t.Fatalf("creating task: %v", err)
	}
	if task1.Title != "subtask 1" {
		t.Errorf("expected title 'subtask 1', got %q", task1.Title)
	}
	if task1.ItemID != item.ID {
		t.Errorf("expected item_id %s, got %s", item.ID, task1.ItemID)
	}

	task2, err := b.CreateTask(item.ID, model.CreateProjectItemTask{Title: "subtask 2"})
	if err != nil {
		t.Fatalf("creating task 2: %v", err)
	}

	// List tasks
	tasks, err := b.ListTasks(item.ID)
	if err != nil {
		t.Fatalf("listing tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	// Update task
	newTitle := "updated subtask"
	updated, err := b.UpdateTask(item.ID, task1.ID, model.UpdateProjectItemTask{Title: &newTitle})
	if err != nil {
		t.Fatalf("updating task: %v", err)
	}
	if updated.Title != "updated subtask" {
		t.Errorf("expected 'updated subtask', got %q", updated.Title)
	}

	// Complete task
	if err := b.CompleteTask(item.ID, task1.ID); err != nil {
		t.Fatalf("completing task: %v", err)
	}

	// Delete task
	if err := b.DeleteTask(item.ID, task2.ID); err != nil {
		t.Fatalf("deleting task: %v", err)
	}
	tasks, _ = b.ListTasks(item.ID)
	if len(tasks) != 1 {
		t.Errorf("expected 1 task after delete, got %d", len(tasks))
	}
}

func TestDeleteProject(t *testing.T) {
	b := newTestBackend(t)

	p := mustCreateProject(t, b, "doomed")
	if err := b.DeleteProject(p.ID); err != nil {
		t.Fatalf("deleting project: %v", err)
	}

	projects, _ := b.ListProjects()
	if len(projects) != 0 {
		t.Errorf("expected 0 projects after delete, got %d", len(projects))
	}
}

func TestDeleteItem(t *testing.T) {
	b := newTestBackend(t)

	p := mustCreateProject(t, b, "work")
	item := mustCreateItem(t, b, model.CreateProjectItem{Title: "doomed", ProjectIDs: []string{p.ID}})

	if err := b.DeleteItem(item.ID); err != nil {
		t.Fatalf("deleting item: %v", err)
	}

	_, err := b.GetItem(item.ID)
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}

	// Undo should restore the item
	_, err = b.Undo()
	if err != nil {
		t.Fatalf("undoing delete: %v", err)
	}
	restored, err := b.GetItem(item.ID)
	if err != nil {
		t.Fatalf("getting restored item: %v", err)
	}
	if restored.Title != "doomed" {
		t.Errorf("expected restored title 'doomed', got %q", restored.Title)
	}
}

func TestReorderItem(t *testing.T) {
	b := newTestBackend(t)

	p := mustCreateProject(t, b, "work")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "first", ProjectIDs: []string{p.ID}})
	item2 := mustCreateItem(t, b, model.CreateProjectItem{Title: "second", ProjectIDs: []string{p.ID}})

	if err := b.ReorderItem(item2.ID, p.ID, 1); err != nil {
		t.Fatalf("reordering item: %v", err)
	}

	items, _ := b.ListItemsByProject(p.ID)
	for _, it := range items {
		if it.ID == item2.ID && it.Position != 1 {
			t.Errorf("expected position 1 for reordered item, got %d", it.Position)
		}
	}
}

func TestRemoveDependency(t *testing.T) {
	b := newTestBackend(t)

	p := mustCreateProject(t, b, "work")
	t1 := mustCreateItem(t, b, model.CreateProjectItem{Title: "blocker", ProjectIDs: []string{p.ID}})
	t2 := mustCreateItem(t, b, model.CreateProjectItem{Title: "blocked", ProjectIDs: []string{p.ID}})

	_ = b.AddDependency(t2.ID, t1.ID)

	if err := b.RemoveDependency(t2.ID, t1.ID); err != nil {
		t.Fatalf("removing dependency: %v", err)
	}

	blockers, _ := b.GetBlockers(t2.ID)
	if len(blockers) != 0 {
		t.Errorf("expected 0 blockers after remove, got %d", len(blockers))
	}
}

func TestListBlocked(t *testing.T) {
	b := newTestBackend(t)

	p := mustCreateProject(t, b, "work")
	blocker := mustCreateItem(t, b, model.CreateProjectItem{Title: "blocker", ProjectIDs: []string{p.ID}})
	blocked := mustCreateItem(t, b, model.CreateProjectItem{Title: "blocked", ProjectIDs: []string{p.ID}})
	mustCreateItem(t, b, model.CreateProjectItem{Title: "free", ProjectIDs: []string{p.ID}})

	_ = b.AddDependency(blocked.ID, blocker.ID)

	items, err := b.ListBlocked()
	if err != nil {
		t.Fatalf("ListBlocked: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 blocked item, got %d", len(items))
	}
	if items[0].ID != blocked.ID {
		t.Errorf("expected blocked item %s, got %s", blocked.ID, items[0].ID)
	}
}

func TestListArchived(t *testing.T) {
	b := newTestBackend(t)

	p := mustCreateProject(t, b, "work")
	mustCreateItem(t, b, model.CreateProjectItem{Title: "active", ProjectIDs: []string{p.ID}})
	archived := mustCreateItem(t, b, model.CreateProjectItem{Title: "archived", ProjectIDs: []string{p.ID}})

	archiveTrue := true
	_, _ = b.UpdateItem(archived.ID, model.UpdateProjectItem{Archived: &archiveTrue})

	items, err := b.ListArchived(p.ID)
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 archived item, got %d", len(items))
	}
	if items[0].Title != "archived" {
		t.Errorf("expected 'archived', got %q", items[0].Title)
	}
}
