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
INSERT INTO projects (name, position)
VALUES (?, (SELECT COALESCE(MAX(position), 0) + 1 FROM projects))
RETURNING *;

-- name: UpdateProject :one
UPDATE projects
SET name = ?,
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
INSERT INTO project_items (title, notes)
VALUES (?, ?)
RETURNING *;

-- name: UpdateItem :one
UPDATE project_items
SET title = ?,
    notes = ?,
    completed = ?,
    archived = ?,
    updated_at = datetime('now')
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

-- name: InsertUndoLog :exec
INSERT INTO undo_log (action, entity_type, entity_id, previous_state)
VALUES (?, ?, ?, ?);

-- name: GetLatestUndoLog :one
SELECT * FROM undo_log ORDER BY id DESC LIMIT 1;

-- name: DeleteUndoLog :exec
DELETE FROM undo_log WHERE id = ?;

-- name: CountUndoLogs :one
SELECT COUNT(*) FROM undo_log;
