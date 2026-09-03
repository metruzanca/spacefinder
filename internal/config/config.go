// Package config provides a TOML config file with generative defaults: the
// file is created on first use instead of requiring an init command.
package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds spacefinder's persisted settings.
type Config struct {
	// Example is a placeholder setting. Replace with real settings as needed.
	Example string `toml:"example"`
}

// Path returns the location of the config file. When GO_CLI_CONFIG_PATH is set
// it is treated as the directory containing config.toml. Otherwise the config
// lives in the user's config dir (on Linux ~/.config/spacefinder/config.toml; on
// Windows %AppData%\spacefinder\config.toml).
func Path() (string, error) {
	if p := os.Getenv("GO_CLI_CONFIG_PATH"); p != "" {
		return filepath.Join(p, "config.toml"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "spacefinder", "config.toml"), nil
}

// Load reads the config file, returning nil if it does not exist yet.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save writes the config to disk, creating the config directory if needed.
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

// LoadOrCreate loads the config, writing a default config on first use. Call
// this so commands work with no prior init.
func LoadOrCreate() (*Config, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		return cfg, nil
	}
	cfg = Default()
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Default returns a config with sensible defaults.
func Default() *Config {
	return &Config{Example: "value"}
}
