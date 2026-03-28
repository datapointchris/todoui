package cli

import "github.com/datapointchris/todoui/internal/backend"

// CLI holds the dependencies for CLI command execution.
type CLI struct {
	backend backend.Backend
}

// NewCLI creates a CLI command handler backed by the given Backend implementation.
func NewCLI(b backend.Backend) *CLI {
	return &CLI{backend: b}
}
