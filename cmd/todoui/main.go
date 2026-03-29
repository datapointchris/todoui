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
	mode := os.Getenv("TODOUI_MODE")
	if mode == "" {
		mode = "local"
	}

	var b backend.Backend

	switch mode {
	case "remote":
		apiURL := os.Getenv("TODOUI_API_URL")
		if apiURL == "" {
			return fmt.Errorf("TODOUI_API_URL is required when TODOUI_MODE=remote")
		}
		b = backend.NewRemoteBackend(apiURL)

	default:
		dbPath := os.Getenv("TODOUI_DB")
		if dbPath == "" {
			dbPath = "todoui.db"
		}
		database, err := db.Open(dbPath)
		if err != nil {
			return fmt.Errorf("opening database: %w", err)
		}
		defer func() { _ = database.Close() }()
		b = backend.NewLocalBackend(database)
	}

	app := tui.NewApp(b, mode)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
