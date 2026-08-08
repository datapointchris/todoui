package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/datapointchris/goselfupdate/autoupdate"
	"github.com/datapointchris/goselfupdate/cobracmd"
	"github.com/spf13/cobra"

	"github.com/datapointchris/todoui/backend"
	"github.com/datapointchris/todoui/cli"
	"github.com/datapointchris/todoui/config"
	"github.com/datapointchris/todoui/db"
	"github.com/datapointchris/todoui/repos"
	"github.com/datapointchris/todoui/sync"
	"github.com/datapointchris/todoui/tui"
)

func main() {
	autoConfig := autoupdate.Config{Update: cli.UpdateConfig()}
	if err := cobracmd.Execute(context.Background(), rootCmd(), autoConfig); err != nil {
		if !errors.Is(err, cobracmd.ErrReported) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
}

// commandsThatSkipPull never read or write todoui data, so making them wait on
// the network buys nothing. Everything else pulls by default — an allowlist
// would mean a new command silently serves stale data, which is the bug this
// exists to fix.
var commandsThatSkipPull = map[string]bool{
	"ui":                            true, // pulls on start, then on its own timer
	"version":                       true,
	"update":                        true,
	"completion":                    true,
	"help":                          true,
	"sync":                          true, // forces a full pull regardless of freshness
	cobra.ShellCompRequestCmd:       true, // cobra runs these on every TAB press
	cobra.ShellCompNoDescRequestCmd: true,
}

// skipsPull matches the whole ancestry, not just the leaf. `completion` owns a
// subcommand per shell, so `todoui completion zsh` executes `zsh` and a leaf-only
// check missed it — every shell that sourced the completions paid a full pull,
// and a stale one blocked startup for the API timeout.
func skipsPull(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if commandsThatSkipPull[c.Name()] {
			return true
		}
	}
	return false
}

// refreshForCLI reconciles with the server before a CLI command reads local
// data. The TUI pulls from App.Init and then holds a live session; a CLI
// process exits immediately, so without this every CLI read served whatever
// the TUI last pulled — days old on a machine where todoui is driven by agents
// rather than opened.
//
// Failure is deliberately not fatal. todoui is local-first: an unreachable API
// must degrade to local data, not break the command.
func refreshForCLI(cmd *cobra.Command, engine *sync.Engine, noSync bool, freshness time.Duration) {
	if engine == nil || noSync || skipsPull(cmd) {
		return
	}
	if _, err := engine.PullIfStale(cmd.Context(), freshness); err != nil {
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

			banRepoNames := backend.RefusingRepoNames(repos.DefaultPath())
			if cfg.Sync.Enabled {
				local := backend.NewLocalBackend(d, banRepoNames)
				syncEngine = sync.New(d, cfg.Sync.APIURL, cfg.Sync.APIKey)
				syncEngine.Start()
				b = sync.NewSyncBackend(local, syncEngine)
			} else {
				b = backend.NewLocalBackend(d, backend.AssigningItemNumbers(), banRepoNames)
			}

			refreshForCLI(cmd, syncEngine, noSync, cfg.Sync.Interval)
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			app := tui.NewApp(b, syncEngine, cfg.Local.DBPath, cfg.Sync.Interval)
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

	var adoptOrigin, forceSweep bool
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile with the ichrisbirch API now",
		Long: "Pushes anything queued, then pulls, regardless of how recently the last pull ran.\n\n" +
			"A pull deletes everything the server did not return, so the database is bound to\n" +
			"the API it was first pulled from and refuses to reconcile against any other.\n" +
			"--adopt rebinds it; --force allows a pull that would delete most of what is here.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if syncEngine == nil {
				return fmt.Errorf("sync is disabled — enable it in ~/.config/todoui/config.toml")
			}
			// Named on the way in, not after: --adopt rewrites which API owns
			// this database, and doing that silently is the mistake the binding
			// exists to prevent.
			if adoptOrigin {
				fmt.Printf("Binding %s to %s\n", cfg.Local.DBPath, syncEngine.APIURL())
			}
			if err := syncEngine.PullWith(cmd.Context(), sync.PullOptions{
				Adopt:           adoptOrigin,
				AllowLargeSweep: forceSweep,
			}); err != nil {
				return err
			}
			fmt.Println("Synced.")
			return nil
		},
	}
	syncCmd.Flags().BoolVar(&adoptOrigin, "adopt", false,
		"bind this database to the configured API, replacing whatever it was bound to")
	syncCmd.Flags().BoolVar(&forceSweep, "force", false,
		"allow a pull that would delete most of the local projects or items")
	root.AddCommand(syncCmd)

	// Explicit "ui" alias for the TUI
	root.AddCommand(&cobra.Command{
		Use:   "ui",
		Short: "Launch the TUI (default when no command given)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			app := tui.NewApp(b, syncEngine, cfg.Local.DBPath, cfg.Sync.Interval)
			p := tea.NewProgram(app, tea.WithAltScreen())
			_, err := p.Run()
			return err
		},
	})

	cli.RegisterAll(root, &b, func() {
		if syncEngine != nil {
			syncEngine.Flush()
		}
	})

	return root
}
