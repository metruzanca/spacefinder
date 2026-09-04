package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/metruzanca/spacefinder/internal/scan"
)

func TestStatusLineFitsWidth(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		b := browseModel()
		b.width = w
		p := pickerModel()
		p.width = w
		lines := map[string]string{
			"infoBrowse": b.infoLine(),
			"helpBrowse": b.helpLine(),
			"infoPicker": p.infoLine(),
			"helpPicker": p.helpLine(),
		}
		for name, line := range lines {
			if got := lipgloss.Width(line); got > w {
				t.Fatalf("width %d: %s is %d cells wide: %q", w, name, got, line)
			}
		}
	}
}

func TestCellAt(t *testing.T) {
	m := browseModel()
	if m.cellAt(0, 0) != -1 {
		t.Fatal("breadcrumb row must not hit the treemap")
	}
	if m.cellAt(500, 500) != -1 || m.cellAt(-1, -1) != -1 {
		t.Fatal("out-of-range coordinates must yield -1")
	}
	// Pick a cell inside rect 0 and map it to screen coordinates (treemap is
	// offset by one row for the breadcrumb).
	b := cellBounds(m.rects[0])
	cx := b.x0 + (b.x1-b.x0)/2
	cy := b.y0 + (b.y1-b.y0)/2
	if got := m.cellAt(cx, cy+1); got != 0 {
		t.Fatalf("cellAt(%d,%d) = %d, want 0", cx, cy+1, got)
	}
}

func TestMouseClickSelects(t *testing.T) {
	m := browseModel()
	b := cellBounds(m.rects[1])
	cx := b.x0 + (b.x1-b.x0)/2
	cy := b.y0 + (b.y1-b.y0)/2
	got, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: cx, Y: cy + 1})
	if got.(*Model).sel != 1 {
		t.Fatalf("click selected tile %d, want 1", got.(*Model).sel)
	}
}

func TestMouseDoubleClickDrills(t *testing.T) {
	m := browseModel()
	// Select "big" (a directory) via a double-click on its tile.
	b := cellBounds(m.rects[0])
	cx := b.x0 + (b.x1-b.x0)/2
	cy := b.y0 + (b.y1-b.y0)/2
	m.lastClickTile = 0
	m.lastClick = time.Now()
	got, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: cx, Y: cy + 1})
	mm := got.(*Model)
	if mm.current != mm.tree.Children[0] {
		t.Fatalf("double-click did not drill into big; current = %v", mm.current)
	}
	if len(mm.crumbs) != 1 {
		t.Fatalf("crumbs = %d, want 1 after drill", len(mm.crumbs))
	}
}

func TestMouseWheelDoesNotCrash(t *testing.T) {
	m := browseModel()
	m.sel = 0
	for i := 0; i < 4; i++ {
		got, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
		if s := got.(*Model).sel; s < 0 || s >= len(m.rects) {
			t.Fatalf("wheel up left selection out of range: %d", s)
		}
		got, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
		if s := got.(*Model).sel; s < 0 || s >= len(m.rects) {
			t.Fatalf("wheel down left selection out of range: %d", s)
		}
	}
}

func TestMoveSelNeighborSides(t *testing.T) {
	m := fixtureModel(44, 22)
	dirs := []struct {
		dx, dy int
	}{
		{1, 0}, {-1, 0}, {0, 1}, {0, -1},
	}
	for _, d := range dirs {
		for i := range m.rects {
			if m.rects[i].Index < 0 {
				continue
			}
			m.sel = i
			before := m.rects[i]
			m.moveSel(d.dx, d.dy)
			if m.sel == i {
				continue // legitimately nothing beyond on this side
			}
			after := m.rects[m.sel]
			switch {
			case d.dx > 0:
				if after.X < before.X+before.W-1e-6 {
					t.Fatalf("rect %d moved right to %#v, not beyond %#v", i, after, before)
				}
			case d.dx < 0:
				if after.X+after.W > before.X+1e-6 {
					t.Fatalf("rect %d moved left to %#v, not left of %#v", i, after, before)
				}
			case d.dy > 0:
				if after.Y < before.Y+before.H-1e-6 {
					t.Fatalf("rect %d moved down to %#v, not below %#v", i, after, before)
				}
			case d.dy < 0:
				if after.Y+after.H > before.Y+1e-6 {
					t.Fatalf("rect %d moved up to %#v, not above %#v", i, after, before)
				}
			}
		}
	}
}

func TestMoveSelRightPrefersAdjacentAligned(t *testing.T) {
	m := fixtureModel(44, 22)
	// Starting from the top-left tile (games), moving right should land on the
	// block sharing its right edge with the best vertical alignment.
	m.sel = 0
	cur := m.rects[0]
	m.moveSel(1, 0)
	got := m.rects[m.sel]
	if got.X < cur.X+cur.W-1e-6 {
		t.Fatalf("right move landed on %#v, which is not to the right of %#v", got, cur)
	}
	// Ideally it is the immediately adjacent block.
	if got.X > cur.X+cur.W+cur.W*0.2 {
		t.Fatalf("right move skipped several blocks: %#v vs %#v", got, cur)
	}
}

func TestMoveSelBoundaryStays(t *testing.T) {
	m := fixtureModel(44, 22)
	for i := range m.rects {
		if m.rects[i].Index < 0 {
			continue
		}
		// The bottom-most band (misc) cannot go down; the left-most cannot go
		// left; etc. These must not wander off the map.
		m.sel = i
		m.moveSel(0, 1)
		m.moveSel(-1, 0)
		if m.sel < 0 || m.sel >= len(m.rects) {
			t.Fatalf("selection left the map after moves from %d", i)
		}
	}
}

// TestSelRestoresOnUp checks up() still restores the selection to the block
// that was drilled.
func TestSelRestoresOnUp(t *testing.T) {
	m := browseModel()
	got, _ := m.Update(keyType(tea.KeyEnter))
	m = got.(*Model)
	saved := m.sel
	_ = saved
	got, _ = m.Update(keyType(tea.KeyEsc))
	m = got.(*Model)
	if m.selectedNode() == nil || m.selectedNode().Name != "big" {
		t.Fatal("selection not restored to big after going up")
	}
}

// TestInsignificantChildrenHidden verifies that children too small to earn a
// tile are neither rendered nor selectable, and are surfaced through the
// hidden count.
func TestInsignificantChildrenHidden(t *testing.T) {
	m := newModel("/")
	m.mode = modeBrowse
	m.width, m.height = 40, 20
	const bigCount = 5
	const tinyCount = 100
	children := make([]*scan.Node, 0, bigCount+tinyCount)
	for i := 0; i < bigCount; i++ {
		children = append(children, &scan.Node{Name: "big" + string(rune('a'+i)), Path: "/", IsDir: true, Size: 1_000_000})
	}
	for i := 0; i < tinyCount; i++ {
		children = append(children, &scan.Node{Name: "t" + string(rune('a'+i)), Path: "/", IsDir: true, Size: 1})
	}
	m.tree = &scan.Node{Name: "fs", Path: "/", IsDir: true, Children: children}
	m.current = m.tree
	m.buildLayout()

	if m.hidden != tinyCount {
		t.Fatalf("hidden = %d, want %d", m.hidden, tinyCount)
	}
	// No rendered tile may refer to a tiny child.
	for _, r := range m.rects {
		if r.Index >= bigCount {
			t.Fatalf("tiny child %d rendered as a tile", r.Index)
		}
	}
}

// TestLabelContrastCheck asserts that label text adapts to the fill: bright
// fills get near-black labels, dark fills white ones, and every hue
// spacefinder can produce stays readable (WCAG AA-large, ≥ 3:1).
func TestLabelContrastCheck(t *testing.T) {
	if got := labelFG("#ffff00"); got != lipgloss.Color("#14181c") {
		t.Fatalf("label on yellow = %v, want dark text", got)
	}
	if got := labelFG("#00008b"); got != lipgloss.Color("#ffffff") {
		t.Fatalf("label on dark blue = %v, want white text", got)
	}
	worst := 1e9
	for hue := 0; hue < 360; hue += 3 {
		for _, v := range []float64{0.72, 0.95} {
			hex := hsvHex(float64(hue), 0.55, v)
			if c := bestContrast(hex); c < worst {
				worst = c
			}
		}
	}
	if worst < 3.0 {
		t.Fatalf("worst label contrast over the hue circle is %.2f, below the floor", worst)
	}
}

// bestContrast is the larger of the white/dark contrast ratios against a fill.
func bestContrast(hex string) float64 {
	L := luminance(hex)
	white := 1.05 / (L + 0.05)
	dark := (L + 0.05) / 0.0585
	if white >= dark {
		return white
	}
	return dark
}

func TestNameHueStable(t *testing.T) {
	a1, _ := nameColor("node_modules")
	a2, _ := nameColor("node_modules")
	if a1 != a2 {
		t.Fatalf("nameColor not stable: %s vs %s", a1, a2)
	}
	// A sample of distinct names should spread across the hue circle.
	names := []string{"dev", "downloads", ".cache", "Documents", "Pictures", "go", ".local"}
	seen := map[string]bool{}
	for _, n := range names {
		c, _ := nameColor(n)
		seen[c] = true
	}
	if len(seen) < 3 {
		t.Fatalf("expected distinct hues, got %d shared colours", len(seen))
	}
}

func TestHiddenCount(t *testing.T) {
	m := browseModel()
	m.tree.Children = append(m.tree.Children, &scan.Node{Name: "empty", Path: "/tmp/root/empty", Size: 0})
	m.buildLayout()
	if m.hidden != 1 {
		t.Fatalf("hidden = %d, want 1", m.hidden)
	}
}

func TestEmptyDirView(t *testing.T) {
	m := browseModel()
	m.current = &scan.Node{Name: "nope", Path: "/tmp/root/nope", IsDir: true}
	m.buildLayout()
	if len(m.rects) != 0 {
		t.Fatalf("rects = %d, want none", len(m.rects))
	}
	v := m.View()
	if !strings.Contains(v, "empty directory") {
		t.Fatal("empty directory state not shown")
	}
	if !strings.Contains(v, "press esc to go up") {
		t.Fatal("go-up hint missing in empty state")
	}
}

func TestTooSmallView(t *testing.T) {
	m := browseModel()
	m.width, m.height = 12, 5
	if v := m.View(); !strings.Contains(v, "too small") {
		t.Fatalf("too-small message missing: %q", v)
	}
}

func TestLayoutSizeSqrt(t *testing.T) {
	if layoutSize(0) != 0 {
		t.Fatal("zero stays zero")
	}
	if layoutSize(-5) != 0 {
		t.Fatal("negative stays zero")
	}
	// sqrt compresses the spread: 1:4 bytes maps to a much tighter ratio.
	big, small := layoutSize(4), layoutSize(1)
	if float64(big)/float64(small) >= 4 {
		t.Fatalf("sqrt scaling not compressed: %d vs %d", big, small)
	}
	// Monotonic + ranking-preserving.
	if layoutSize(100) <= layoutSize(10) {
		t.Fatal("layoutSize not monotonic")
	}
}

func TestFreeGutterAtRoot(t *testing.T) {
	m := browseModel()
	m.freeBytes = 10 * 1024 * 1024 * 1024
	m2 := *m
	// Heights: 24 rows total, minus breadcrumb+status = 22; gutter 1 row.
	if m2.freeRows() != 1 {
		t.Fatalf("freeRows = %d, want 1", m2.freeRows())
	}
	v := m.View()
	if !strings.Contains(v, "free ·") {
		t.Fatal("free gutter missing at root")
	}
	// Drilled below the root: no gutter.
	m.current = m.tree.Children[0]
	m.crumbs = []*scan.Node{m.tree}
	if m.freeRows() != 0 {
		t.Fatal("freeRows should be zero below the root")
	}
	if v := m.View(); strings.Contains(v, "free ·") {
		t.Fatal("free gutter shown below the root")
	}
}

func TestFreeRowsCapped(t *testing.T) {
	m := browseModel()
	m.freeBytes = 1 << 40
	for _, h := range []int{24, 40, 60, 100} {
		m.height = h
		if n := m.freeRows(); n < 1 || n > 3 {
			t.Fatalf("height %d: freeRows = %d, want 1..3", h, n)
		}
	}
}
