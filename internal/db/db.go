package db

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Open creates a SQLite connection and ensures the schema exists.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// In-memory databases are per-connection in SQLite. Without this,
	// Go's connection pool hands out separate blank databases to each goroutine.
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	// Enable WAL mode and foreign keys
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("setting %s: %w", pragma, err)
		}
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	// Check if schema already exists by looking for the projects table
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='project_items'").Scan(&name)
	if err == sql.ErrNoRows {
		// Fresh database — apply full schema (includes sync tables)
		if _, err := db.Exec(schema); err != nil {
			return fmt.Errorf("applying schema: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("checking schema: %w", err)
	}

	// Existing database — apply incremental migrations
	if err := migrateSyncTables(db); err != nil {
		return err
	}
	return nil
}

func migrateSyncTables(db *sql.DB) error {
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='pending_sync'").Scan(&name)
	if err == sql.ErrNoRows {
		const syncMigration = `
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
);`
		if _, err := db.Exec(syncMigration); err != nil {
			return fmt.Errorf("applying sync migration: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("checking sync tables: %w", err)
	}
	return nil
}
