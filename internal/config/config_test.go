package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	// No config file, no env vars — should use defaults
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
