package backend

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/datapointchris/todoui/db/generated"
	"github.com/datapointchris/todoui/graph"
	"github.com/datapointchris/todoui/model"
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

func newID() string {
	return uuid.Must(uuid.NewV7()).String()
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

func (b *LocalBackend) GetProject(id string) (*model.ProjectWithItemCount, error) {
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

func (b *LocalBackend) CreateProject(input model.CreateProject) (*model.Project, error) {
	ctx := b.ctx()
	tx, err := b.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning create project transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := b.q.WithTx(tx)

	p, err := qtx.CreateProject(ctx, generated.CreateProjectParams{
		ID:          newID(),
		Name:        input.Name,
		Description: toNullString(input.Description),
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, model.ErrDuplicateName
		}
		return nil, fmt.Errorf("creating project: %w", err)
	}
	if err := b.logUndo(qtx, "create", "project", p.ID, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing create project: %w", err)
	}
	result := toModelProject(p)
	return &result, nil
}

func (b *LocalBackend) UpdateProject(id string, input model.UpdateProject) (*model.Project, error) {
	ctx := b.ctx()

	current, err := b.q.GetProject(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("getting project for update: %w", err)
	}

	params := generated.UpdateProjectParams{
		Name:        current.Name,
		Description: current.Description,
		Position:    current.Position,
		ID:          id,
	}
	if input.Name != nil {
		params.Name = *input.Name
	}
	if input.Description != nil {
		params.Description = toNullString(input.Description)
	}
	if input.Position != nil {
		params.Position = int64(*input.Position)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning update project transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := b.q.WithTx(tx)

	p, err := qtx.UpdateProject(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("updating project: %w", err)
	}
	if err := b.logUndo(qtx, "update", "project", id, current); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing update project: %w", err)
	}
	result := toModelProject(p)
	return &result, nil
}

func (b *LocalBackend) DeleteProject(id string) error {
	ctx := b.ctx()
	snapshot, err := captureProject(ctx, b.q, id)
	if err != nil {
		return err
	}

	tx, err := b.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning delete project transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := b.q.WithTx(tx)

	if err := qtx.DeleteProject(ctx, id); err != nil {
		return fmt.Errorf("deleting project: %w", err)
	}
	if err := b.logUndo(qtx, "delete", "project", id, snapshot); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing delete project: %w", err)
	}
	return nil
}

func (b *LocalBackend) ReorderProject(projectID string, newPosition int) error {
	return b.q.UpdateProjectPosition(b.ctx(), generated.UpdateProjectPositionParams{
		ID:       projectID,
		Position: int64(newPosition),
	})
}

// --- Items ---

func (b *LocalBackend) ListAllItems() ([]model.ProjectItem, error) {
	items, err := b.q.ListAllItems(b.ctx())
	if err != nil {
		return nil, fmt.Errorf("listing all items: %w", err)
	}
	return toModelProjectItems(items), nil
}

func (b *LocalBackend) ListItemsByProject(projectID string) ([]model.ProjectItemInProject, error) {
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

func (b *LocalBackend) GetItem(id string) (*model.ProjectItemDetail, error) {
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
		depIDs = []string{}
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
		ID:    newID(),
		Title: input.Title,
		Notes: toNullString(input.Notes),
		Repo:  repoToNullString(input.Repo),
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
			return nil, fmt.Errorf("adding item to project %s: %w", pid, err)
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

func (b *LocalBackend) UpdateItem(id string, input model.UpdateProjectItem) (*model.ProjectItem, error) {
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
		Repo:      current.Repo,
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
	if input.Repo != nil {
		params.Repo = repoToNullString(input.Repo)
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

func (b *LocalBackend) DeleteItem(id string) error {
	ctx := b.ctx()
	snapshot, err := captureItem(ctx, b.q, id)
	if err != nil {
		return err
	}

	tx, err := b.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := b.q.WithTx(tx)

	if err := b.logUndo(qtx, "delete", "item", id, snapshot); err != nil {
		return err
	}

	if err := qtx.DeleteItem(ctx, id); err != nil {
		return fmt.Errorf("deleting item: %w", err)
	}

	return tx.Commit()
}

func (b *LocalBackend) ReorderItem(itemID, projectID string, newPosition int) error {
	return b.q.UpdateItemPosition(b.ctx(), generated.UpdateItemPositionParams{
		ItemID:    itemID,
		ProjectID: projectID,
		Position:  int64(newPosition),
	})
}

// --- Multi-project membership ---

func (b *LocalBackend) AddToProject(itemID, projectID string) error {
	return b.q.AddItemToProject(b.ctx(), generated.AddItemToProjectParams{
		ItemID:      itemID,
		ProjectID:   projectID,
		ProjectID_2: projectID,
	})
}

func (b *LocalBackend) RemoveFromProject(itemID, projectID string) error {
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

func (b *LocalBackend) GetItemProjects(itemID string) ([]model.Project, error) {
	ps, err := b.q.GetItemProjects(b.ctx(), itemID)
	if err != nil {
		return nil, fmt.Errorf("getting item projects: %w", err)
	}
	return toModelProjects(ps), nil
}

// --- Dependencies ---

func (b *LocalBackend) AddDependency(itemID, dependsOn string) error {
	ctx := b.ctx()

	deps, err := b.q.GetAllDependencies(ctx)
	if err != nil {
		return fmt.Errorf("getting dependencies for cycle check: %w", err)
	}

	adj := make(map[string][]string)
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

func (b *LocalBackend) RemoveDependency(itemID, dependsOn string) error {
	return b.q.RemoveDependency(b.ctx(), generated.RemoveDependencyParams{
		ItemID:      itemID,
		DependsOnID: dependsOn,
	})
}

func (b *LocalBackend) GetBlockers(itemID string) ([]model.ProjectItem, error) {
	items, err := b.q.GetBlockers(b.ctx(), itemID)
	if err != nil {
		return nil, fmt.Errorf("getting blockers: %w", err)
	}
	return toModelProjectItems(items), nil
}

// --- Tasks ---

func (b *LocalBackend) ListTasks(itemID string) ([]model.ProjectItemTask, error) {
	rows, err := b.q.ListTasksByItem(b.ctx(), itemID)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	out := make([]model.ProjectItemTask, len(rows))
	for i, r := range rows {
		out[i] = toModelProjectItemTask(r)
	}
	return out, nil
}

func (b *LocalBackend) CreateTask(itemID string, input model.CreateProjectItemTask) (*model.ProjectItemTask, error) {
	id := newID()
	t, err := b.q.CreateTask(b.ctx(), generated.CreateTaskParams{
		ID:       id,
		ItemID:   itemID,
		Title:    input.Title,
		ItemID_2: itemID,
	})
	if err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}
	result := toModelProjectItemTask(t)
	return &result, nil
}

func (b *LocalBackend) UpdateTask(itemID, taskID string, input model.UpdateProjectItemTask) (*model.ProjectItemTask, error) {
	ctx := b.ctx()

	current, err := b.q.GetTask(ctx, taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("getting task for update: %w", err)
	}

	params := generated.UpdateTaskParams{
		Title:     current.Title,
		Completed: current.Completed,
		Position:  current.Position,
		ID:        taskID,
	}
	if input.Title != nil {
		params.Title = *input.Title
	}
	if input.Completed != nil {
		params.Completed = boolToInt64(input.Completed)
	}
	if input.Position != nil {
		params.Position = int64(*input.Position)
	}

	t, err := b.q.UpdateTask(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("updating task: %w", err)
	}
	result := toModelProjectItemTask(t)
	return &result, nil
}

func (b *LocalBackend) DeleteTask(itemID, taskID string) error {
	return b.q.DeleteTask(b.ctx(), taskID)
}

func (b *LocalBackend) CompleteTask(itemID, taskID string) error {
	ctx := b.ctx()

	current, err := b.q.GetTask(ctx, taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.ErrNotFound
		}
		return fmt.Errorf("getting task for complete: %w", err)
	}

	completed := int64(1)
	_, err = b.q.UpdateTask(ctx, generated.UpdateTaskParams{
		Title:     current.Title,
		Completed: completed,
		Position:  current.Position,
		ID:        taskID,
	})
	if err != nil {
		return fmt.Errorf("completing task: %w", err)
	}
	return nil
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

func (b *LocalBackend) ListArchived(projectID string) ([]model.ProjectItemInProject, error) {
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

func (b *LocalBackend) Undo() (*model.UndoResult, error) {
	ctx := b.ctx()
	entry, err := b.q.GetLatestUndoLog(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNothingToUndo
		}
		return nil, fmt.Errorf("getting undo log: %w", err)
	}

	tx, err := b.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning undo transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := b.q.WithTx(tx)

	result := &model.UndoResult{
		Description: fmt.Sprintf("undid %s on %s %s", entry.Action, entry.EntityType, shortEntityID(entry.EntityID)),
		Action:      entry.Action,
		EntityType:  entry.EntityType,
		EntityID:    entry.EntityID,
	}

	if entry.EntityType == "project" {
		if err := undoProject(ctx, qtx, entry, result); err != nil {
			return nil, err
		}
		if err := qtx.DeleteUndoLog(ctx, entry.ID); err != nil {
			return nil, fmt.Errorf("deleting undo log: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("committing undo: %w", err)
		}
		return result, nil
	}

	switch entry.Action {
	case "create":
		if err := qtx.DeleteItem(ctx, entry.EntityID); err != nil {
			return nil, fmt.Errorf("undoing create: %w", err)
		}
	case "update":
		if entry.PreviousState.Valid {
			var prev generated.ProjectItem
			if err := json.Unmarshal([]byte(entry.PreviousState.String), &prev); err != nil {
				return nil, fmt.Errorf("unmarshaling previous state: %w", err)
			}
			restored, err := qtx.UpdateItem(ctx, generated.UpdateItemParams{
				Title:     prev.Title,
				Notes:     prev.Notes,
				Repo:      prev.Repo,
				Completed: prev.Completed,
				Archived:  prev.Archived,
				ID:        entry.EntityID,
			})
			if err != nil {
				return nil, fmt.Errorf("restoring previous state: %w", err)
			}
			item := toModelProjectItem(restored)
			result.Restored = &item
		}
	case "delete":
		if entry.PreviousState.Valid {
			snapshot, err := decodeItemSnapshot(entry.PreviousState.String)
			if err != nil {
				return nil, err
			}
			if err := restoreItemSnapshot(ctx, qtx, snapshot); err != nil {
				return nil, err
			}
			item := toModelProjectItem(snapshot.Item)
			result.Restored = &item
			for _, membership := range snapshot.Memberships {
				result.RestoredProjectIDs = append(result.RestoredProjectIDs, membership.ProjectID)
			}
			for _, dependency := range snapshot.Dependencies {
				result.RestoredDependencies = append(result.RestoredDependencies, model.ItemDependency{
					ItemID:      dependency.ItemID,
					DependsOnID: dependency.DependsOnID,
				})
			}
			for _, task := range snapshot.Tasks {
				result.RestoredTasks = append(result.RestoredTasks, toModelProjectItemTask(task))
			}
		}
	}

	if err := qtx.DeleteUndoLog(ctx, entry.ID); err != nil {
		return nil, fmt.Errorf("deleting undo log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing undo: %w", err)
	}

	return result, nil
}

// undoProject reverses a logged project mutation.
func undoProject(ctx context.Context, qtx *generated.Queries, entry generated.UndoLog, result *model.UndoResult) error {
	switch entry.Action {
	case "create":
		if err := qtx.DeleteProject(ctx, entry.EntityID); err != nil {
			return fmt.Errorf("undoing project create: %w", err)
		}
	case "update":
		if entry.PreviousState.Valid {
			var prev generated.Project
			if err := json.Unmarshal([]byte(entry.PreviousState.String), &prev); err != nil {
				return fmt.Errorf("unmarshaling previous project state: %w", err)
			}
			restored, err := qtx.UpdateProject(ctx, generated.UpdateProjectParams{
				Name:        prev.Name,
				Description: prev.Description,
				Position:    prev.Position,
				ID:          entry.EntityID,
			})
			if err != nil {
				return fmt.Errorf("restoring previous project state: %w", err)
			}
			project := toModelProject(restored)
			result.RestoredProject = &project
		}
	case "delete":
		if entry.PreviousState.Valid {
			snapshot, err := decodeProjectSnapshot(entry.PreviousState.String)
			if err != nil {
				return err
			}
			if err := restoreProjectSnapshot(ctx, qtx, snapshot); err != nil {
				return err
			}
			project := toModelProject(snapshot.Project)
			result.RestoredProject = &project
			for _, membership := range snapshot.Memberships {
				result.RestoredItemIDs = append(result.RestoredItemIDs, membership.ItemID)
			}
		}
	}
	return nil
}

// UUIDv7 front-loads the millisecond timestamp, so a prefix is identical for
// everything created in the same ~65s window. Matches the CLI and TUI display.
func shortEntityID(id string) string {
	if len(id) >= 8 {
		return id[len(id)-8:]
	}
	return id
}

func (b *LocalBackend) CanUndo() (bool, error) {
	count, err := b.q.CountUndoLogs(b.ctx())
	if err != nil {
		return false, fmt.Errorf("counting undo logs: %w", err)
	}
	return count > 0, nil
}

// itemSnapshot is everything a delete destroys. The row alone is not enough
// to undo one: memberships, dependencies, and tasks all cascade. An item
// restored without its memberships belongs to no project, which the TUI
// cannot show at all — it builds every view by walking projects — while the
// CLI's flat listing still returns it.
type itemSnapshot struct {
	Item         generated.ProjectItem             `json:"item"`
	Memberships  []generated.ProjectItemMembership `json:"memberships"`
	Dependencies []generated.ProjectItemDependency `json:"dependencies"`
	Tasks        []generated.ProjectItemTask       `json:"tasks"`
}

// projectSnapshot is the project equivalent. Memberships cascade off a
// project delete; the items on the other end of them do not.
type projectSnapshot struct {
	Project     generated.Project                 `json:"project"`
	Memberships []generated.ProjectItemMembership `json:"memberships"`
}

func captureItem(ctx context.Context, q *generated.Queries, id string) (*itemSnapshot, error) {
	item, err := q.GetItem(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("getting item for delete: %w", err)
	}
	memberships, err := q.GetItemMemberships(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("capturing item memberships: %w", err)
	}
	dependencies, err := q.GetDependenciesInvolvingItem(ctx, generated.GetDependenciesInvolvingItemParams{
		ItemID:      id,
		DependsOnID: id,
	})
	if err != nil {
		return nil, fmt.Errorf("capturing item dependencies: %w", err)
	}
	tasks, err := q.ListTasksByItem(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("capturing item tasks: %w", err)
	}
	return &itemSnapshot{
		Item:         item,
		Memberships:  memberships,
		Dependencies: dependencies,
		Tasks:        tasks,
	}, nil
}

func captureProject(ctx context.Context, q *generated.Queries, id string) (*projectSnapshot, error) {
	project, err := q.GetProject(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("getting project for delete: %w", err)
	}
	memberships, err := q.GetProjectMemberships(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("capturing project memberships: %w", err)
	}
	return &projectSnapshot{Project: project, Memberships: memberships}, nil
}

// decodeItemSnapshot accepts either shape of previous_state: the snapshot
// written now, or the bare item row written before snapshots existed. Undo
// logs are not pruned, so pre-existing databases still hold the old shape.
func decodeItemSnapshot(raw string) (*itemSnapshot, error) {
	var snapshot itemSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err == nil && snapshot.Item.ID != "" {
		return &snapshot, nil
	}
	var item generated.ProjectItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		return nil, fmt.Errorf("unmarshaling deleted item state: %w", err)
	}
	return &itemSnapshot{Item: item}, nil
}

func decodeProjectSnapshot(raw string) (*projectSnapshot, error) {
	var snapshot projectSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err == nil && snapshot.Project.ID != "" {
		return &snapshot, nil
	}
	var project generated.Project
	if err := json.Unmarshal([]byte(raw), &project); err != nil {
		return nil, fmt.Errorf("unmarshaling deleted project state: %w", err)
	}
	return &projectSnapshot{Project: project}, nil
}

// restoreItemSnapshot puts the row back with its original timestamps, then
// reattaches whatever cascaded off it.
func restoreItemSnapshot(ctx context.Context, qtx *generated.Queries, snapshot *itemSnapshot) error {
	if err := qtx.UpsertItem(ctx, generated.UpsertItemParams(snapshot.Item)); err != nil {
		return fmt.Errorf("recreating deleted item: %w", err)
	}
	for _, membership := range snapshot.Memberships {
		if err := qtx.RestoreMembership(ctx, generated.RestoreMembershipParams(membership)); err != nil {
			return fmt.Errorf("restoring item membership: %w", err)
		}
	}
	for _, dependency := range snapshot.Dependencies {
		if err := qtx.UpsertDependency(ctx, generated.UpsertDependencyParams(dependency)); err != nil {
			return fmt.Errorf("restoring item dependency: %w", err)
		}
	}
	for _, task := range snapshot.Tasks {
		if err := qtx.UpsertTask(ctx, generated.UpsertTaskParams(task)); err != nil {
			return fmt.Errorf("restoring item task: %w", err)
		}
	}
	return nil
}

func restoreProjectSnapshot(ctx context.Context, qtx *generated.Queries, snapshot *projectSnapshot) error {
	if err := qtx.UpsertProject(ctx, generated.UpsertProjectParams(snapshot.Project)); err != nil {
		return fmt.Errorf("recreating deleted project: %w", err)
	}
	for _, membership := range snapshot.Memberships {
		if err := qtx.RestoreMembership(ctx, generated.RestoreMembershipParams(membership)); err != nil {
			return fmt.Errorf("restoring project membership: %w", err)
		}
	}
	return nil
}

func (b *LocalBackend) logUndo(qtx *generated.Queries, action, entityType string, entityID string, previousState any) error {
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
