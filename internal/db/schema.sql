-- Schema definition for sqlc (mirrors migrations/001_initial.sql)

CREATE TABLE projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE project_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    notes TEXT,
    completed INTEGER NOT NULL DEFAULT 0,
    archived INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE project_item_memberships (
    item_id INTEGER NOT NULL REFERENCES project_items(id) ON DELETE CASCADE,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, project_id)
);

CREATE TABLE project_item_dependencies (
    item_id INTEGER NOT NULL REFERENCES project_items(id) ON DELETE CASCADE,
    depends_on_id INTEGER NOT NULL REFERENCES project_items(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, depends_on_id),
    CHECK (item_id != depends_on_id)
);

CREATE TABLE undo_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id INTEGER NOT NULL,
    previous_state TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
