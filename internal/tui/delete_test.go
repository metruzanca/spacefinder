package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/metruzanca/spacefinder/internal/scan"
)

// browseModelOf scans dir into a browsable model.
func browseModelOf(t *testing.T, dir string) *Model {
	t.Helper()
	tree, err := scan.Scan(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(dir)
	m.mode = modeBrowse
	m.width, m.height = 80, 24
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
	// The parent total must drop by the deleted node's size (kept: 2 bytes).
	if m.tree.Size != 2 {
		t.Fatalf("tree.Size = %d, want 2 after delete", m.tree.Size)
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
