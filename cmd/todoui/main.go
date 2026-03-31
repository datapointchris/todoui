package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/internal/backend"
	"github.com/datapointchris/todoui/internal/cli"
	"github.com/datapointchris/todoui/internal/config"
	"github.com/datapointchris/todoui/internal/db"
	"github.com/datapointchris/todoui/internal/sync"
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
	var syncEngine *sync.Engine
	var mode string

	root := &cobra.Command{
		Use:   "todoui",
		Short: "Personal project organization",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			mode = cfg.Mode

			switch mode {
			case "remote":
				b = backend.NewRemoteBackend(cfg.Remote.APIURL)
			default:
				if err := os.MkdirAll(filepath.Dir(cfg.Local.DBPath), 0o755); err != nil {
					return fmt.Errorf("creating data directory: %w", err)
				}
				d, err := db.Open(cfg.Local.DBPath)
				if err != nil {
					return fmt.Errorf("opening database: %w", err)
				}
				database = d
				local := backend.NewLocalBackend(d)

				if cfg.Sync.Enabled {
					syncEngine = sync.New(d, cfg.Sync.APIURL)
					syncEngine.Start()
					b = sync.NewSyncBackend(local, syncEngine)
				} else {
					b = local
				}
			}
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			app := tui.NewApp(b, mode, syncEngine)
			p := tea.NewProgram(app, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
		PersistentPostRunE: func(_ *cobra.Command, _ []string) error {
			if syncEngine != nil {
				syncEngine.Stop()
			}
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
			app := tui.NewApp(b, mode, syncEngine)
			p := tea.NewProgram(app, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	})

	cli.RegisterAll(root, &b)

	return root
}
