package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds the application configuration loaded from TOML and env vars.
type Config struct {
	Mode   string       `mapstructure:"mode"`
	Remote RemoteConfig `mapstructure:"remote"`
	Local  LocalConfig  `mapstructure:"local"`
}

// RemoteConfig holds settings for the remote API mode.
type RemoteConfig struct {
	APIURL string `mapstructure:"api_url"`
	APIKey string `mapstructure:"api_key"`
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
	v.SetDefault("mode", "local")
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

	// Env var overrides: TODOUI_MODE, TODOUI_API_URL, TODOUI_DB
	v.SetEnvPrefix("TODOUI")
	_ = v.BindEnv("mode", "TODOUI_MODE")
	_ = v.BindEnv("remote.api_url", "TODOUI_API_URL")
	_ = v.BindEnv("local.db_path", "TODOUI_DB")

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Mode == "remote" && cfg.Remote.APIURL == "" {
		return nil, fmt.Errorf("remote.api_url is required when mode=remote (set in config or TODOUI_API_URL)")
	}

	return &cfg, nil
}

func defaultDBPath() string {
	dataDir, err := os.UserConfigDir()
	if err != nil {
		return "todoui.db"
	}
	return filepath.Join(dataDir, "todoui", "todoui.db")
}
