-- name: ListProjects :many
SELECT * FROM projects ORDER BY position, name;

-- name: GetProject :one
SELECT * FROM projects WHERE id = ?;

-- name: CreateProject :one
INSERT INTO projects (name, position)
VALUES (?, (SELECT COALESCE(MAX(position), 0) + 1 FROM projects))
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = ?;

-- name: UpdateProjectPosition :exec
UPDATE projects SET position = ? WHERE id = ?;

-- name: ListTodosByProject :many
SELECT t.*
FROM todos t
JOIN todo_projects tp ON t.id = tp.todo_id
WHERE tp.project_id = ? AND t.archived = 0
ORDER BY tp.position, t.created_at;

-- name: GetTodo :one
SELECT * FROM todos WHERE id = ?;

-- name: CreateTodo :one
INSERT INTO todos (title, notes, due_date)
VALUES (?, ?, ?)
RETURNING *;

-- name: UpdateTodo :one
UPDATE todos
SET title = COALESCE(?, title),
    notes = COALESCE(?, notes),
    due_date = COALESCE(?, due_date),
    completed = COALESCE(?, completed),
    archived = COALESCE(?, archived),
    updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: DeleteTodo :exec
DELETE FROM todos WHERE id = ?;

-- name: AddTodoToProject :exec
INSERT INTO todo_projects (todo_id, project_id, position)
VALUES (?, ?, (SELECT COALESCE(MAX(tp.position), 0) + 1 FROM todo_projects tp WHERE tp.project_id = ?));

-- name: RemoveTodoFromProject :exec
DELETE FROM todo_projects WHERE todo_id = ? AND project_id = ?;

-- name: GetTodoProjects :many
SELECT p.*
FROM projects p
JOIN todo_projects tp ON p.id = tp.project_id
WHERE tp.todo_id = ?
ORDER BY p.name;

-- name: UpdateTodoPosition :exec
UPDATE todo_projects SET position = ? WHERE todo_id = ? AND project_id = ?;

-- name: AddDependency :exec
INSERT INTO dependencies (todo_id, depends_on_id) VALUES (?, ?);

-- name: RemoveDependency :exec
DELETE FROM dependencies WHERE todo_id = ? AND depends_on_id = ?;

-- name: GetBlockers :many
SELECT t.*
FROM todos t
JOIN dependencies d ON t.id = d.depends_on_id
WHERE d.todo_id = ? AND t.completed = 0;

-- name: GetAllDependencies :many
SELECT * FROM dependencies;

-- name: SearchTodos :many
SELECT * FROM todos
WHERE (title LIKE '%' || ? || '%' OR notes LIKE '%' || ? || '%')
  AND archived = 0
ORDER BY created_at DESC;

-- name: ListTodosToday :many
SELECT * FROM todos
WHERE due_date IS NOT NULL
  AND due_date <= date('now')
  AND completed = 0
  AND archived = 0
ORDER BY due_date, created_at;

-- name: ListBlockedTodos :many
SELECT DISTINCT t.*
FROM todos t
JOIN dependencies d ON t.id = d.todo_id
JOIN todos blocker ON d.depends_on_id = blocker.id
WHERE blocker.completed = 0
  AND t.completed = 0
  AND t.archived = 0;

-- name: ListArchivedTodos :many
SELECT t.*
FROM todos t
JOIN todo_projects tp ON t.id = tp.todo_id
WHERE tp.project_id = ? AND t.archived = 1
ORDER BY t.updated_at DESC;

-- name: InsertUndoLog :exec
INSERT INTO undo_log (action, entity_type, entity_id, previous_state)
VALUES (?, ?, ?, ?);

-- name: GetLatestUndoLog :one
SELECT * FROM undo_log ORDER BY id DESC LIMIT 1;

-- name: DeleteUndoLog :exec
DELETE FROM undo_log WHERE id = ?;

-- name: CountUndoLogs :one
SELECT COUNT(*) FROM undo_log;
