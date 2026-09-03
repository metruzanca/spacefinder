package tui

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/metruzanca/spacefinder/internal/scan"
)

// testTree builds a small in-memory tree.
func testTree() *scan.Node {
	return &scan.Node{
		Name:  "root",
		Path:  "/tmp/root",
		IsDir: true,
		Size:  600,
		Children: []*scan.Node{
			{Name: "big", Path: "/tmp/root/big", IsDir: true, Size: 400},
			{Name: "small", Path: "/tmp/root/small", IsDir: false, Size: 200},
		},
	}
}

func browseModel() *Model {
	m := newModel("/tmp/root")
	m.mode = modeBrowse
	m.width, m.height = 80, 24
	m.tree = testTree()
	m.current = m.tree
	m.buildLayout()
	return m
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyType(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

func TestBrowseDrillAndUp(t *testing.T) {
	m := browseModel()
	if m.selectedNode() == nil || m.selectedNode().Name != "big" {
		t.Fatalf("initial selection = %#v, want big", m.selectedNode())
	}
	// Drill into "big" (a directory).
	got, _ := m.Update(keyType(tea.KeyEnter))
	m = got.(*Model)
	if m.current != m.tree.Children[0] {
		t.Fatalf("current after drill = %v, want big", m.current)
	}
	if len(m.crumbs) != 1 {
		t.Fatalf("crumbs = %d, want 1", len(m.crumbs))
	}
	// Esc goes back up and restores the selection to big.
	got, _ = m.Update(keyType(tea.KeyEsc))
	m = got.(*Model)
	if m.current != m.tree {
		t.Fatalf("current after esc = %v, want root", m.current)
	}
	if m.selectedNode() == nil || m.selectedNode().Name != "big" {
		t.Fatalf("selection after esc = %#v, want big", m.selectedNode())
	}
}

func TestDrillFileNoop(t *testing.T) {
	m := browseModel()
	found := false
	for i := range m.rects {
		idx := m.rects[i].Index
		if idx >= 0 && idx < len(m.current.Children) && m.current.Children[idx].Name == "small" {
			m.sel = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("small not laid out")
	}
	got, _ := m.Update(keyType(tea.KeyEnter))
	m = got.(*Model)
	if m.current != m.tree {
		t.Fatalf("drilled into a file; current should stay at root")
	}
}

func TestMoveSelDirections(t *testing.T) {
	m := browseModel()
	left := m.sel
	m.moveSel(-1, 0)
	if m.sel != left {
		t.Fatalf("nothing left of %d; selection should not move", left)
	}
	// With two children big (400) and small (200), layouts are side by side or
	// stacked; movement must stay within the valid range.
	m.sel = 0
	m.moveSel(1, 0)
	if m.sel < 0 {
		t.Fatal("selection went negative")
	}
}

// TestProgramQuitsOnQ runs a real bubbletea Program and confirms that sending
// "q" ends it cleanly (no pty required).
func TestProgramQuitsOnQ(t *testing.T) {
	var out bytes.Buffer
	p := tea.NewProgram(browseModel(),
		tea.WithInput(strings.NewReader("q")),
		tea.WithOutput(&out),
		tea.WithoutRenderer(),
	)
	if _, err := p.Run(); err != nil {
		t.Fatalf("program exited with error: %v", err)
	}
}
