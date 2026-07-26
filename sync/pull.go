package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/datapointchris/todoui/db/generated"
	"github.com/datapointchris/todoui/model"
)

// LastPullAt reports when the local database last reconciled with the server,
// or the zero time if it never has.
func (e *Engine) LastPullAt() (time.Time, error) {
	state, err := e.q.GetSyncState(e.ctx, "item")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("reading sync state: %w", err)
	}
	stamp, err := time.Parse(time.RFC3339Nano, state.LastPullAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing last pull time %q: %w", state.LastPullAt, err)
	}
	return stamp, nil
}

// PullIfStale reconciles only when the last pull is older than maxAge, and
// reports whether it pulled. A full pull costs 2+2N requests — one per item for
// its detail and again for its tasks — so running it ahead of every CLI
// invocation would put dozens of round trips in front of a command that prints
// four lines.
func (e *Engine) PullIfStale(ctx context.Context, maxAge time.Duration) (bool, error) {
	last, err := e.LastPullAt()
	if err != nil {
		return false, err
	}
	if time.Since(last) < maxAge {
		return false, nil
	}
	return true, e.Pull(ctx)
}

// Pull fetches all data from the remote API and reconciles it with the local database.
// It attempts to push pending ops first (so the server has our changes), then does a
// full pull. Server state wins on conflicts.
//
// A failure is recorded on the status so the TUI's indicator reflects it. Pull runs
// unattended on a timer, and an error only a foreground caller can see would leave
// the bar reading SYNCED while nothing had reconciled for hours.
func (e *Engine) Pull(ctx context.Context) error {
	e.beginSync()
	defer e.endSync()

	err := e.pull(ctx)
	e.setStatus(func(s *SyncStatus) {
		s.Connected = err == nil
		if err != nil {
			s.LastError = err.Error()
		} else {
			s.LastError = ""
		}
	})
	return err
}

func (e *Engine) pull(ctx context.Context) error {
	// Push first: try to drain pending ops so server has our changes
	e.drainPendingOps()

	// Everything queued from here on describes a local change the fetch below
	// cannot know about. Recording the high-water mark lets the reconciliation
	// clear only what it actually superseded — see the delete calls at the end.
	pushedThrough, err := e.q.MaxPendingSyncID(ctx)
	if err != nil {
		return fmt.Errorf("reading pending sync high-water mark: %w", err)
	}

	// Fetch all data from the server
	projects, err := fetchJSON[[]model.Project](ctx, e.client, e.apiURL, "/projects/")
	if err != nil {
		return fmt.Errorf("pulling projects: %w", err)
	}

	items, err := fetchJSON[[]model.ProjectItem](ctx, e.client, e.apiURL, "/project-items/")
	if err != nil {
		return fmt.Errorf("pulling items: %w", err)
	}

	// For each item, fetch detail (memberships + deps) and tasks
	type itemExtra struct {
		detail *model.ProjectItemDetail
		tasks  []model.ProjectItemTask
	}
	extras := make(map[string]itemExtra, len(items))

	for _, item := range items {
		detail, err := fetchJSON[*model.ProjectItemDetail](ctx, e.client, e.apiURL, fmt.Sprintf("/project-items/%s/", item.ID))
		if err != nil {
			return fmt.Errorf("pulling item %s detail: %w", item.ID, err)
		}

		tasks, err := fetchJSON[[]model.ProjectItemTask](ctx, e.client, e.apiURL, fmt.Sprintf("/project-items/%s/tasks/", item.ID))
		if err != nil {
			return fmt.Errorf("pulling item %s tasks: %w", item.ID, err)
		}

		extras[item.ID] = itemExtra{detail: detail, tasks: tasks}
	}

	// Reconcile in a transaction
	tx, err := e.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning sync transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := e.q.WithTx(tx)

	// Entities changed locally while the fetch was in flight. The server response
	// predates them, so the sweeps below would read them as "deleted upstream" and
	// erase a row the user created seconds ago. Ids are UUIDv7 and unique across
	// tables, so one set covers projects, items, and tasks alike.
	pendingIDs, err := qtx.ListPendingSyncEntityIDsAfter(ctx, pushedThrough)
	if err != nil {
		return fmt.Errorf("listing in-flight sync entities: %w", err)
	}
	queuedLocally := make(map[string]bool, len(pendingIDs))
	for _, id := range pendingIDs {
		queuedLocally[id] = true
	}

	// Upsert projects
	serverProjectIDs := make(map[string]bool, len(projects))
	for _, p := range projects {
		serverProjectIDs[p.ID] = true
		if err := qtx.UpsertProject(ctx, generated.UpsertProjectParams{
			ID:          p.ID,
			Name:        p.Name,
			Description: nullStr(p.Description),
			Position:    int64(p.Position),
			CreatedAt:   p.CreatedAt.Format(time.RFC3339Nano),
		}); err != nil {
			return fmt.Errorf("upserting project %s: %w", p.ID, err)
		}
	}

	// Delete local projects not on server
	localProjects, err := qtx.ListAllProjectsRaw(ctx)
	if err != nil {
		return fmt.Errorf("listing local projects: %w", err)
	}
	for _, lp := range localProjects {
		if !serverProjectIDs[lp.ID] && !queuedLocally[lp.ID] {
			if err := qtx.DeleteProject(ctx, lp.ID); err != nil {
				return fmt.Errorf("deleting stale project %s: %w", lp.ID, err)
			}
		}
	}

	// Upsert items
	serverItemIDs := make(map[string]bool, len(items))
	for _, item := range items {
		serverItemIDs[item.ID] = true
		if err := qtx.UpsertItem(ctx, generated.UpsertItemParams{
			ID:        item.ID,
			Title:     item.Title,
			Notes:     nullStr(item.Notes),
			Repo:      nullStr(item.Repo),
			Completed: boolToInt(item.Completed),
			Archived:  boolToInt(item.Archived),
			CreatedAt: item.CreatedAt.Format(time.RFC3339Nano),
			UpdatedAt: item.UpdatedAt.Format(time.RFC3339Nano),
		}); err != nil {
			return fmt.Errorf("upserting item %s: %w", item.ID, err)
		}
	}

	// Delete local items not on server
	localItems, err := qtx.ListAllItemsRaw(ctx)
	if err != nil {
		return fmt.Errorf("listing local items: %w", err)
	}
	for _, li := range localItems {
		if !serverItemIDs[li.ID] && !queuedLocally[li.ID] {
			if err := qtx.DeleteItem(ctx, li.ID); err != nil {
				return fmt.Errorf("deleting stale item %s: %w", li.ID, err)
			}
		}
	}

	// Replace all memberships with server state
	if err := qtx.DeleteAllMemberships(ctx); err != nil {
		return fmt.Errorf("clearing memberships: %w", err)
	}
	for _, extra := range extras {
		if extra.detail == nil {
			continue
		}
		for _, proj := range extra.detail.Projects {
			if err := qtx.UpsertMembership(ctx, generated.UpsertMembershipParams{
				ItemID:    extra.detail.ID,
				ProjectID: proj.ID,
				Position:  0, // server doesn't return per-project position in detail
			}); err != nil {
				return fmt.Errorf("upserting membership: %w", err)
			}
		}
	}

	// Replace all dependencies with server state
	if err := qtx.DeleteAllDependencies(ctx); err != nil {
		return fmt.Errorf("clearing dependencies: %w", err)
	}
	for _, extra := range extras {
		if extra.detail == nil {
			continue
		}
		for _, depID := range extra.detail.DependencyIDs {
			if err := qtx.UpsertDependency(ctx, generated.UpsertDependencyParams{
				ItemID:      extra.detail.ID,
				DependsOnID: depID,
			}); err != nil {
				return fmt.Errorf("upserting dependency: %w", err)
			}
		}
	}

	// Upsert tasks and delete stale ones
	serverTaskIDs := make(map[string]bool)
	for _, extra := range extras {
		for _, task := range extra.tasks {
			serverTaskIDs[task.ID] = true
			if err := qtx.UpsertTask(ctx, generated.UpsertTaskParams{
				ID:        task.ID,
				ItemID:    task.ItemID,
				Title:     task.Title,
				Completed: boolToInt(task.Completed),
				Position:  int64(task.Position),
				CreatedAt: task.CreatedAt.Format(time.RFC3339Nano),
			}); err != nil {
				return fmt.Errorf("upserting task %s: %w", task.ID, err)
			}
		}
	}

	localTasks, err := qtx.ListAllTasks(ctx)
	if err != nil {
		return fmt.Errorf("listing local tasks: %w", err)
	}
	for _, lt := range localTasks {
		if !serverTaskIDs[lt.ID] && !queuedLocally[lt.ID] {
			if err := qtx.DeleteTask(ctx, lt.ID); err != nil {
				return fmt.Errorf("deleting stale task %s: %w", lt.ID, err)
			}
		}
	}

	// Clear only the ops this reconciliation superseded. Anything queued after the
	// high-water mark postdates the server response and still has to be pushed.
	if err := qtx.DeletePendingSyncUpTo(ctx, pushedThrough); err != nil {
		return fmt.Errorf("clearing pending sync: %w", err)
	}

	// Update sync state
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, entityType := range []string{"project", "item", "task"} {
		if err := qtx.UpsertSyncState(ctx, generated.UpsertSyncStateParams{
			EntityType: entityType,
			LastPullAt: now,
			LastPushAt: now,
		}); err != nil {
			return fmt.Errorf("updating sync state: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing sync: %w", err)
	}

	// Ops that survived the truncation were queued mid-pull and are still unsent.
	if len(queuedLocally) > 0 {
		e.Notify()
	}
	return nil
}

// fetchJSON performs a GET request and decodes the JSON response into T.
func fetchJSON[T any](ctx context.Context, client *http.Client, baseURL, path string) (T, error) {
	var zero T

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return zero, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return zero, friendlyNetErr(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return zero, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return zero, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}

func nullStr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
