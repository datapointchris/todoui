package main

import (
	"database/sql"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/cli"
	"github.com/datapointchris/todoui/internal/db"
	"github.com/datapointchris/todoui/internal/tui"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var b backend.Backend
	var database *sql.DB
	var mode string

	root := &cobra.Command{
		Use:   "todoui",
		Short: "Personal project organization",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			mode = os.Getenv("TODOUI_MODE")
			if mode == "" {
				mode = "local"
			}
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
				d, err := db.Open(dbPath)
				if err != nil {
					return fmt.Errorf("opening database: %w", err)
				}
				database = d
				b = backend.NewLocalBackend(d)
			}
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			app := tui.NewApp(b, mode)
			p := tea.NewProgram(app, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
		PersistentPostRunE: func(_ *cobra.Command, _ []string) error {
			if database != nil {
				return database.Close()
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Explicit "ui" alias for the TUI
	root.AddCommand(&cobra.Command{
		Use:   "ui",
		Short: "Launch the TUI (default when no command given)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			app := tui.NewApp(b, mode)
			p := tea.NewProgram(app, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	})

	cli.RegisterAll(root, &b)

	return root
}
