-- Schema definition for sqlc

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE project_items (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    notes TEXT,
    completed INTEGER NOT NULL DEFAULT 0,
    archived INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE project_item_memberships (
    item_id TEXT NOT NULL REFERENCES project_items(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, project_id)
);

CREATE TABLE project_item_dependencies (
    item_id TEXT NOT NULL REFERENCES project_items(id) ON DELETE CASCADE,
    depends_on_id TEXT NOT NULL REFERENCES project_items(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, depends_on_id),
    CHECK (item_id != depends_on_id)
);

CREATE TABLE project_item_tasks (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL REFERENCES project_items(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    completed INTEGER NOT NULL DEFAULT 0,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE pending_sync (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT
);

CREATE TABLE sync_state (
    entity_type TEXT PRIMARY KEY,
    last_pull_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00.000Z',
    last_push_at TEXT NOT NULL DEFAULT '1970-01-01T00:00:00.000Z'
);

CREATE TABLE undo_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    previous_state TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
