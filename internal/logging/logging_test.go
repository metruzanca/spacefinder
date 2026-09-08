package logging

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDebugDisabledByDefault(t *testing.T) {
	t.Setenv("SPACEFINDER_DEBUG", "")
	t.Setenv("SPACEFINDER_LOG", "")
	if Init("test") {
		t.Fatal("logging must be off without SPACEFINDER_DEBUG")
		t.Cleanup(Close)
	}
}

func TestDebugEnabledByVar(t *testing.T) {
	t.Setenv("SPACEFINDER_DEBUG", "1")
	path := filepath.Join(t.TempDir(), "g.log")
	t.Setenv("SPACEFINDER_LOG", path)
	if !Init("test") {
		t.Fatal("SPACEFINDER_DEBUG=1 should enable file logging")
	}
	t.Cleanup(Close)
}

func TestLogEnabledByLogAlone(t *testing.T) {
	t.Setenv("SPACEFINDER_DEBUG", "")
	t.Setenv("SPACEFINDER_LOG", filepath.Join(t.TempDir(), "debug.log"))
	if !Init("test") {
		t.Fatal("SPACEFINDER_LOG alone should enable file logging")
	}
	t.Cleanup(Close)
}

func TestLogPathOverride(t *testing.T) {
	t.Setenv("SPACEFINDER_DEBUG", "1")
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "debug.log")
	t.Setenv("SPACEFINDER_LOG", path)
	if !Init("test") {
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
	t.Setenv("SPACEFINDER_DEBUG", "1")
	path := filepath.Join(t.TempDir(), "g.log")
	t.Setenv("SPACEFINDER_LOG", path)
	if !Init("test") {
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

func TestSystemHeaderWritten(t *testing.T) {
	t.Setenv("SPACEFINDER_DEBUG", "1")
	path := filepath.Join(t.TempDir(), "h.log")
	t.Setenv("SPACEFINDER_LOG", path)
	if !Init("1.2.3") {
		t.Fatal("expected logging enabled")
	}
	Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"SYSTEM version=\"1.2.3\"",
		"SYSTEM os=" + runtime.GOOS + " arch=" + runtime.GOARCH,
		"SYSTEM env SPACEFINDER_DEBUG",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log header missing %q:\n%s", want, got)
		}
	}
}
