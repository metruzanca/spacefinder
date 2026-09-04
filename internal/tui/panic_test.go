package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/metruzanca/spacefinder/internal/logging"
)

// TestRunSafeRecoversPanic confirms a panicking background command turns into
// an internalErrMsg instead of escaping as a panic (which bubbletea would
// swallow and report as "program was killed").
func TestRunSafeRecoversPanic(t *testing.T) {
	msg := runSafe("test", func() tea.Msg { panic("kaboom") })
	ie, ok := msg.(internalErrMsg)
	if !ok {
		t.Fatalf("runSafe returned %T, want internalErrMsg", msg)
	}
	if ie.op != "test" {
		t.Fatalf("op = %q, want test", ie.op)
	}
	if !strings.Contains(ie.err.Error(), "kaboom") {
		t.Fatalf("err = %q, want the panic value mentioned", ie.err)
	}
}

// TestRunSafeLogsPanic confirms the recovered stack trace lands in the debug
// log file, so the cause of a panic is never lost.
func TestRunSafeLogsPanic(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "spacefinder.log")
	t.Setenv("GO_CLI_LOG", logPath)
	t.Setenv("GO_CLI_DEBUG", "1")
	logging.Init()
	t.Cleanup(logging.Close)

	runSafe("test", func() tea.Msg { panic("boom-grade") })

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{"panic in test", "boom-grade", "goroutine"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log missing %q:\n%s", want, got)
		}
	}
}

// TestRunSafeNormalResultPassesThrough confirms a healthy command is returned
// untouched by the recovery wrapper.
func TestRunSafeNormalResultPassesThrough(t *testing.T) {
	sentinel := internalErrMsg{} // any distinct type is fine
	got := runSafe("test", func() tea.Msg { return sentinel })
	if got != sentinel {
		t.Fatalf("got %#v, want the command's own message", got)
	}
}

// TestInternalErrMsgShowsErrorView confirms an internalErrMsg (delivered when
// a background task panicked) lands in the error view, not a dead program.
func TestInternalErrMsgShowsErrorView(t *testing.T) {
	m := browseModel()
	got, _ := m.Update(internalErrMsg{op: "scan", err: errors.New("boom")})
	nm, ok := got.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", got)
	}
	if nm.mode != modeError {
		t.Fatalf("mode = %v, want modeError", nm.mode)
	}
	if !strings.Contains(nm.errMessage, "boom") {
		t.Fatalf("errMessage = %q, want the error text", nm.errMessage)
	}
}

// TestQuitWorksFromErrorView confirms the error view still quits cleanly, the
// common exit path after a recovered panic.
func TestQuitWorksFromErrorView(t *testing.T) {
	m, _ := browseModel().Update(internalErrMsg{op: "scan", err: errors.New("boom")})
	nm := m.(*Model)
	_, cmd := nm.Update(keyRunes("q"))
	if cmd == nil {
		t.Fatal("expected a quit command while in the error view")
	}
}
