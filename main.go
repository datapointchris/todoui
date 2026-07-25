package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/cli"
	"github.com/datapointchris/todoui/config"
	"github.com/datapointchris/todoui/db"
	"github.com/datapointchris/todoui/sync"
	"github.com/datapointchris/todoui/tui"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// cliPullFreshness is how stale local data may be before a CLI command
// refreshes it. Short enough that an agent reading todoui is not acting on
// yesterday's picture, long enough that a burst of commands pulls once.
const cliPullFreshness = 2 * time.Minute

// commandsThatSkipPull never read or write todoui data, so making them wait on
// the network buys nothing. Everything else pulls by default — an allowlist
// would mean a new command silently serves stale data, which is the bug this
// exists to fix.
var commandsThatSkipPull = map[string]bool{
	"ui":         true, // pulls itself on start, and keeps pulling
	"version":    true,
	"update":     true,
	"completion": true,
	"help":       true,
	"sync":       true, // forces a full pull regardless of freshness
}

// refreshForCLI reconciles with the server before a CLI command reads local
// data. The TUI pulls from App.Init and then holds a live session; a CLI
// process exits immediately, so without this every CLI read served whatever
// the TUI last pulled — days old on a machine where todoui is driven by agents
// rather than opened.
//
// Failure is deliberately not fatal. todoui is local-first: an unreachable API
// must degrade to local data, not break the command.
func refreshForCLI(cmd *cobra.Command, engine *sync.Engine, noSync bool) {
	if engine == nil || noSync || commandsThatSkipPull[cmd.Name()] {
		return
	}
	if _, err := engine.PullIfStale(cmd.Context(), cliPullFreshness); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not refresh from %s (%v) — showing local data\n", engine.APIURL(), err)
	}
}

func rootCmd() *cobra.Command {
	var b backend.Backend
	var database *sql.DB
	var syncEngine *sync.Engine
	var cfg *config.Config
	var noSync bool

	root := &cobra.Command{
		Use:     "todoui",
		Short:   "Personal project organization",
		Version: cli.Version(),
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			cfg, err = config.Load()
			if err != nil {
				return err
			}

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
				syncEngine = sync.New(d, cfg.Sync.APIURL, cfg.Sync.APIKey)
				syncEngine.Start()
				b = sync.NewSyncBackend(local, syncEngine)
			} else {
				b = local
			}

			refreshForCLI(cmd, syncEngine, noSync)
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			app := tui.NewApp(b, syncEngine, cfg.Local.DBPath)
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

	root.PersistentFlags().BoolVar(&noSync, "no-sync", false,
		"skip the pre-command refresh and use local data as-is")

	root.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Reconcile with the ichrisbirch API now",
		Long:  "Pushes anything queued, then pulls, regardless of how recently the last pull ran.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if syncEngine == nil {
				return fmt.Errorf("sync is disabled — enable it in ~/.config/todoui/config.toml")
			}
			if err := syncEngine.Pull(cmd.Context()); err != nil {
				return err
			}
			fmt.Println("Synced.")
			return nil
		},
	})

	// Explicit "ui" alias for the TUI
	root.AddCommand(&cobra.Command{
		Use:   "ui",
		Short: "Launch the TUI (default when no command given)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			app := tui.NewApp(b, syncEngine, cfg.Local.DBPath)
			p := tea.NewProgram(app, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	})

	cli.RegisterAll(root, &b)

	return root
}
