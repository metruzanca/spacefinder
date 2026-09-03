package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	t.Setenv("GO_CLI_CONFIG_PATH", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatalf("Load() = %v, want nil for missing config", cfg)
	}
}

func TestPathEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GO_CLI_CONFIG_PATH", dir)

	want := filepath.Join(dir, "config.toml")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestSaveAndLoad(t *testing.T) {
	t.Setenv("GO_CLI_CONFIG_PATH", t.TempDir())

	cfg := &Config{Example: "hello"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected config to load")
	}
	if loaded.Example != cfg.Example {
		t.Fatalf("Example = %q, want %q", loaded.Example, cfg.Example)
	}

	// The config file was created on disk.
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not created: %v", err)
	}
}

func TestLoadOrCreate(t *testing.T) {
	t.Setenv("GO_CLI_CONFIG_PATH", t.TempDir())

	cfg, err := LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("LoadOrCreate() = nil, want default config")
	}

	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not created: %v", err)
	}
}
