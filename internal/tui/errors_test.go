package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/metruzanca/spacefinder/internal/scan"
)

// modelWithErrors returns a browse model at the scan root carrying a real
// scanner that recorded some unreadable paths during Measure.
func modelWithErrors(t *testing.T) *Model {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret")
	if err := os.Mkdir(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "locked.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission checks are bypassed")
	}
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(secret, 0o755) })

	sc, tree, err := scan.Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sc.TotalErrors() == 0 {
		t.Fatal("test scan recorded no errors")
	}
	m := newModel(root)
	m.mode = modeBrowse
	m.width, m.height = 80, 24
	m.scanner = sc
	m.tree = tree
	m.current = tree
	m.refreshScanErrors()
	m.buildLayout()
	return m
}

func TestNoErrorsNoIndicator(t *testing.T) {
	m := browseModel()
	if strings.Contains(m.infoLine(), "unreadable") {
		t.Fatalf("info line shows errors with none recorded: %q", m.infoLine())
	}
	if strings.Contains(m.helpLine(), "errors") {
		t.Fatalf("help bar advertises errors with none recorded: %q", m.helpLine())
	}
	// 'e' with no errors is a no-op and stays in browse.
	got, cmd := m.Update(keyRunes("e"))
	if cmd != nil {
		t.Fatal("e with no errors should not start commands")
	}
	if got.(*Model).mode != modeBrowse {
		t.Fatalf("e with no errors left browse; mode = %v", got.(*Model).mode)
	}
}

func TestInfoLineShowsUnreadableCount(t *testing.T) {
	m := modelWithErrors(t)
	if !strings.Contains(m.infoLine(), "1 unreadable") {
		t.Fatalf("info line missing unreadable count: %q", m.infoLine())
	}
	if !strings.Contains(m.helpLine(), "errors(1)") {
		t.Fatalf("help bar missing error count: %q", m.helpLine())
	}
}

func TestKeybindShowsErrorCount(t *testing.T) {
	m := manyErrorsModel(t, 3)
	if !strings.Contains(m.helpLine(), "errors(3)") {
		t.Fatalf("help bar should show the error count: %q", m.helpLine())
	}
}

func TestHelpMenuAlwaysListsErrors(t *testing.T) {
	m := browseModel() // no errors: the e binding stays in the ? menu
	m.Update(keyRunes("?"))
	v := m.View()
	if !strings.Contains(v, "show unreadable paths") {
		t.Fatalf("? menu should always list the e binding; got:\n%s", v)
	}
}

func TestErrorsViewWrapsLongLines(t *testing.T) {
	m := browseModel()
	long := strings.Repeat("/long/directory/name/segment", 14) // ~350 chars
	m.scanErrors = []scan.ScanError{
		{Path: long, Err: errors.New("permission denied")},
	}
	m.scanErrTotal = 1
	m.Update(keyRunes("e"))
	v := m.View()
	// The wrapped modal keeps the whole reason and the path tail (previously
	// both were truncated away), and no rendered line overflows the terminal.
	for _, want := range []string{"permission denied", "/segment"} {
		if !strings.Contains(v, want) {
			t.Fatalf("wrapped errors view lost %q; got:\n%s", want, v)
		}
	}
	for _, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("line exceeds terminal width after wrapping: %q", line)
		}
	}
}

func TestWrapText(t *testing.T) {
	// Short text stays on a single line.
	if got := wrapText("hello world", 20); len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("wrapText short = %q", got)
	}
	// Whitespace breaks keep every word under the width.
	for _, l := range wrapText("one two three four five", 8) {
		if lipgloss.Width(l) > 8 {
			t.Fatalf("whitespace wrap left %q wider than 8", l)
		}
	}
	// A long unbroken word hard-wraps without losing content.
	joined := strings.Join(wrapText("abcdefghijklmnop", 5), "")
	if joined != "abcdefghijklmnop" {
		t.Fatalf("hard wrap lost content: %q", joined)
	}
	// Paths break at separators, keeping segments whole and under the width.
	for _, l := range wrapText("/home/user/very/long/project/directory/name", 12) {
		if lipgloss.Width(l) > 12 {
			t.Fatalf("path wrap left %q wider than 12", l)
		}
	}
	// Empty input yields no lines.
	if got := wrapText("", 10); len(got) != 0 {
		t.Fatalf("wrapText empty = %q, want none", got)
	}
}

func TestErrorsOpenClose(t *testing.T) {
	m := modelWithErrors(t)
	got, cmd := m.Update(keyRunes("e"))
	mm := got.(*Model)
	if cmd != nil {
		t.Fatal("opening errors should not start commands")
	}
	if mm.mode != modeErrors {
		t.Fatalf("mode = %v, want modeErrors", mm.mode)
	}
	// esc, e, and enter each close back to browse.
	for _, key := range []tea.KeyMsg{keyType(tea.KeyEsc), keyRunes("e"), keyType(tea.KeyEnter)} {
		m = modelWithErrors(t)
		m.Update(keyRunes("e"))
		got, _ = m.Update(key)
		if got.(*Model).mode != modeBrowse {
			t.Fatalf("%v closed to %v, want modeBrowse", key, got.(*Model).mode)
		}
	}
}

func TestErrorsViewListsPathsAndReasons(t *testing.T) {
	m := modelWithErrors(t)
	m.Update(keyRunes("e"))
	v := m.View()
	for _, want := range []string{"UNREADABLE", "secret", "errors"} {
		if !strings.Contains(v, want) {
			t.Fatalf("errors view missing %q; got:\n%s", want, v)
		}
	}
}

// manyErrorsModel builds a browse model over a root with n unreadable
// directories, so the errors modal has enough lines to scroll.
func manyErrorsModel(t *testing.T, n int) *Model {
	t.Helper()
	root := t.TempDir()
	dirs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		d := filepath.Join(root, "secret"+string(rune('a'+i)))
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if os.Geteuid() == 0 {
			t.Skip("running as root; permission checks are bypassed")
		}
		if err := os.Chmod(d, 0o000); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
	}
	t.Cleanup(func() {
		for _, d := range dirs {
			_ = os.Chmod(d, 0o755)
		}
	})

	sc, tree, err := scan.Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(root)
	m.mode = modeBrowse
	m.width, m.height = 80, 24
	m.scanner = sc
	m.tree = tree
	m.current = tree
	m.refreshScanErrors()
	m.buildLayout()
	return m
}

func TestErrorsScroll(t *testing.T) {
	m := modelWithErrors(t)
	m.Update(keyRunes("e"))
	if m.errScroll != 0 {
		t.Fatalf("errScroll = %d, want 0 on open", m.errScroll)
	}
	// With a single error the viewport has one line and cannot scroll; that is
	// a valid, clamped state.
	m.Update(keyType(tea.KeyDown))
	if m.errScroll != 0 {
		t.Fatalf("errScroll = %d, want clamped at 0 with a single error", m.errScroll)
	}
	// A model with several errors can scroll.
	m2 := manyErrorsModel(t, 5)
	m2.Update(keyRunes("e"))
	m2.Update(keyType(tea.KeyDown))
	if m2.errScroll != 1 {
		t.Fatalf("errScroll after down = %d, want 1", m2.errScroll)
	}
	m2.Update(keyType(tea.KeyDown))
	if m2.errScroll != 2 {
		t.Fatalf("errScroll = %d, want 2", m2.errScroll)
	}
	m2.Update(keyType(tea.KeyUp))
	if m2.errScroll != 1 {
		t.Fatalf("errScroll after up = %d, want 1", m2.errScroll)
	}
	m2.Update(keyType(tea.KeyUp))
	m2.Update(keyType(tea.KeyUp))
	if m2.errScroll != 0 {
		t.Fatalf("errScroll = %d, want clamped at 0", m2.errScroll)
	}
}

func TestErrorsWheelScroll(t *testing.T) {
	m := manyErrorsModel(t, 5)
	m.Update(keyRunes("e"))
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.errScroll != 1 {
		t.Fatalf("errScroll after wheel = %d, want 1", m.errScroll)
	}
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if m.errScroll != 0 {
		t.Fatalf("errScroll after wheel up = %d, want 0", m.errScroll)
	}
}

func TestErrorsIndicatorShownOnlyAtRoot(t *testing.T) {
	m := modelWithErrors(t)
	if !strings.Contains(m.infoLine(), "unreadable") {
		t.Fatalf("root info line missing unreadable: %q", m.infoLine())
	}
	// Drill one level down: the indicator disappears.
	m.current = m.tree.Children[0]
	m.crumbs = []*scan.Node{m.tree}
	m.buildLayout()
	if strings.Contains(m.infoLine(), "unreadable") {
		t.Fatalf("drilled info line still shows unreadable: %q", m.infoLine())
	}
}

// TestErrorsTruncatedFooter is skipped when running as root; it needs a large
// real error sample, which requires permission-denied reads.
func TestErrorsTruncatedFooter(t *testing.T) {
	root := t.TempDir()
	const n = 120
	dirs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		d := filepath.Join(root, fmt.Sprintf("secret%03d", i))
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if os.Geteuid() == 0 {
			t.Skip("running as root; permission checks are bypassed")
		}
		if err := os.Chmod(d, 0o000); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
	}
	t.Cleanup(func() {
		for _, d := range dirs {
			_ = os.Chmod(d, 0o755)
		}
	})

	sc, tree, err := scan.Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sc.TotalErrors() <= 100 {
		t.Fatalf("expected >100 errors to overflow the sample, got %d", sc.TotalErrors())
	}
	m := newModel(root)
	m.mode = modeBrowse
	m.width, m.height = 80, 24
	m.scanner = sc
	m.tree = tree
	m.current = tree
	m.refreshScanErrors()
	m.buildLayout()
	m.Update(keyRunes("e"))
	v := m.View()
	if !strings.Contains(v, "showing ") || !strings.Contains(v, "scroll for more") {
		t.Fatalf("errors view missing truncated-footer hint; got:\n%s", v)
	}
}
