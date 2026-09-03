package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugDisabledByDefault(t *testing.T) {
	t.Setenv("GO_CLI_DEBUG", "")
	t.Setenv("GO_CLI_LOG", "")
	if Init() {
		t.Fatal("logging must be off without GO_CLI_DEBUG")
		t.Cleanup(Close)
	}
}

func TestLogEnabledByLogAlone(t *testing.T) {
	t.Setenv("GO_CLI_DEBUG", "")
	t.Setenv("GO_CLI_LOG", filepath.Join(t.TempDir(), "debug.log"))
	if !Init() {
		t.Fatal("GO_CLI_LOG alone should enable file logging")
	}
	t.Cleanup(Close)
}

func TestLogPathOverride(t *testing.T) {
	t.Setenv("GO_CLI_DEBUG", "1")
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "debug.log")
	t.Setenv("GO_CLI_LOG", path)
	if !Init() {
		t.Fatal("expected logging enabled")
	}
	t.Cleanup(Close)
	if Path != path {
		t.Fatalf("Path = %q, want %q", Path, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}

func TestDebugfWritesWhenEnabled(t *testing.T) {
	t.Setenv("GO_CLI_DEBUG", "1")
	path := filepath.Join(t.TempDir(), "g.log")
	t.Setenv("GO_CLI_LOG", path)
	if !Init() {
		t.Fatal("expected logging enabled")
	}
	Debugf("hello %s", "world")
	Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Fatalf("log file missing message: %q", data)
	}
}
