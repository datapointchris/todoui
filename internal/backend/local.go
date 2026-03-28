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

func (b *LocalBackend) ListProjects() ([]model.ProjectWithItemCount, error) {
	rows, err := b.q.ListProjectsWithItemCount(b.ctx())
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	out := make([]model.ProjectWithItemCount, len(rows))
	for i, r := range rows {
		out[i] = toModelProjectWithItemCount(r)
	}
	return out, nil
}

func (b *LocalBackend) GetProject(id int64) (*model.ProjectWithItemCount, error) {
	row, err := b.q.GetProjectWithItemCount(b.ctx(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("getting project: %w", err)
	}
	result := toModelProjectWithItemCountFromGet(row)
	return &result, nil
}

func (b *LocalBackend) CreateProject(name string) (*model.Project, error) {
	p, err := b.q.CreateProject(b.ctx(), name)
	if err != nil {
		return nil, fmt.Errorf("creating project: %w", err)
	}
	result := toModelProject(p)
	return &result, nil
}

func (b *LocalBackend) UpdateProject(id int64, input model.UpdateProject) (*model.Project, error) {
	ctx := b.ctx()

	current, err := b.q.GetProject(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("getting project for update: %w", err)
	}

	params := generated.UpdateProjectParams{
		Name:     current.Name,
		Position: current.Position,
		ID:       id,
	}
	if input.Name != nil {
		params.Name = *input.Name
	}
	if input.Position != nil {
		params.Position = int64(*input.Position)
	}

	p, err := b.q.UpdateProject(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("updating project: %w", err)
	}
	result := toModelProject(p)
	return &result, nil
}

func (b *LocalBackend) DeleteProject(id int64) error {
	return b.q.DeleteProject(b.ctx(), id)
}

// --- Items ---

func (b *LocalBackend) ListAllItems() ([]model.ProjectItem, error) {
	items, err := b.q.ListAllItems(b.ctx())
	if err != nil {
		return nil, fmt.Errorf("listing all items: %w", err)
	}
	return toModelProjectItems(items), nil
}

func (b *LocalBackend) ListItemsByProject(projectID int64) ([]model.ProjectItemInProject, error) {
	rows, err := b.q.ListItemsByProject(b.ctx(), projectID)
	if err != nil {
		return nil, fmt.Errorf("listing items by project: %w", err)
	}
	out := make([]model.ProjectItemInProject, len(rows))
	for i, r := range rows {
		out[i] = toModelProjectItemInProject(r)
	}
	return out, nil
}

func (b *LocalBackend) GetItem(id int64) (*model.ProjectItemDetail, error) {
	pi, err := b.q.GetItem(b.ctx(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("getting item: %w", err)
	}
	result := toModelProjectItem(pi)

	ps, err := b.q.GetItemProjects(b.ctx(), id)
	if err != nil {
		return nil, fmt.Errorf("getting item projects: %w", err)
	}

	depIDs, err := b.q.GetDependencyIDs(b.ctx(), id)
	if err != nil {
		return nil, fmt.Errorf("getting dependency IDs: %w", err)
	}
	if depIDs == nil {
		depIDs = []int64{}
	}

	return &model.ProjectItemDetail{
		ProjectItem:   result,
		Projects:      toModelProjects(ps),
		DependencyIDs: depIDs,
	}, nil
}

func (b *LocalBackend) CreateItem(input model.CreateProjectItem) (*model.ProjectItemDetail, error) {
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

	pi, err := qtx.CreateItem(ctx, generated.CreateItemParams{
		Title: input.Title,
		Notes: toNullString(input.Notes),
	})
	if err != nil {
		return nil, fmt.Errorf("creating item: %w", err)
	}

	for _, pid := range input.ProjectIDs {
		err := qtx.AddItemToProject(ctx, generated.AddItemToProjectParams{
			ItemID:      pi.ID,
			ProjectID:   pid,
			ProjectID_2: pid,
		})
		if err != nil {
			return nil, fmt.Errorf("adding item to project %d: %w", pid, err)
		}
	}

	if err := b.logUndo(qtx, "create", "item", pi.ID, nil); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return b.GetItem(pi.ID)
}

func (b *LocalBackend) UpdateItem(id int64, input model.UpdateProjectItem) (*model.ProjectItem, error) {
	ctx := b.ctx()

	current, err := b.q.GetItem(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("getting item for update: %w", err)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := b.q.WithTx(tx)

	params := generated.UpdateItemParams{
		Title:     current.Title,
		Notes:     current.Notes,
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
	if input.Completed != nil {
		params.Completed = boolToInt64(input.Completed)
	}
	if input.Archived != nil {
		params.Archived = boolToInt64(input.Archived)
	}

	pi, err := qtx.UpdateItem(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("updating item: %w", err)
	}

	if err := b.logUndo(qtx, "update", "item", id, current); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	result := toModelProjectItem(pi)
	return &result, nil
}

func (b *LocalBackend) DeleteItem(id int64) error {
	ctx := b.ctx()
	current, err := b.q.GetItem(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.ErrNotFound
		}
		return fmt.Errorf("getting item for delete: %w", err)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := b.q.WithTx(tx)

	if err := b.logUndo(qtx, "delete", "item", id, current); err != nil {
		return err
	}

	if err := qtx.DeleteItem(ctx, id); err != nil {
		return fmt.Errorf("deleting item: %w", err)
	}

	return tx.Commit()
}

func (b *LocalBackend) ReorderItem(itemID, projectID int64, newPosition int) error {
	return b.q.UpdateItemPosition(b.ctx(), generated.UpdateItemPositionParams{
		ItemID:    itemID,
		ProjectID: projectID,
		Position:  int64(newPosition),
	})
}

// --- Multi-project membership ---

func (b *LocalBackend) AddToProject(itemID, projectID int64) error {
	return b.q.AddItemToProject(b.ctx(), generated.AddItemToProjectParams{
		ItemID:      itemID,
		ProjectID:   projectID,
		ProjectID_2: projectID,
	})
}

func (b *LocalBackend) RemoveFromProject(itemID, projectID int64) error {
	projects, err := b.q.GetItemProjects(b.ctx(), itemID)
	if err != nil {
		return fmt.Errorf("checking project count: %w", err)
	}
	if len(projects) <= 1 {
		return model.ErrLastProject
	}
	return b.q.RemoveItemFromProject(b.ctx(), generated.RemoveItemFromProjectParams{
		ItemID:    itemID,
		ProjectID: projectID,
	})
}

func (b *LocalBackend) GetItemProjects(itemID int64) ([]model.Project, error) {
	ps, err := b.q.GetItemProjects(b.ctx(), itemID)
	if err != nil {
		return nil, fmt.Errorf("getting item projects: %w", err)
	}
	return toModelProjects(ps), nil
}

// --- Dependencies ---

func (b *LocalBackend) AddDependency(itemID, dependsOn int64) error {
	ctx := b.ctx()

	deps, err := b.q.GetAllDependencies(ctx)
	if err != nil {
		return fmt.Errorf("getting dependencies for cycle check: %w", err)
	}

	adj := make(map[int64][]int64)
	for _, d := range deps {
		adj[d.DependsOnID] = append(adj[d.DependsOnID], d.ItemID)
	}

	if graph.WouldCycle(adj, dependsOn, itemID) {
		return model.ErrCyclicDependency
	}

	return b.q.AddDependency(ctx, generated.AddDependencyParams{
		ItemID:      itemID,
		DependsOnID: dependsOn,
	})
}

func (b *LocalBackend) RemoveDependency(itemID, dependsOn int64) error {
	return b.q.RemoveDependency(b.ctx(), generated.RemoveDependencyParams{
		ItemID:      itemID,
		DependsOnID: dependsOn,
	})
}

func (b *LocalBackend) GetBlockers(itemID int64) ([]model.ProjectItem, error) {
	items, err := b.q.GetBlockers(b.ctx(), itemID)
	if err != nil {
		return nil, fmt.Errorf("getting blockers: %w", err)
	}
	return toModelProjectItems(items), nil
}

// --- Search ---

func (b *LocalBackend) Search(query string) ([]model.ProjectItem, error) {
	q := sql.NullString{String: query, Valid: true}
	items, err := b.q.SearchItems(b.ctx(), generated.SearchItemsParams{
		Column1: q,
		Column2: q,
	})
	if err != nil {
		return nil, fmt.Errorf("searching items: %w", err)
	}
	return toModelProjectItems(items), nil
}

// --- Filters ---

func (b *LocalBackend) ListBlocked() ([]model.ProjectItem, error) {
	items, err := b.q.ListBlockedItems(b.ctx())
	if err != nil {
		return nil, fmt.Errorf("listing blocked: %w", err)
	}
	return toModelProjectItems(items), nil
}

func (b *LocalBackend) ListArchived(projectID int64) ([]model.ProjectItemInProject, error) {
	rows, err := b.q.ListArchivedItems(b.ctx(), projectID)
	if err != nil {
		return nil, fmt.Errorf("listing archived: %w", err)
	}
	out := make([]model.ProjectItemInProject, len(rows))
	for i, r := range rows {
		out[i] = toModelProjectItemInProjectFromArchived(r)
	}
	return out, nil
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
		if err := qtx.DeleteItem(ctx, entry.EntityID); err != nil {
			return "", fmt.Errorf("undoing create: %w", err)
		}
	case "update":
		if entry.PreviousState.Valid {
			var prev generated.ProjectItem
			if err := json.Unmarshal([]byte(entry.PreviousState.String), &prev); err != nil {
				return "", fmt.Errorf("unmarshaling previous state: %w", err)
			}
			if _, err := qtx.UpdateItem(ctx, generated.UpdateItemParams{
				Title:     prev.Title,
				Notes:     prev.Notes,
				Completed: prev.Completed,
				Archived:  prev.Archived,
				ID:        entry.EntityID,
			}); err != nil {
				return "", fmt.Errorf("restoring previous state: %w", err)
			}
		}
	case "delete":
		if entry.PreviousState.Valid {
			var prev generated.ProjectItem
			if err := json.Unmarshal([]byte(entry.PreviousState.String), &prev); err != nil {
				return "", fmt.Errorf("unmarshaling deleted state: %w", err)
			}
			if _, err := qtx.CreateItem(ctx, generated.CreateItemParams{
				Title: prev.Title,
				Notes: prev.Notes,
			}); err != nil {
				return "", fmt.Errorf("recreating deleted item: %w", err)
			}
		}
	case "complete":
		if _, err := qtx.UpdateItem(ctx, generated.UpdateItemParams{
			Title:     "",
			Notes:     sql.NullString{},
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
