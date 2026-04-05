package cli

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"
)

const repo = "datapointchris/todoui"

// version is set at build time via ldflags. Falls back to Go module info.
var version = ""

func Version() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update todoui to the latest version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			current := Version()
			fmt.Printf("Current version: %s\n", current)

			if current == "dev" {
				return fmt.Errorf("cannot update a dev build; install from a release instead")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
			if err != nil {
				return fmt.Errorf("creating update source: %w", err)
			}

			updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: source})
			if err != nil {
				return fmt.Errorf("creating updater: %w", err)
			}

			latest, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(repo))
			if err != nil {
				return fmt.Errorf("checking for updates: %w", err)
			}
			if !found {
				return fmt.Errorf("no releases found for %s", repo)
			}

			if latest.LessOrEqual(current) {
				fmt.Printf("Already up to date (latest: %s)\n", latest.Version())
				return nil
			}

			fmt.Printf("Updating to %s...\n", latest.Version())
			if err := updater.UpdateTo(ctx, latest, ""); err != nil {
				return fmt.Errorf("updating binary: %w", err)
			}

			fmt.Printf("Updated to %s\n", latest.Version())
			return nil
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the current version",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(Version())
		},
	}
}
