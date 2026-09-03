package tui

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/metruzanca/spacefinder/internal/scan"
)

// statSize returns the du-style allocated size of a path for test assertions.
func statSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return info.Size()
}

// browseModelOf measures dir into a browsable model.
func browseModelOf(t *testing.T, dir string) *Model {
	t.Helper()
	sc, tree, err := scan.Measure(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(dir)
	m.mode = modeBrowse
	m.width, m.height = 80, 24
	m.scanner = sc
	m.tree = tree
	m.current = tree
	m.buildLayout()
	t.Cleanup(m.closeConfirm)
	return m
}

// selectPath moves the selection to the rectangle for the given path.
func selectPath(m *Model, path string) bool {
	for i := range m.rects {
		idx := m.rects[i].Index
		if idx >= 0 && idx < len(m.current.Children) && m.current.Children[idx].Path == path {
			m.sel = i
			return true
		}
	}
	return false
}

func TestDeleteRequiresExactName(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(root, "victim.txt")
	keeper := filepath.Join(root, "keeper.txt")
	if err := os.WriteFile(victim, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keeper, []byte("yy"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := browseModelOf(t, root)
	if !selectPath(m, victim) {
		t.Fatal("could not select victim.txt")
	}
	m.openConfirm()
	if m.mode != modeConfirm {
		t.Fatalf("mode = %d, want modeConfirm", m.mode)
	}
	if m.confirmNode == nil || m.confirmNode.Path != victim {
		t.Fatalf("confirmNode = %#v, want victim", m.confirmNode)
	}

	// Wrong name: nothing is deleted, back to the prompt with an error shown.
	m.input.SetValue("nope")
	got, _ := m.Update(keyType(tea.KeyEnter))
	m = got.(*Model)
	if m.mode != modeConfirm {
		t.Fatalf("mode = %d, want modeConfirm after wrong name", m.mode)
	}
	if !m.confirmErr {
		t.Fatal("confirmErr not set after wrong name")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("victim was deleted on mismatch: %v", err)
	}

	// Exact name: deleted, wrapped back to browse, tree updated.
	m.input.SetValue("victim.txt")
	got, _ = m.Update(keyType(tea.KeyEnter))
	m = got.(*Model)
	if m.mode != modeBrowse {
		t.Fatalf("mode = %d, want modeBrowse after delete", m.mode)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Fatalf("victim still exists after exact-name delete: %v", err)
	}
	if _, err := os.Stat(keeper); err != nil {
		t.Fatalf("keeper.txt was wrongly deleted: %v", err)
	}
	for _, c := range m.current.Children {
		if c.Path == victim {
			t.Fatal("victim still in children after delete")
		}
	}
	// The parent total must drop by the deleted node's size (keeper only).
	if m.tree.Size != statSize(t, keeper) {
		t.Fatalf("tree.Size = %d, want %d after delete", m.tree.Size, statSize(t, keeper))
	}
}

func TestConfirmEscCancels(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(root, "victim.txt")
	if err := os.WriteFile(victim, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := browseModelOf(t, root)
	selectPath(m, victim)
	m.openConfirm()
	m.input.SetValue("victim.txt")
	got, _ := m.Update(keyType(tea.KeyEsc))
	m = got.(*Model)
	if m.mode != modeBrowse {
		t.Fatalf("mode = %d, want modeBrowse after esc", m.mode)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("victim removed by esc: %v", err)
	}
}

func TestDeleteRefusesNoSelection(t *testing.T) {
	root := t.TempDir()
	m := browseModelOf(t, root)
	// Point the selection into the void; opening the confirm modal must no-op.
	m.sel = -1
	m.openConfirm()
	if m.mode == modeConfirm {
		t.Fatal("confirm modal opened without a selectable node")
	}
}

func TestDrillLazyExpand(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "deep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := browseModelOf(t, root)
	if !selectPath(m, filepath.Join(root, "sub")) {
		t.Fatal("could not select sub")
	}
	if m.selectedNode().Children != nil {
		t.Fatal("sub should be thin (nil children) before drill")
	}

	// Enter starts measure-then-open: mode switches to measuring and a command
	// is scheduled.
	got, cmd := m.Update(keyType(tea.KeyEnter))
	m = got.(*Model)
	if m.mode != modeMeasuring {
		t.Fatalf("mode = %d, want modeMeasuring", m.mode)
	}
	if m.pending == nil || m.pending.Path != filepath.Join(root, "sub") {
		t.Fatalf("pending = %#v, want sub", m.pending)
	}

	// Run the scheduled command synchronously to get the done message.
	if cmd == nil {
		t.Fatal("drill returned no command to expand")
	}
	done := cmd()
	got, _ = m.Update(done)
	m = got.(*Model)
	if m.mode != modeBrowse {
		t.Fatalf("mode = %d, want modeBrowse after expand", m.mode)
	}
	if m.current.Path != filepath.Join(root, "sub") {
		t.Fatalf("current = %s, want sub", m.current.Path)
	}
	if len(m.current.Children) != 1 {
		t.Fatalf("sub children = %d, want 1 after expand", len(m.current.Children))
	}
	if m.current.Size != statSize(t, filepath.Join(root, "sub", "deep.txt")) {
		t.Fatalf("sub.Size = %d, want %d", m.current.Size, statSize(t, filepath.Join(root, "sub", "deep.txt")))
	}
}

func TestDrillStaleExpandIgnored(t *testing.T) {
	root := t.TempDir()
	m := browseModelOf(t, root)
	// A stale completion for a node nobody asked for must be ignored.
	some := &scan.Node{Name: "ghost", Path: filepath.Join(root, "ghost"), IsDir: true}
	got, _ := m.Update(expandDoneMsg{node: some})
	if got.(*Model).mode != modeBrowse {
		t.Fatal("stale expandDoneMsg changed mode")
	}
	if got.(*Model).current != m.current {
		t.Fatal("stale expandDoneMsg changed the current directory")
	}
}
