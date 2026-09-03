// Package logging provides debug logging to a file. Logs never go to stdout or
// stderr so the CLI/TUI output stays clean.
//
// Logging is disabled unless GO_CLI_DEBUG is set to a truthy value (or
// GO_CLI_LOG is set). GO_CLI_LOG overrides the log file location; otherwise it
// defaults to the repo root during development and
// os.UserConfigDir()/spacefinder/spacefinder.log (~/.config/spacefinder on Linux) in
// production.
package logging

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	envDebug = "GO_CLI_DEBUG"
	envLog   = "GO_CLI_LOG"
)

var (
	mu     sync.Mutex
	on     bool
	file   *os.File
	logger *log.Logger
	// Path is the active log file location.
	Path string
)

// Init enables file logging when GO_CLI_DEBUG is set and opens the log file.
// Returns whether logging is active.
func Init() bool {
	mu.Lock()
	defer mu.Unlock()
	if on {
		return true
	}
	if !debugEnabled() {
		return false
	}
	p := os.Getenv(envLog)
	if p != "" {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "spacefinder: debug log: %v\n", err)
			return false
		}
	} else {
		dir, err := defaultLogDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "spacefinder: debug log: %v\n", err)
			return false
		}
		p = filepath.Join(dir, "spacefinder.log")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "spacefinder: debug log: %v\n", err)
			return false
		}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spacefinder: debug log: %v\n", err)
		return false
	}
	on = true
	file = f
	Path = p
	logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	fmt.Fprintf(os.Stderr, "spacefinder: debug log: %s\n", p)
	// Log directly: Init already holds the mutex.
	logger.Printf("DEBUG started pid=%d args=%q", os.Getpid(), os.Args[1:])
	return true
}

// Close closes the log file. Safe to call multiple times.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
		file = nil
	}
	on = false
	logger = nil
}

// Debugf logs a debug message when logging is enabled.
func Debugf(format string, args ...any) {
	logf("DEBUG", format, args...)
}

// Errorf logs an error message when logging is enabled.
func Errorf(format string, args ...any) {
	logf("ERROR", format, args...)
}

func logf(level, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if !on {
		return
	}
	logger.Printf("%s "+format, append([]any{level}, args...)...)
}

func debugEnabled() bool {
	// GO_CLI_LOG alone is enough to enable file logging at that path.
	if os.Getenv(envLog) != "" {
		return true
	}
	v := os.Getenv(envDebug)
	return v != "" && v != "0"
}

// defaultLogDir returns the log directory: the repo root during development,
// os.UserConfigDir()/spacefinder otherwise.
func defaultLogDir() (string, error) {
	if repo := repoRoot(); repo != "" {
		return repo, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "spacefinder"), nil
}

// repoRoot returns the nearest git work tree at or above the working
// directory, or "" when the cwd is not inside a repository.
func repoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
	}
}
