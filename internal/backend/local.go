package backend

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/datapointchris/todoui/internal/db/generated"
	"github.com/datapointchris/todoui/internal/graph"
	"github.com/datapointchris/todoui/internal/model"
)

// LocalBackend provides direct SQLite access for local mode.
type LocalBackend struct {
	db *sql.DB
	q  *generated.Queries
}

// NewLocalBackend creates a backend that operates directly on a local SQLite database.
func NewLocalBackend(db *sql.DB) *LocalBackend {
	return &LocalBackend{db: db, q: generated.New(db)}
}

// Compile-time check that LocalBackend implements Backend.
var _ Backend = (*LocalBackend)(nil)

func (b *LocalBackend) ctx() context.Context {
	return context.Background()
}

// --- Projects ---

func (b *LocalBackend) ListProjects() ([]model.Project, error) {
	ps, err := b.q.ListProjects(b.ctx())
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	return toModelProjects(ps), nil
}

func (b *LocalBackend) CreateProject(name string) (*model.Project, error) {
	p, err := b.q.CreateProject(b.ctx(), name)
	if err != nil {
		return nil, fmt.Errorf("creating project: %w", err)
	}
	result := toModelProject(p)
	return &result, nil
}

func (b *LocalBackend) DeleteProject(id int64) error {
	return b.q.DeleteProject(b.ctx(), id)
}

func (b *LocalBackend) ReorderProject(id int64, newPosition int) error {
	return b.q.UpdateProjectPosition(b.ctx(), generated.UpdateProjectPositionParams{
		ID:       id,
		Position: int64(newPosition),
	})
}

// --- Todos ---

func (b *LocalBackend) ListTodos(projectID int64) ([]model.Todo, error) {
	ts, err := b.q.ListTodosByProject(b.ctx(), projectID)
	if err != nil {
		return nil, fmt.Errorf("listing todos: %w", err)
	}
	return toModelTodos(ts), nil
}

func (b *LocalBackend) GetTodo(id int64) (*model.Todo, error) {
	t, err := b.q.GetTodo(b.ctx(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("getting todo: %w", err)
	}
	result := toModelTodo(t)

	// Attach project list
	ps, err := b.q.GetTodoProjects(b.ctx(), id)
	if err != nil {
		return nil, fmt.Errorf("getting todo projects: %w", err)
	}
	result.Projects = toModelProjects(ps)
	return &result, nil
}

func (b *LocalBackend) CreateTodo(input model.CreateTodo) (*model.Todo, error) {
	if len(input.ProjectIDs) == 0 {
		return nil, model.ErrLastProject
	}

	tx, err := b.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := b.q.WithTx(tx)
	ctx := b.ctx()

	t, err := qtx.CreateTodo(ctx, generated.CreateTodoParams{
		Title:   input.Title,
		Notes:   toNullString(input.Notes),
		DueDate: timeToNullString(input.DueDate),
	})
	if err != nil {
		return nil, fmt.Errorf("creating todo: %w", err)
	}

	for _, pid := range input.ProjectIDs {
		err := qtx.AddTodoToProject(ctx, generated.AddTodoToProjectParams{
			TodoID:      t.ID,
			ProjectID:   pid,
			ProjectID_2: pid,
		})
		if err != nil {
			return nil, fmt.Errorf("adding todo to project %d: %w", pid, err)
		}
	}

	if err := b.logUndo(qtx, "create", "todo", t.ID, nil); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return b.GetTodo(t.ID)
}

func (b *LocalBackend) UpdateTodo(id int64, input model.UpdateTodo) (*model.Todo, error) {
	ctx := b.ctx()

	// Snapshot current state for undo
	current, err := b.q.GetTodo(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("getting todo for update: %w", err)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := b.q.WithTx(tx)

	// Build update params — COALESCE in the query handles nil fields
	params := generated.UpdateTodoParams{
		Title:     current.Title,
		Notes:     current.Notes,
		DueDate:   current.DueDate,
		Completed: current.Completed,
		Archived:  current.Archived,
		ID:        id,
	}
	if input.Title != nil {
		params.Title = *input.Title
	}
	if input.Notes != nil {
		params.Notes = toNullString(input.Notes)
	}
	if input.DueDate != nil {
		params.DueDate = timeToNullString(input.DueDate)
	}
	if input.Completed != nil {
		params.Completed = boolToInt64(input.Completed)
	}
	if input.Archived != nil {
		params.Archived = boolToInt64(input.Archived)
	}

	if _, err := qtx.UpdateTodo(ctx, params); err != nil {
		return nil, fmt.Errorf("updating todo: %w", err)
	}

	if err := b.logUndo(qtx, "update", "todo", id, current); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return b.GetTodo(id)
}

func (b *LocalBackend) DeleteTodo(id int64) error {
	ctx := b.ctx()
	current, err := b.q.GetTodo(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.ErrNotFound
		}
		return fmt.Errorf("getting todo for delete: %w", err)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := b.q.WithTx(tx)

	if err := b.logUndo(qtx, "delete", "todo", id, current); err != nil {
		return err
	}

	if err := qtx.DeleteTodo(ctx, id); err != nil {
		return fmt.Errorf("deleting todo: %w", err)
	}

	return tx.Commit()
}

func (b *LocalBackend) ReorderTodo(todoID, projectID int64, newPosition int) error {
	return b.q.UpdateTodoPosition(b.ctx(), generated.UpdateTodoPositionParams{
		TodoID:    todoID,
		ProjectID: projectID,
		Position:  int64(newPosition),
	})
}

// --- Multi-project membership ---

func (b *LocalBackend) AddToProject(todoID, projectID int64) error {
	return b.q.AddTodoToProject(b.ctx(), generated.AddTodoToProjectParams{
		TodoID:      todoID,
		ProjectID:   projectID,
		ProjectID_2: projectID,
	})
}

func (b *LocalBackend) RemoveFromProject(todoID, projectID int64) error {
	// Check this isn't the last project
	projects, err := b.q.GetTodoProjects(b.ctx(), todoID)
	if err != nil {
		return fmt.Errorf("checking project count: %w", err)
	}
	if len(projects) <= 1 {
		return model.ErrLastProject
	}
	return b.q.RemoveTodoFromProject(b.ctx(), generated.RemoveTodoFromProjectParams{
		TodoID:    todoID,
		ProjectID: projectID,
	})
}

func (b *LocalBackend) GetTodoProjects(todoID int64) ([]model.Project, error) {
	ps, err := b.q.GetTodoProjects(b.ctx(), todoID)
	if err != nil {
		return nil, fmt.Errorf("getting todo projects: %w", err)
	}
	return toModelProjects(ps), nil
}

// --- Dependencies ---

func (b *LocalBackend) AddDependency(todoID, dependsOn int64) error {
	ctx := b.ctx()

	// Build adjacency list and check for cycles
	deps, err := b.q.GetAllDependencies(ctx)
	if err != nil {
		return fmt.Errorf("getting dependencies for cycle check: %w", err)
	}

	adj := make(map[int64][]int64)
	for _, d := range deps {
		adj[d.DependsOnID] = append(adj[d.DependsOnID], d.TodoID)
	}

	if graph.WouldCycle(adj, dependsOn, todoID) {
		return model.ErrCyclicDependency
	}

	return b.q.AddDependency(ctx, generated.AddDependencyParams{
		TodoID:      todoID,
		DependsOnID: dependsOn,
	})
}

func (b *LocalBackend) RemoveDependency(todoID, dependsOn int64) error {
	return b.q.RemoveDependency(b.ctx(), generated.RemoveDependencyParams{
		TodoID:      todoID,
		DependsOnID: dependsOn,
	})
}

func (b *LocalBackend) GetBlockers(todoID int64) ([]model.Todo, error) {
	ts, err := b.q.GetBlockers(b.ctx(), todoID)
	if err != nil {
		return nil, fmt.Errorf("getting blockers: %w", err)
	}
	return toModelTodos(ts), nil
}

// --- Search ---

func (b *LocalBackend) Search(query string) ([]model.Todo, error) {
	q := sql.NullString{String: query, Valid: true}
	ts, err := b.q.SearchTodos(b.ctx(), generated.SearchTodosParams{
		Column1: q,
		Column2: q,
	})
	if err != nil {
		return nil, fmt.Errorf("searching todos: %w", err)
	}
	return toModelTodos(ts), nil
}

// --- Filters ---

func (b *LocalBackend) ListToday() ([]model.Todo, error) {
	ts, err := b.q.ListTodosToday(b.ctx())
	if err != nil {
		return nil, fmt.Errorf("listing today: %w", err)
	}
	return toModelTodos(ts), nil
}

func (b *LocalBackend) ListBlocked() ([]model.Todo, error) {
	ts, err := b.q.ListBlockedTodos(b.ctx())
	if err != nil {
		return nil, fmt.Errorf("listing blocked: %w", err)
	}
	return toModelTodos(ts), nil
}

func (b *LocalBackend) ListArchived(projectID int64) ([]model.Todo, error) {
	ts, err := b.q.ListArchivedTodos(b.ctx(), projectID)
	if err != nil {
		return nil, fmt.Errorf("listing archived: %w", err)
	}
	return toModelTodos(ts), nil
}

// --- Undo ---

func (b *LocalBackend) Undo() (string, error) {
	ctx := b.ctx()
	entry, err := b.q.GetLatestUndoLog(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", model.ErrNothingToUndo
		}
		return "", fmt.Errorf("getting undo log: %w", err)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return "", fmt.Errorf("beginning undo transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := b.q.WithTx(tx)

	description := fmt.Sprintf("undid %s on %s #%d", entry.Action, entry.EntityType, entry.EntityID)

	switch entry.Action {
	case "create":
		// Undo a create = delete
		if err := qtx.DeleteTodo(ctx, entry.EntityID); err != nil {
			return "", fmt.Errorf("undoing create: %w", err)
		}
	case "update":
		// Undo an update = restore previous state
		if entry.PreviousState.Valid {
			var prev generated.Todo
			if err := json.Unmarshal([]byte(entry.PreviousState.String), &prev); err != nil {
				return "", fmt.Errorf("unmarshaling previous state: %w", err)
			}
			if _, err := qtx.UpdateTodo(ctx, generated.UpdateTodoParams{
				Title:     prev.Title,
				Notes:     prev.Notes,
				DueDate:   prev.DueDate,
				Completed: prev.Completed,
				Archived:  prev.Archived,
				ID:        entry.EntityID,
			}); err != nil {
				return "", fmt.Errorf("restoring previous state: %w", err)
			}
		}
	case "delete":
		// Undo a delete = recreate from snapshot
		if entry.PreviousState.Valid {
			var prev generated.Todo
			if err := json.Unmarshal([]byte(entry.PreviousState.String), &prev); err != nil {
				return "", fmt.Errorf("unmarshaling deleted state: %w", err)
			}
			if _, err := qtx.CreateTodo(ctx, generated.CreateTodoParams{
				Title:   prev.Title,
				Notes:   prev.Notes,
				DueDate: prev.DueDate,
			}); err != nil {
				return "", fmt.Errorf("recreating deleted todo: %w", err)
			}
		}
	case "complete":
		// Undo a complete = mark incomplete
		if _, err := qtx.UpdateTodo(ctx, generated.UpdateTodoParams{
			Title:     "",
			Notes:     sql.NullString{},
			DueDate:   sql.NullString{},
			Completed: 0,
			Archived:  0,
			ID:        entry.EntityID,
		}); err != nil {
			return "", fmt.Errorf("undoing complete: %w", err)
		}
	}

	if err := qtx.DeleteUndoLog(ctx, entry.ID); err != nil {
		return "", fmt.Errorf("deleting undo log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("committing undo: %w", err)
	}

	return description, nil
}

func (b *LocalBackend) CanUndo() (bool, error) {
	count, err := b.q.CountUndoLogs(b.ctx())
	if err != nil {
		return false, fmt.Errorf("counting undo logs: %w", err)
	}
	return count > 0, nil
}

// logUndo records an action to the undo log within the given transaction.
func (b *LocalBackend) logUndo(qtx *generated.Queries, action, entityType string, entityID int64, previousState any) error {
	var prevJSON sql.NullString
	if previousState != nil {
		data, err := json.Marshal(previousState)
		if err != nil {
			return fmt.Errorf("marshaling previous state: %w", err)
		}
		prevJSON = sql.NullString{String: string(data), Valid: true}
	}
	return qtx.InsertUndoLog(b.ctx(), generated.InsertUndoLogParams{
		Action:        action,
		EntityType:    entityType,
		EntityID:      entityID,
		PreviousState: prevJSON,
	})
}
