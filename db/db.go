package db

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

//go:embed indexes.sql
var indexes string

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

	if _, err := db.Exec(indexes); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating indexes: %w", err)
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
	if err := migrateItemRepo(db); err != nil {
		return err
	}
	if err := migrateItemNumber(db); err != nil {
		return err
	}
	if err := migrateProjectStatus(db); err != nil {
		return err
	}
	return nil
}

// migrateProjectStatus adds the project lifecycle columns and, critically, drops
// the global UNIQUE on projects.name in favor of the partial index over active
// projects in indexes.sql.
//
// The unique constraint is why this is a table rebuild rather than three ALTER
// TABLE ADD COLUMNs. SQLite attaches it as an implicit index that no DDL can
// drop, and leaving it would fail the entire pull transaction the first time the
// server returned a completed project beside a live one sharing its name —
// exactly the case the partial index exists to allow.
//
// Foreign keys are off for the swap because project_item_memberships references
// projects ON DELETE CASCADE, so dropping the old table with them on would take
// every membership with it. PRAGMA foreign_keys is a no-op inside a transaction,
// hence the explicit ordering below; this is SQLite's own documented procedure
// for altering a table it otherwise cannot.
func migrateProjectStatus(db *sql.DB) error {
	var name string
	err := db.QueryRow("SELECT name FROM pragma_table_info('projects') WHERE name = 'status'").Scan(&name)
	if err == nil {
		return nil
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("checking projects.status: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		return fmt.Errorf("disabling foreign keys for the projects rebuild: %w", err)
	}
	// Restored even on the error paths: leaving the connection with foreign keys
	// off would silently disable every cascade for the rest of the process.
	defer func() { _, _ = db.Exec("PRAGMA foreign_keys=ON") }()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning the projects rebuild: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const rebuild = `
CREATE TABLE projects_rebuilt (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    status_reason TEXT,
    closed_at TEXT,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO projects_rebuilt (id, name, description, position, created_at)
    SELECT id, name, description, position, created_at FROM projects;
DROP TABLE projects;
ALTER TABLE projects_rebuilt RENAME TO projects;`
	if _, err := tx.Exec(rebuild); err != nil {
		return fmt.Errorf("rebuilding projects: %w", err)
	}

	var violations int
	if err := tx.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil {
		return fmt.Errorf("checking foreign keys after the projects rebuild: %w", err)
	}
	if violations > 0 {
		return fmt.Errorf("projects rebuild left %d dangling foreign key rows", violations)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing the projects rebuild: %w", err)
	}
	return nil
}

// migrateItemNumber adds project_items.number to databases created before the
// column existed. Left null rather than backfilled: with sync on the server
// owns the numbering and the next pull fills every row in, and with sync off
// the first create allocates from an empty column, which starts at 1.
//
// The UNIQUE constraint lives in indexes.sql because ALTER TABLE ADD COLUMN
// cannot carry one.
func migrateItemNumber(db *sql.DB) error {
	var name string
	err := db.QueryRow("SELECT name FROM pragma_table_info('project_items') WHERE name = 'number'").Scan(&name)
	if err == sql.ErrNoRows {
		if _, err := db.Exec("ALTER TABLE project_items ADD COLUMN number INTEGER"); err != nil {
			return fmt.Errorf("adding project_items.number: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("checking project_items.number: %w", err)
	}
	return nil
}

// migrateItemRepo adds project_items.repo to databases created before the
// column existed. Detected by column presence rather than a version counter,
// matching how migrateSyncTables probes for its tables.
func migrateItemRepo(db *sql.DB) error {
	var name string
	err := db.QueryRow("SELECT name FROM pragma_table_info('project_items') WHERE name = 'repo'").Scan(&name)
	if err == sql.ErrNoRows {
		if _, err := db.Exec("ALTER TABLE project_items ADD COLUMN repo TEXT"); err != nil {
			return fmt.Errorf("adding project_items.repo: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("checking project_items.repo: %w", err)
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
