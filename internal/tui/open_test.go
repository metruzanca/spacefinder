package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// doubleClickAt sends two left-button presses at the centre of tile's cells,
// the second acting as a double-click within the debounce window.
func doubleClickAt(m *Model, tile int) (tea.Model, tea.Cmd) {
	b := cellBounds(m.rects[tile])
	cx := b.x0 + (b.x1-b.x0)/2
	cy := b.y0 + (b.y1-b.y0)/2
	msg := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: cx, Y: cy + 1}
	got, _ := m.Update(msg) // first click selects
	m = got.(*Model)
	m.lastClickTile = tile
	m.lastClick = time.Now()
	return m.Update(msg) // second click is the double-click
}

func TestMouseDoubleClickOpensFile(t *testing.T) {
	m := browseModel()
	// "small" is a file; find the tile it occupies.
	fileTile := -1
	for i := range m.rects {
		idx := m.rects[i].Index
		if idx >= 0 && idx < len(m.current.Children) && m.current.Children[idx].Name == "small" {
			fileTile = i
			break
		}
	}
	if fileTile < 0 {
		t.Fatal("file 'small' not laid out")
	}

	got, cmd := doubleClickAt(m, fileTile)
	mm := got.(*Model)
	if cmd == nil {
		t.Fatal("double-click on a file returned no open command")
	}
	if mm.mode != modeBrowse {
		t.Fatalf("mode = %d, want modeBrowse after open", mm.mode)
	}
	if mm.current != m.current {
		t.Fatal("opening a file must not change the current directory")
	}

	// Running the command must not touch the model; a missing opener surfaces
	// an openDoneMsg error rather than panicking.
	if msg := cmd(); msg != nil {
		switch msg.(type) {
		case openDoneMsg:
		default:
			t.Fatalf("open command returned %T, want openDoneMsg", msg)
		}
	}
}

func TestMouseDoubleClickDrillStillWorks(t *testing.T) {
	m := browseModel()
	got, cmd := doubleClickAt(m, 0) // "big" is a directory
	mm := got.(*Model)
	if mm.current != mm.tree.Children[0] {
		t.Fatalf("double-click on dir did not drill; current = %v", mm.current)
	}
	_ = cmd
	if len(mm.crumbs) != 1 {
		t.Fatalf("crumbs = %d, want 1 after drill", len(mm.crumbs))
	}
}