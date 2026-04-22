package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"
)

const (
	githubOwner = "datapointchris"
	githubRepo  = "todoui"
)

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

			if current == "dev" {
				fmt.Fprintln(os.Stderr, "✗ todoui upgrade failed: cannot update a dev build")
				return fmt.Errorf("dev build")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗ todoui upgrade failed: %s\n", err)
				return err
			}

			updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: source})
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗ todoui upgrade failed: %s\n", err)
				return err
			}

			latest, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(githubOwner+"/"+githubRepo))
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗ todoui upgrade failed: %s\n", err)
				return err
			}
			if !found {
				fmt.Fprintln(os.Stderr, "✗ todoui upgrade failed: no releases found")
				return fmt.Errorf("no releases")
			}

			beforeTag := ensureVPrefix(current)
			latestTag := ensureVPrefix(latest.Version())

			if latest.LessOrEqual(current) {
				fmt.Printf("✓ todoui already at latest: %s\n", latestTag)
				return nil
			}

			exe, err := os.Executable()
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗ todoui upgrade failed: %s\n", err)
				return err
			}

			if err := updater.UpdateTo(ctx, latest, exe); err != nil {
				fmt.Fprintf(os.Stderr, "✗ todoui upgrade failed: %s\n", err)
				return err
			}

			fmt.Printf("✓ todoui upgraded: %s → %s\n", beforeTag, latestTag)

			if subjects, err := fetchChanges(ctx, githubOwner, githubRepo, beforeTag, latestTag); err == nil && len(subjects) > 0 {
				fmt.Println()
				fmt.Println("Changes:")
				for _, s := range subjects {
					fmt.Printf("  • %s\n", s)
				}
			}

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

func ensureVPrefix(v string) string {
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

func fetchChanges(ctx context.Context, owner, repo, fromTag, toTag string) ([]string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/compare/%s...%s", owner, repo, fromTag, toTag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api: %s", resp.Status)
	}

	var body struct {
		Commits []struct {
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		} `json:"commits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	subjects := make([]string, 0, len(body.Commits))
	for _, c := range body.Commits {
		subject, _, _ := strings.Cut(c.Commit.Message, "\n")
		if subject != "" {
			subjects = append(subjects, subject)
		}
	}
	return subjects, nil
}
