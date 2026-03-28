package tui

import "github.com/datapointchris/todoui/internal/backend"

// App is the top-level Bubble Tea model for the TUI.
type App struct {
	backend backend.Backend
}

// NewApp creates a new TUI application backed by the given Backend.
func NewApp(b backend.Backend) *App {
	return &App{backend: b}
}
