// Package logging provides debug logging to a file. Logs never go to stdout or
// stderr so the CLI/TUI output stays clean.
//
// Logging is disabled unless SPACEFINDER_DEBUG is set to a truthy value (or
// SPACEFINDER_LOG is set). SPACEFINDER_LOG overrides the log file location;
// otherwise it defaults to the repo root during development and
// os.UserConfigDir()/spacefinder/spacefinder.log (~/.config/spacefinder on Linux) in
// production.
//
// When logging is enabled, the log opens with a system/OS header (version, OS,
// architecture, runtime, process, environment) so a captured log is self-contained
// and easy to triage, followed by verbose DEBUG/ERROR lines describing what the
// app is doing.
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	envDebug = "SPACEFINDER_DEBUG"
	envLog   = "SPACEFINDER_LOG"
)

var (
	mu     sync.Mutex
	on     bool
	file   *os.File
	logger *log.Logger
	// Path is the active log file location.
	Path string
)

// Init enables file logging when SPACEFINDER_DEBUG is set and opens the log
// file. version is the application version (baked in via ldflags); it is
// reported in the system header. Returns whether logging is active.
func Init(version string) bool {
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
	logSystemHeader(version)
	logger.Printf("DEBUG started pid=%d args=%q", os.Getpid(), os.Args[1:])
	return true
}

// logSystemHeader writes a self-contained system/OS header at the top of the
// log so a captured file carries the environment it was produced in. Callers
// must already hold the mutex.
func logSystemHeader(version string) {
	exec, _ := os.Executable()
	cwd, _ := os.Getwd()
	host, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	config, _ := os.UserConfigDir()

	env := []struct{ k, v string }{
		{"SPACEFINDER_DEBUG", os.Getenv(envDebug)},
		{"SPACEFINDER_LOG", os.Getenv(envLog)},
		{"TERM", os.Getenv("TERM")},
		{"COLORTERM", os.Getenv("COLORTERM")},
		{"SHELL", os.Getenv("SHELL")},
	}
	logger.Printf("SYSTEM version=%q started=%s", version, time.Now().Format("2006-01-02T15:04:05-07:00"))
	logger.Printf("SYSTEM os=%s arch=%s go=%s", runtime.GOOS, runtime.GOARCH, runtime.Version())
	logger.Printf("SYSTEM host=%q home=%q config=%q", host, home, config)
	logger.Printf("SYSTEM exec=%q cwd=%q pid=%d args=%q", exec, cwd, os.Getpid(), os.Args)
	for _, e := range env {
		logger.Printf("SYSTEM env %s=%q", e.k, e.v)
	}
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

// LogWriter returns the active log file as an io.Writer, or nil when logging
// is disabled. Callers may mirror program output (e.g. recovered panic traces)
// into the log.
func LogWriter() io.Writer {
	mu.Lock()
	defer mu.Unlock()
	if !on {
		return nil
	}
	return file
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
	// SPACEFINDER_LOG alone is enough to enable file logging at that path.
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
