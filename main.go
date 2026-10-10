package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/datapointchris/goclikit"
	"github.com/datapointchris/goselfupdate/autoupdate"
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
	if err := goclikit.Execute(context.Background(), rootCmd(), autoConfig); err != nil {
		if !errors.Is(err, goclikit.ErrReported) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		// 2 says the command line was wrong rather than the run, which is the
		// only failure a caller should retry with different arguments.
		if errors.Is(err, goclikit.ErrUsage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// needsBackend reports whether a command reads or writes todoui's own data.
//
// PersistentPreRunE opens the database and runs migrations, and cobra runs it
// for the shell-completion callback like any other command — so every TAB press
// opened the database, and on a new machine created it. `version` is what a
// script calls to find out what is installed, `help` answers from the command
// tree, and `update` replaces the binary. None of them has anything to read.
//
// The ancestry is walked rather than only the leaf, because `completion` owns a
// subcommand per shell: `todoui completion zsh` has leaf name "zsh".
//
// `--help` never reaches here at all — cobra answers the flag before running
// this — which is why the list names the command and not the flag.
func needsBackend(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		switch current.Name() {
		case "help", "version", "update", "completion",
			cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return false
		}
	}
	return true
}

func rootCmd() *cobra.Command {
	var b backend.Backend
	var database *sql.DB
	var syncEngine *sync.Engine
	var cfg *config.Config

	root := &cobra.Command{
		Use:     "todoui",
		Short:   "Personal project organization",
		Version: cli.Version(),
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if !needsBackend(cmd) {
				return nil
			}
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
				syncEngine = sync.New(d, cfg.Sync.APIURL, cfg.Sync.TokenCommand)
				syncEngine.Start()
				b = sync.NewSyncBackend(local, syncEngine)
			} else {
				b = backend.NewLocalBackend(d, backend.AssigningItemNumbers(), banRepoNames)
			}
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

	var adoptOrigin, forceSweep bool
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile with the ichrisbirch API now",
		Long: "Pushes anything queued, then pulls, regardless of how recently the last pull ran.\n\n" +
			"A pull deletes everything the server did not return, so the database is bound to\n" +
			"the API it was first pulled from and refuses to reconcile against any other.\n" +
			"--adopt rebinds it; --force allows a pull that would delete most of what is here.",
		Args: cli.NoArgs,
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
	root.AddCommand(syncCmd, cli.UpdateCmd(), cli.VersionCmd())

	return root
}
