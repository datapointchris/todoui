package backend

import (
	"testing"
	"time"

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

func mustCreateTodo(t *testing.T, b *LocalBackend, input model.CreateTodo) *model.Todo {
	t.Helper()
	todo, err := b.CreateTodo(input)
	if err != nil {
		t.Fatalf("creating todo %q: %v", input.Title, err)
	}
	return todo
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

func TestCreateTodoRequiresProject(t *testing.T) {
	b := newTestBackend(t)

	_, err := b.CreateTodo(model.CreateTodo{
		Title:      "orphan task",
		ProjectIDs: []int64{},
	})
	if err != model.ErrLastProject {
		t.Errorf("expected ErrLastProject, got %v", err)
	}
}

func TestCreateAndGetTodo(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	notes := "some notes"
	todo, err := b.CreateTodo(model.CreateTodo{
		Title:      "fix bug",
		Notes:      &notes,
		ProjectIDs: []int64{p.ID},
	})
	if err != nil {
		t.Fatalf("creating todo: %v", err)
	}
	if todo.Title != "fix bug" {
		t.Errorf("expected title 'fix bug', got %q", todo.Title)
	}
	if todo.Notes == nil || *todo.Notes != "some notes" {
		t.Errorf("expected notes 'some notes', got %v", todo.Notes)
	}
	if len(todo.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(todo.Projects))
	}
	if todo.Projects[0].Name != "work" {
		t.Errorf("expected project 'work', got %q", todo.Projects[0].Name)
	}
}

func TestMultiProjectTodo(t *testing.T) {
	b := newTestBackend(t)

	p1, _ := b.CreateProject("work")
	p2, _ := b.CreateProject("personal")

	todo, err := b.CreateTodo(model.CreateTodo{
		Title:      "cross-cutting task",
		ProjectIDs: []int64{p1.ID, p2.ID},
	})
	if err != nil {
		t.Fatalf("creating multi-project todo: %v", err)
	}
	if len(todo.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(todo.Projects))
	}

	// Appears in both project listings
	workTodos, _ := b.ListTodos(p1.ID)
	personalTodos, _ := b.ListTodos(p2.ID)
	if len(workTodos) != 1 {
		t.Errorf("expected 1 todo in work, got %d", len(workTodos))
	}
	if len(personalTodos) != 1 {
		t.Errorf("expected 1 todo in personal, got %d", len(personalTodos))
	}
}

func TestRemoveFromProjectPreservesLastProject(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("only")
	todo, _ := b.CreateTodo(model.CreateTodo{
		Title:      "task",
		ProjectIDs: []int64{p.ID},
	})

	err := b.RemoveFromProject(todo.ID, p.ID)
	if err != model.ErrLastProject {
		t.Errorf("expected ErrLastProject, got %v", err)
	}
}

func TestDependencyCycleDetection(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	t1, _ := b.CreateTodo(model.CreateTodo{Title: "task 1", ProjectIDs: []int64{p.ID}})
	t2, _ := b.CreateTodo(model.CreateTodo{Title: "task 2", ProjectIDs: []int64{p.ID}})
	t3, _ := b.CreateTodo(model.CreateTodo{Title: "task 3", ProjectIDs: []int64{p.ID}})

	// t1 depends on t2, t2 depends on t3 — no cycle
	if err := b.AddDependency(t1.ID, t2.ID); err != nil {
		t.Fatalf("adding dep t1->t2: %v", err)
	}
	if err := b.AddDependency(t2.ID, t3.ID); err != nil {
		t.Fatalf("adding dep t2->t3: %v", err)
	}

	// t3 depends on t1 would create a cycle: t1->t2->t3->t1
	err := b.AddDependency(t3.ID, t1.ID)
	if err != model.ErrCyclicDependency {
		t.Errorf("expected ErrCyclicDependency, got %v", err)
	}
}

func TestGetBlockers(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	t1, _ := b.CreateTodo(model.CreateTodo{Title: "blocker", ProjectIDs: []int64{p.ID}})
	t2, _ := b.CreateTodo(model.CreateTodo{Title: "blocked", ProjectIDs: []int64{p.ID}})

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

func TestUpdateTodo(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	todo, _ := b.CreateTodo(model.CreateTodo{Title: "original", ProjectIDs: []int64{p.ID}})

	newTitle := "updated"
	updated, err := b.UpdateTodo(todo.ID, model.UpdateTodo{Title: &newTitle})
	if err != nil {
		t.Fatalf("updating todo: %v", err)
	}
	if updated.Title != "updated" {
		t.Errorf("expected title 'updated', got %q", updated.Title)
	}
}

func TestCompleteTodo(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	todo, _ := b.CreateTodo(model.CreateTodo{Title: "task", ProjectIDs: []int64{p.ID}})

	done := true
	updated, err := b.UpdateTodo(todo.ID, model.UpdateTodo{Completed: &done})
	if err != nil {
		t.Fatalf("completing todo: %v", err)
	}
	if !updated.Completed {
		t.Error("expected todo to be completed")
	}
}

func TestSearch(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	mustCreateTodo(t, b, model.CreateTodo{Title: "fix auth bug", ProjectIDs: []int64{p.ID}})
	mustCreateTodo(t, b, model.CreateTodo{Title: "write tests", ProjectIDs: []int64{p.ID}})

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

func TestListToday(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	today := time.Now().Truncate(24 * time.Hour)
	tomorrow := today.Add(48 * time.Hour)

	mustCreateTodo(t, b, model.CreateTodo{Title: "due today", DueDate: &today, ProjectIDs: []int64{p.ID}})
	mustCreateTodo(t, b, model.CreateTodo{Title: "due later", DueDate: &tomorrow, ProjectIDs: []int64{p.ID}})
	mustCreateTodo(t, b, model.CreateTodo{Title: "no date", ProjectIDs: []int64{p.ID}})

	results, err := b.ListToday()
	if err != nil {
		t.Fatalf("listing today: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "due today" {
		t.Errorf("expected 'due today', got %q", results[0].Title)
	}
}

func TestUndoCreateTodo(t *testing.T) {
	b := newTestBackend(t)

	p, _ := b.CreateProject("work")
	todo, _ := b.CreateTodo(model.CreateTodo{Title: "will undo", ProjectIDs: []int64{p.ID}})

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

	_, err = b.GetTodo(todo.ID)
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
