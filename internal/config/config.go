package config

// Config holds the application configuration loaded from TOML and env vars.
type Config struct {
	Mode   string       `json:"mode"`
	Remote RemoteConfig `json:"remote"`
	Local  LocalConfig  `json:"local"`
}

type RemoteConfig struct {
	APIURL string `json:"api_url"`
	APIKey string `json:"api_key,omitempty"`
}

type LocalConfig struct {
	DBPath string `json:"db_path"`
}
