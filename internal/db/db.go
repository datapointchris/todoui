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
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='projects'").Scan(&name)
	if err == sql.ErrNoRows {
		// Fresh database — apply schema
		if _, err := db.Exec(schema); err != nil {
			return fmt.Errorf("applying schema: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("checking schema: %w", err)
	}
	return nil
}
