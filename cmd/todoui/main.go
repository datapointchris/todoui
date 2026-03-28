package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/db"
	"github.com/datapointchris/todoui/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// For now, default to local mode with a local database.
	// Config-based mode selection comes in Phase 7.
	dbPath := os.Getenv("TODOUI_DB")
	if dbPath == "" {
		dbPath = "todoui.db"
	}

	mode := os.Getenv("TODO_MODE")
	if mode == "" {
		mode = "local"
	}

	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer func() { _ = database.Close() }()

	b := backend.NewLocalBackend(database)
	app := tui.NewApp(b, mode)

	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
