-- name: ListProjectsWithItemCount :many
SELECT p.*, COUNT(pi.id) AS item_count
FROM projects p
LEFT JOIN project_item_memberships m ON p.id = m.project_id
LEFT JOIN project_items pi ON m.item_id = pi.id AND pi.archived = 0
GROUP BY p.id
ORDER BY p.position, p.name;

-- name: GetProject :one
SELECT * FROM projects WHERE id = ?;

-- name: GetProjectWithItemCount :one
SELECT p.*, COUNT(pi.id) AS item_count
FROM projects p
LEFT JOIN project_item_memberships m ON p.id = m.project_id
LEFT JOIN project_items pi ON m.item_id = pi.id AND pi.archived = 0
WHERE p.id = ?
GROUP BY p.id;

-- name: CreateProject :one
INSERT INTO projects (id, name, description, position)
VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM projects))
RETURNING *;

-- name: UpdateProject :one
UPDATE projects
SET name = ?,
    description = ?,
    position = ?
WHERE id = ?
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = ?;

-- name: ListAllItems :many
SELECT * FROM project_items
WHERE archived = 0
ORDER BY created_at DESC;

-- name: ListItemsByProject :many
SELECT pi.*, m.position AS membership_position,
    (SELECT COUNT(*) FROM project_item_memberships m2 WHERE m2.item_id = pi.id) AS project_count
FROM project_items pi
JOIN project_item_memberships m ON pi.id = m.item_id
WHERE m.project_id = ? AND pi.archived = 0
ORDER BY m.position, pi.created_at;

-- name: GetItem :one
SELECT * FROM project_items WHERE id = ?;

-- name: CreateItem :one
INSERT INTO project_items (id, title, notes)
VALUES (?, ?, ?)
RETURNING *;

-- name: UpdateItem :one
UPDATE project_items
SET title = ?,
    notes = ?,
    completed = ?,
    archived = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?
RETURNING *;

-- name: DeleteItem :exec
DELETE FROM project_items WHERE id = ?;

-- name: AddItemToProject :exec
INSERT INTO project_item_memberships (item_id, project_id, position)
VALUES (?, ?, (SELECT COALESCE(MAX(m.position), 0) + 1 FROM project_item_memberships m WHERE m.project_id = ?));

-- name: RemoveItemFromProject :exec
DELETE FROM project_item_memberships WHERE item_id = ? AND project_id = ?;

-- name: GetItemProjects :many
SELECT p.*
FROM projects p
JOIN project_item_memberships m ON p.id = m.project_id
WHERE m.item_id = ?
ORDER BY p.name;

-- name: UpdateItemPosition :exec
UPDATE project_item_memberships SET position = ? WHERE item_id = ? AND project_id = ?;

-- name: AddDependency :exec
INSERT INTO project_item_dependencies (item_id, depends_on_id) VALUES (?, ?);

-- name: RemoveDependency :exec
DELETE FROM project_item_dependencies WHERE item_id = ? AND depends_on_id = ?;

-- name: GetBlockers :many
SELECT pi.*
FROM project_items pi
JOIN project_item_dependencies d ON pi.id = d.depends_on_id
WHERE d.item_id = ? AND pi.completed = 0;

-- name: GetDependencyIDs :many
SELECT depends_on_id FROM project_item_dependencies WHERE item_id = ?;

-- name: GetAllDependencies :many
SELECT * FROM project_item_dependencies;

-- name: SearchItems :many
SELECT * FROM project_items
WHERE (title LIKE '%' || ? || '%' OR notes LIKE '%' || ? || '%')
  AND archived = 0
ORDER BY created_at DESC;

-- name: ListBlockedItems :many
SELECT DISTINCT pi.*
FROM project_items pi
JOIN project_item_dependencies d ON pi.id = d.item_id
JOIN project_items blocker ON d.depends_on_id = blocker.id
WHERE blocker.completed = 0
  AND pi.completed = 0
  AND pi.archived = 0;

-- name: ListArchivedItems :many
SELECT pi.*, m.position AS membership_position
FROM project_items pi
JOIN project_item_memberships m ON pi.id = m.item_id
WHERE m.project_id = ? AND pi.archived = 1
ORDER BY pi.updated_at DESC;

-- name: ListTasksByItem :many
SELECT * FROM project_item_tasks
WHERE item_id = ?
ORDER BY position, created_at;

-- name: GetTask :one
SELECT * FROM project_item_tasks WHERE id = ?;

-- name: CreateTask :one
INSERT INTO project_item_tasks (id, item_id, title, position)
VALUES (?, ?, ?, (SELECT COALESCE(MAX(t.position), 0) + 1 FROM project_item_tasks t WHERE t.item_id = ?))
RETURNING *;

-- name: UpdateTask :one
UPDATE project_item_tasks
SET title = ?,
    completed = ?,
    position = ?
WHERE id = ?
RETURNING *;

-- name: DeleteTask :exec
DELETE FROM project_item_tasks WHERE id = ?;

-- Sync: pending operations

-- name: InsertPendingSync :exec
INSERT INTO pending_sync (operation, entity_type, entity_id, payload)
VALUES (?, ?, ?, ?);

-- name: ListPendingSync :many
SELECT * FROM pending_sync ORDER BY id ASC;

-- name: GetOldestPendingSync :one
SELECT * FROM pending_sync ORDER BY id ASC LIMIT 1;

-- name: DeletePendingSync :exec
DELETE FROM pending_sync WHERE id = ?;

-- name: UpdatePendingSyncError :exec
UPDATE pending_sync SET attempts = attempts + 1, last_error = ? WHERE id = ?;

-- name: CountPendingSync :one
SELECT COUNT(*) FROM pending_sync;

-- name: DeleteAllPendingSync :exec
DELETE FROM pending_sync;

-- Sync: state tracking

-- name: GetSyncState :one
SELECT * FROM sync_state WHERE entity_type = ?;

-- name: UpsertSyncState :exec
INSERT INTO sync_state (entity_type, last_pull_at, last_push_at)
VALUES (?, ?, ?)
ON CONFLICT(entity_type) DO UPDATE SET
    last_pull_at = CASE WHEN excluded.last_pull_at > sync_state.last_pull_at THEN excluded.last_pull_at ELSE sync_state.last_pull_at END,
    last_push_at = CASE WHEN excluded.last_push_at > sync_state.last_push_at THEN excluded.last_push_at ELSE sync_state.last_push_at END;

-- Sync: pull reconciliation (upserts)

-- name: UpsertProject :exec
INSERT INTO projects (id, name, description, position, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    description = excluded.description,
    position = excluded.position;

-- name: UpsertItem :exec
INSERT INTO project_items (id, title, notes, completed, archived, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    notes = excluded.notes,
    completed = excluded.completed,
    archived = excluded.archived,
    updated_at = excluded.updated_at;

-- name: UpsertTask :exec
INSERT INTO project_item_tasks (id, item_id, title, completed, position, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    completed = excluded.completed,
    position = excluded.position;

-- name: UpsertMembership :exec
INSERT INTO project_item_memberships (item_id, project_id, position)
VALUES (?, ?, ?)
ON CONFLICT(item_id, project_id) DO UPDATE SET
    position = excluded.position;

-- name: UpsertDependency :exec
INSERT OR IGNORE INTO project_item_dependencies (item_id, depends_on_id)
VALUES (?, ?);

-- name: DeleteAllMemberships :exec
DELETE FROM project_item_memberships;

-- name: DeleteAllDependencies :exec
DELETE FROM project_item_dependencies;

-- name: ListAllProjectsRaw :many
SELECT * FROM projects ORDER BY position, name;

-- name: ListAllItemsRaw :many
SELECT * FROM project_items ORDER BY created_at DESC;

-- name: ListAllMemberships :many
SELECT * FROM project_item_memberships;

-- name: ListAllTasks :many
SELECT * FROM project_item_tasks ORDER BY item_id, position;

-- Undo

-- name: InsertUndoLog :exec
INSERT INTO undo_log (action, entity_type, entity_id, previous_state)
VALUES (?, ?, ?, ?);

-- name: GetLatestUndoLog :one
SELECT * FROM undo_log ORDER BY id DESC LIMIT 1;

-- name: DeleteUndoLog :exec
DELETE FROM undo_log WHERE id = ?;

-- name: CountUndoLogs :one
SELECT COUNT(*) FROM undo_log;
