package db

import (
	"database/sql"
	"testing"
)

func TestOpen_FreshDatabase(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = database.Close() }()

	// All tables should exist
	tables := []string{
		"projects",
		"project_items",
		"project_item_memberships",
		"project_item_dependencies",
		"project_item_tasks",
		"pending_sync",
		"sync_state",
		"undo_log",
	}
	for _, table := range tables {
		var name string
		err := database.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestOpen_ForeignKeysEnabled(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = database.Close() }()

	var fk int
	if err := database.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("checking foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("expected foreign_keys=1, got %d", fk)
	}
}

func TestOpen_WALMode(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = database.Close() }()

	var mode string
	if err := database.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("checking journal_mode: %v", err)
	}
	// In-memory databases use "memory" journal mode instead of "wal"
	if mode != "memory" && mode != "wal" {
		t.Errorf("expected journal_mode 'wal' or 'memory', got %q", mode)
	}
}

func TestOpen_IncrementalMigration(t *testing.T) {
	// Simulate an existing database without sync tables
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening raw db: %v", err)
	}
	database.SetMaxOpenConns(1)

	// Create only the core tables (no sync tables)
	_, err = database.Exec(`
		CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			position INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		);
		CREATE TABLE project_items (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			completed INTEGER NOT NULL DEFAULT 0,
			archived INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		);
	`)
	if err != nil {
		t.Fatalf("creating core tables: %v", err)
	}

	// Insert test data
	_, err = database.Exec("INSERT INTO projects (id, name) VALUES ('p1', 'existing')")
	if err != nil {
		t.Fatalf("inserting test data: %v", err)
	}

	// Run migration — should add sync tables without losing data
	if err := migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Verify sync tables exist
	var name string
	err = database.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='pending_sync'",
	).Scan(&name)
	if err != nil {
		t.Error("pending_sync table not created by migration")
	}

	err = database.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='sync_state'",
	).Scan(&name)
	if err != nil {
		t.Error("sync_state table not created by migration")
	}

	// Verify existing data survived
	var projectName string
	err = database.QueryRow("SELECT name FROM projects WHERE id='p1'").Scan(&projectName)
	if err != nil {
		t.Fatalf("existing data lost: %v", err)
	}
	if projectName != "existing" {
		t.Errorf("expected project name 'existing', got %q", projectName)
	}

	_ = database.Close()
}
