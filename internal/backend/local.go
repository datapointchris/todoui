package backend

import "database/sql"

// LocalBackend provides direct SQLite access for local mode.
type LocalBackend struct {
	db *sql.DB
}

// NewLocalBackend creates a backend that operates directly on a local SQLite database.
func NewLocalBackend(db *sql.DB) *LocalBackend {
	return &LocalBackend{db: db}
}
