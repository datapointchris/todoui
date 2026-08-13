package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// No config file, no env vars — should use defaults
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TODOUI_SYNC", "")
	t.Setenv("TODOUI_SYNC_URL", "")
	t.Setenv("TODOUI_DB", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Sync.Enabled {
		t.Error("expected sync disabled by default")
	}
	if cfg.Local.DBPath == "" {
		t.Error("expected non-empty default db_path")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("TODOUI_DB", "/tmp/test.db")
	t.Setenv("TODOUI_SYNC", "true")
	t.Setenv("TODOUI_SYNC_URL", "https://example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Local.DBPath != "/tmp/test.db" {
		t.Errorf("expected db_path '/tmp/test.db', got %q", cfg.Local.DBPath)
	}
	if !cfg.Sync.Enabled {
		t.Error("expected sync enabled via env")
	}
	if cfg.Sync.APIURL != "https://example.com" {
		t.Errorf("expected sync url 'https://example.com', got %q", cfg.Sync.APIURL)
	}
}

func TestLoad_SyncEnabledWithoutURL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TODOUI_SYNC", "true")
	t.Setenv("TODOUI_SYNC_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when sync enabled without api_url")
	}
}

func TestDefaultDBPath_XDGOverride(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")

	got := defaultDBPath()
	want := "/custom/data/todoui/todoui.db"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The interval governs both the TUI's background pull and the age at which a
// CLI read refreshes, so a missing default would mean no automatic sync at all.
func TestLoad_SyncIntervalDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TODOUI_SYNC_INTERVAL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Sync.Interval != 2*time.Minute {
		t.Errorf("expected a 2m default interval, got %v", cfg.Sync.Interval)
	}
}

func TestLoad_SyncIntervalFromEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TODOUI_SYNC_INTERVAL", "45s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Sync.Interval != 45*time.Second {
		t.Errorf("expected 45s from the environment, got %v", cfg.Sync.Interval)
	}
}

// repos_registry is a bare top-level key, which TOML requires above the first
// [section] header — declared under one it would belong to that section and
// unmarshal into nothing.
func TestReposRegistryIsReadFromTheConfigFile(t *testing.T) {
	const declared = "/declared/repos.json"
	writeConfig(t, "repos_registry = \""+declared+"\"\n\n[sync]\nenabled = false\n")

	if got := ConfiguredReposRegistry(); got != declared {
		t.Errorf("ConfiguredReposRegistry() = %q, want %q", got, declared)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ReposRegistry != declared {
		t.Errorf("cfg.ReposRegistry = %q, want %q", cfg.ReposRegistry, declared)
	}
}

// A machine keeping its registry where todoui expects it holds no config at
// all, so an absent or unreadable file answers "" rather than failing.
func TestConfiguredReposRegistryIsEmptyWithoutAConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := ConfiguredReposRegistry(); got != "" {
		t.Errorf("ConfiguredReposRegistry() = %q, want empty", got)
	}

	writeConfig(t, "repos_registry = [not toml\n")
	if got := ConfiguredReposRegistry(); got != "" {
		t.Errorf("a malformed config must answer empty, got %q", got)
	}
}

// writeConfig points $XDG_CONFIG_HOME at a directory holding body as todoui's
// config file.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "todoui"), 0o755); err != nil {
		t.Fatalf("creating config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "todoui", "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
}

// time.NewTicker panics on a non-positive duration, so a typo here would take
// the TUI down on launch rather than degrading to a slower sync.
func TestLoad_SyncIntervalIsFloored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for _, given := range []string{"0s", "-5m", "1s"} {
		t.Setenv("TODOUI_SYNC_INTERVAL", given)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load(%s): %v", given, err)
		}
		if cfg.Sync.Interval < minSyncInterval {
			t.Errorf("interval %q left below the floor: %v", given, cfg.Sync.Interval)
		}
	}
}
