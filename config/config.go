package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

// Config holds the application configuration loaded from TOML and env vars.
type Config struct {
	Local LocalConfig `mapstructure:"local"`
	Sync  SyncConfig  `mapstructure:"sync"`
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
	v := viper.New()

	// Defaults. A full pull costs 2+2N requests, so the interval trades API load
	// against staleness; two minutes keeps an unattended TUI current without
	// making a burst of agent CLI calls pull more than once.
	v.SetDefault("local.db_path", defaultDBPath())
	v.SetDefault("sync.interval", 2*time.Minute)

	// Config file: $XDG_CONFIG_HOME/todoui/config.toml or ~/.config/todoui/config.toml
	// Use XDG explicitly rather than Go's UserConfigDir, which returns
	// ~/Library/Application Support on macOS — not where CLI tools put config.
	v.AddConfigPath(filepath.Join(userConfigDir(), "todoui"))
	v.SetConfigName("config")
	v.SetConfigType("toml")

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
