CREATE TABLE projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE todos (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    notes TEXT,
    due_date TEXT,
    completed INTEGER NOT NULL DEFAULT 0,
    archived INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE todo_projects (
    todo_id INTEGER NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (todo_id, project_id)
);

CREATE TABLE dependencies (
    todo_id INTEGER NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    depends_on_id INTEGER NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    PRIMARY KEY (todo_id, depends_on_id),
    CHECK (todo_id != depends_on_id)
);

CREATE TABLE undo_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id INTEGER NOT NULL,
    previous_state TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_todo_projects_project ON todo_projects(project_id);
CREATE INDEX idx_todo_projects_todo ON todo_projects(todo_id);
CREATE INDEX idx_todo_projects_position ON todo_projects(project_id, position);
CREATE INDEX idx_todos_due ON todos(due_date) WHERE due_date IS NOT NULL;
CREATE INDEX idx_todos_active ON todos(archived) WHERE archived = 0;
CREATE INDEX idx_deps_todo ON dependencies(todo_id);
CREATE INDEX idx_deps_depends ON dependencies(depends_on_id);
CREATE INDEX idx_undo_recent ON undo_log(created_at DESC);
