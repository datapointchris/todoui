package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds the application configuration loaded from TOML and env vars.
type Config struct {
	Local LocalConfig `mapstructure:"local"`
	Sync  SyncConfig  `mapstructure:"sync"`

	// ReposRegistry names a registry maintained outside todoui's data directory,
	// which is how a machine sharing one between tools declares it. Empty, or no
	// config file at all, leaves todoui's own data directory as the answer.
	//
	// The field is the config layer alone. $TODOUI_REPOS_REGISTRY above it and
	// the data directory below it are composed by repos.DefaultPath, so the whole
	// resolution order reads in one place rather than half here and half there.
	ReposRegistry string `mapstructure:"repos_registry"`
}

// SyncConfig holds settings for background sync with the remote API.
type SyncConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	APIURL  string `mapstructure:"api_url"`
	APIKey  string `mapstructure:"api_key"`

	// Interval is the longest todoui will go without reconciling: the TUI's
	// background pull period, and the age at which a CLI read refreshes first.
	Interval time.Duration `mapstructure:"interval"`
}

// LocalConfig holds settings for the local embedded mode.
type LocalConfig struct {
	DBPath string `mapstructure:"db_path"`
}

// Load reads configuration from the TOML config file and environment variables.
// Priority (highest to lowest): env vars → config file → defaults.
func Load() (*Config, error) {
	v := newViper()

	// Defaults. A full pull costs 2+2N requests, so the interval trades API load
	// against staleness; two minutes keeps an unattended TUI current without
	// making a burst of agent CLI calls pull more than once.
	v.SetDefault("local.db_path", defaultDBPath())
	v.SetDefault("sync.interval", 2*time.Minute)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	// Env var overrides
	v.SetEnvPrefix("TODOUI")
	_ = v.BindEnv("local.db_path", "TODOUI_DB")
	_ = v.BindEnv("sync.enabled", "TODOUI_SYNC")
	_ = v.BindEnv("sync.api_url", "TODOUI_SYNC_URL")
	_ = v.BindEnv("sync.api_key", "TODOUI_SYNC_KEY")
	_ = v.BindEnv("sync.interval", "TODOUI_SYNC_INTERVAL")

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Sync.Enabled && cfg.Sync.APIURL == "" {
		return nil, fmt.Errorf("sync.api_url is required when sync is enabled (set in config or TODOUI_SYNC_URL)")
	}

	// time.NewTicker panics on a non-positive duration, and a config typo must not
	// take the TUI down. Disabling automatic sync is what sync.enabled is for.
	if cfg.Sync.Interval < minSyncInterval {
		cfg.Sync.Interval = minSyncInterval
	}

	return &cfg, nil
}

// minSyncInterval floors the reconcile period. A full pull is 2+2N requests, so
// anything shorter is a self-inflicted denial of service on the API.
const minSyncInterval = 15 * time.Second

// newViper points a fresh viper at todoui's config file. Shared by both readers
// here so they cannot drift on where that file is.
//
// Config file: $XDG_CONFIG_HOME/todoui/config.toml or ~/.config/todoui/config.toml.
// Use XDG explicitly rather than Go's UserConfigDir, which returns
// ~/Library/Application Support on macOS — not where CLI tools put config.
func newViper() *viper.Viper {
	v := viper.New()
	v.AddConfigPath(filepath.Join(userConfigDir(), "todoui"))
	v.SetConfigName("config")
	v.SetConfigType("toml")
	return v
}

// ConfiguredReposRegistry is the repos_registry key with a leading ~ expanded,
// and "" for every way reading it can fail. Absent config is one of those ways
// and is not an error: a machine keeping its registry where todoui expects it
// should not have to hold a file saying so.
//
// A read of its own rather than a field off Load's result, because Load
// validates. Sync enabled without an api_url is an error there and has nothing
// to do with where the registry lives, so resolving through it would turn that
// unrelated failure into an empty registry — which bans nothing and reports
// nothing.
func ConfiguredReposRegistry() string {
	v := newViper()
	if err := v.ReadInConfig(); err != nil {
		return ""
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return ""
	}
	return expandTilde(cfg.ReposRegistry)
}

// expandTilde resolves a leading ~, which a hand-edited config will carry. Left
// literal it names a directory that does not exist, and a registry that is not
// there is not an error here — it reads as an empty registry rather than a bad
// path, so the mistake surfaces as `--repo` silently accepting anything.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

func defaultDBPath() string {
	return filepath.Join(userDataDir(), "todoui", "todoui.db")
}

// userConfigDir returns $XDG_CONFIG_HOME or ~/.config.
func userConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config")
}

// UserDataDir returns the XDG data directory ($XDG_DATA_HOME or ~/.local/share).
// Exported so every package resolving a data path uses one resolver rather than
// its own copy of the env-var-then-fallback shape.
func UserDataDir() string {
	return userDataDir()
}

// userDataDir returns the XDG data directory ($XDG_DATA_HOME or ~/.local/share).
func userDataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "share")
}
