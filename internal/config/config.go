package config

import (
	"fmt"
	"os"
	"path/filepath"

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
}

// LocalConfig holds settings for the local embedded mode.
type LocalConfig struct {
	DBPath string `mapstructure:"db_path"`
}

// Load reads configuration from the TOML config file and environment variables.
// Priority (highest to lowest): env vars → config file → defaults.
func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("local.db_path", defaultDBPath())

	// Config file: ~/.config/todoui/config.toml
	configDir, err := os.UserConfigDir()
	if err == nil {
		v.AddConfigPath(filepath.Join(configDir, "todoui"))
	}
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

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Sync.Enabled && cfg.Sync.APIURL == "" {
		return nil, fmt.Errorf("sync.api_url is required when sync is enabled (set in config or TODOUI_SYNC_URL)")
	}

	return &cfg, nil
}

func defaultDBPath() string {
	return filepath.Join(userDataDir(), "todoui", "todoui.db")
}

// userDataDir returns the XDG data directory ($XDG_DATA_HOME or ~/.local/share).
// Go's stdlib only provides UserConfigDir, not UserDataDir.
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
