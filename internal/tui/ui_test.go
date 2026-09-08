package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/metruzanca/spacefinder/internal/scan"
	"github.com/metruzanca/spacefinder/internal/treemap"
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
			beforePage := m.page
			m.moveSel(d.dx, d.dy)
			if m.sel == i {
				continue // legitimately nothing beyond on this side
			}
			if m.page != beforePage {
				continue // wrapped to the neighbouring page
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

// TestPagesReachAllChildren verifies that children too small to fit on the
// first page are chunked onto later pages at a legible minimum size, and that
// every non-zero child is reachable across the pages (nothing is silently
// folded away except zero-size entries, which stay hidden).
func TestPagesReachAllChildren(t *testing.T) {
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

	if m.hidden != 0 {
		t.Fatalf("hidden = %d, want 0 (only zero-size entries are dropped)", m.hidden)
	}
	if m.pageCount < 2 {
		t.Fatalf("pageCount = %d, want multiple pages for 105 children", m.pageCount)
	}
	// Every non-zero child is reachable across the pages, and no page renders a
	// tile below the label floor.
	w, h := m.treemapSize()
	seen := map[int]bool{}
	for p := 0; p < m.pageCount; p++ {
		m.layoutPage(p, w, h)
		for _, ci := range m.pageIdx {
			if seen[ci] {
				t.Fatalf("child %d appears on more than one page", ci)
			}
			seen[ci] = true
		}
		for _, r := range m.rects {
			if r.Index < 0 {
				continue
			}
			if r.W < minLabelCols || r.H < minLabelRows {
				t.Fatalf("page %d has a tile below the label floor: %#v", p, r)
			}
		}
	}
	if len(seen) != bigCount+tinyCount {
		t.Fatalf("reachable children = %d, want %d", len(seen), bigCount+tinyCount)
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

// TestPaletteDistinct asserts the palette holds no near-duplicate colours: every
// pair of hues stays ≥30° apart around the wheel, and each renders a unique
// fill, so two tiles can only look the same when they are not adjacent. The
// palette order is a priority, not a sort, so the check is order-independent.
func TestPaletteDistinct(t *testing.T) {
	dist := func(a, b float64) float64 {
		d := a - b
		if d < 0 {
			d = -d
		}
		if d > 180 {
			d = 360 - d
		}
		return d
	}
	for i := 0; i < len(tilePalette); i++ {
		for j := i + 1; j < len(tilePalette); j++ {
			if d := dist(tilePalette[i], tilePalette[j]); d < 30 {
				t.Fatalf("palette hues %v and %v too close together: %v", tilePalette[i], tilePalette[j], tilePalette)
			}
		}
	}
	seen := map[string]bool{}
	for _, hue := range tilePalette {
		seen[hsvHex(hue, 0.55, 0.72)] = true
	}
	if len(seen) != len(tilePalette) {
		t.Fatalf("palette produced %d distinct fills from %d hues", len(seen), len(tilePalette))
	}
}

// TestAdjacentTilesDistinct builds a layout of three mutually edge-adjacent
// rectangles and asserts assignColors gives each a different palette colour, so
// no two tiles the user sees side by side ever blend together.
func TestAdjacentTilesDistinct(t *testing.T) {
	rects := []treemap.Rect{
		{Index: 0, X: 0, Y: 0, W: 5, H: 3},
		{Index: 1, X: 5, Y: 0, W: 5, H: 3},
		{Index: 2, X: 0, Y: 3, W: 10, H: 3},
	}
	const w, h = 10, 6
	cols := assignColors(rects, treemap.Raster(rects, w, h), w, h)
	for i := range cols {
		if cols[i] < 0 {
			t.Fatalf("real rect %d got the 'other' bucket colour", i)
		}
	}
	if cols[0] == cols[1] || cols[0] == cols[2] || cols[1] == cols[2] {
		t.Fatalf("mutually adjacent rects share colours: %v", cols)
	}

	// The "other" bucket never takes a palette colour and never forces one on
	// its neighbours.
	rects = append(rects, treemap.Rect{Index: -1, X: 0, Y: 6, W: 10, H: 3})
	cols = assignColors(rects, treemap.Raster(rects, w, 9), w, 9)
	if cols[3] != -1 {
		t.Fatalf("other bucket colour = %d, want -1", cols[3])
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

// multiPageModel builds a level with enough equal-sized children that several
// pages of floored tiles are required.
func multiPageModel() *Model {
	m := newModel("/")
	m.mode = modeBrowse
	m.width, m.height = 80, 24
	children := make([]*scan.Node, 0, 160)
	for i := 0; i < 160; i++ {
		children = append(children, &scan.Node{Name: fmt.Sprintf("f%03d", i), Path: "/", IsDir: false, Size: 1000})
	}
	m.tree = &scan.Node{Name: "fs", Path: "/", IsDir: true, Children: children}
	m.current = m.tree
	m.buildLayout()
	return m
}

// selectChild navigates to the tile for the named child on whichever page it
// sits, leaving the model laid out on that page.
func selectChild(t *testing.T, m *Model, name string) {
	t.Helper()
	w, h := m.treemapSize()
	for p := 0; p < m.pageCount; p++ {
		m.layoutPage(p, w, h)
		for ri := range m.rects {
			idx := m.rects[ri].Index
			if idx >= 0 && idx < len(m.pageIdx) && m.current.Children[m.pageIdx[idx]].Name == name {
				m.sel = ri
				return
			}
		}
	}
	t.Fatalf("child %q not found on any page", name)
}

func TestTabFlipsPages(t *testing.T) {
	m := multiPageModel()
	if m.pageCount < 2 {
		t.Skipf("fixture produced %d pages, want 2+", m.pageCount)
	}
	if m.page != 0 {
		t.Fatalf("initial page = %d, want 0", m.page)
	}
	got, _ := m.Update(keyType(tea.KeyTab))
	m = got.(*Model)
	if m.page != 1 {
		t.Fatalf("tab left page %d, want 1", m.page)
	}
	if m.sel != firstSelectable(m.rects) {
		t.Fatal("tab did not select the first tile of the new page")
	}
	// PgUp goes back; Shift+Tab also goes back.
	got, _ = m.Update(keyType(tea.KeyPgUp))
	m = got.(*Model)
	if m.page != 0 {
		t.Fatalf("pgup left page %d, want 0", m.page)
	}
	got, _ = m.Update(keyType(tea.KeyShiftTab))
	m = got.(*Model)
	if m.page != 0 {
		t.Fatalf("shift+tab wrapped past the first page to %d", m.page)
	}
}

func TestMoveSelWrapsPages(t *testing.T) {
	m := multiPageModel()
	if m.pageCount < 2 {
		t.Skipf("fixture produced %d pages, want 2+", m.pageCount)
	}
	// Moving past the last tile flips forward.
	m.sel = lastSelectable(m.rects)
	before := m.page
	m.moveSel(1, 0)
	if m.page != before+1 {
		t.Fatalf("right past the last tile: page %d, want %d", m.page, before+1)
	}
	if m.sel != firstSelectable(m.rects) {
		t.Fatal("forward wrap did not land on the first tile")
	}
	// Moving past the first tile flips back.
	m.sel = firstSelectable(m.rects)
	before = m.page
	m.moveSel(-1, 0)
	if m.page != before-1 {
		t.Fatalf("left past the first tile: page %d, want %d", m.page, before-1)
	}
	if m.sel != lastSelectable(m.rects) {
		t.Fatal("backward wrap did not land on the last tile")
	}
	// Vertical moves wrap too.
	m.sel = lastSelectable(m.rects)
	before = m.page
	m.moveSel(0, 1)
	if m.page != before+1 {
		t.Fatalf("down past the last tile: page %d, want %d", m.page, before+1)
	}
}

func TestUpRestoresSelectionAcrossPages(t *testing.T) {
	m := multiPageModel()
	m.current.Children = append(m.current.Children, &scan.Node{Name: "zdir", Path: "/zdir", IsDir: true, Size: 1000})
	m.buildLayout()
	if m.pageCount < 2 {
		t.Skipf("fixture produced %d pages, want 2+", m.pageCount)
	}
	selectChild(t, m, "zdir")
	dirPage := m.page
	if dirPage == 0 {
		t.Skip("zdir landed on the first page; adjust the fixture")
	}
	got, _ := m.Update(keyType(tea.KeyEnter)) // drill into zdir
	m = got.(*Model)
	if m.current.Name != "zdir" {
		t.Fatalf("current = %v, want zdir", m.current.Name)
	}
	got, _ = m.Update(keyType(tea.KeyEsc)) // back up
	m = got.(*Model)
	if m.page != dirPage {
		t.Fatalf("after esc page = %d, want %d", m.page, dirPage)
	}
	if n := m.selectedNode(); n == nil || n.Name != "zdir" {
		t.Fatalf("selection after esc = %#v, want zdir", n)
	}
}

func TestPageIndicatorShown(t *testing.T) {
	m := multiPageModel()
	if m.pageCount < 2 {
		t.Skipf("fixture produced %d pages, want 2+", m.pageCount)
	}
	if !strings.Contains(m.infoLine(), "page 1/"+strconv.Itoa(m.pageCount)) {
		t.Fatalf("info line missing page indicator: %q", m.infoLine())
	}
	// A single-page level shows no indicator.
	s := browseModel()
	if strings.Contains(s.infoLine(), "page 1/1") {
		t.Fatal("single-page level should not show a page indicator")
	}
}

// TestSharedScaleAcrossPages verifies that pages share one bytes→cells scale
// instead of re-normalizing each page to fill the screen: the dominant item on
// page 1 keeps (nearly) its true share of the grid, and the tail on later
// pages stays at the minimum tile area rather than being blown up to page
// size, leaving empty gaps on sparse pages.
func TestSharedScaleAcrossPages(t *testing.T) {
	m := newModel("/")
	m.mode = modeBrowse
	m.width, m.height = 80, 24
	children := []*scan.Node{
		{Name: "huge", Path: "/huge", IsDir: true, Size: 1 << 33},
	}
	for i := 0; i < 40; i++ {
		children = append(children, &scan.Node{Name: fmt.Sprintf("f%02d", i), Path: "/x", IsDir: false, Size: 1 << 10})
	}
	m.tree = &scan.Node{Name: "fs", Path: "/", IsDir: true, Children: children}
	m.current = m.tree
	m.buildLayout()
	if m.pageCount < 2 {
		t.Skipf("fixture produced %d pages, want 2+", m.pageCount)
	}
	w, h := m.treemapSize()
	grid := float64(w * h)

	// Page 1: the dominant item keeps its true ~98% share of the grid.
	m.layoutPage(0, w, h)
	p1max := 0.0
	for _, r := range m.rects {
		if r.Index >= 0 && r.W*r.H > p1max {
			p1max = r.W * r.H
		}
	}
	if p1max < 0.9*grid {
		t.Fatalf("page 1 dominant tile uses %.1f%% of the grid, want ≥90%%", 100*p1max/grid)
	}

	// Page 2: the tail is clamped to the minimum area (not re-scaled to page
	// size), so the page is sparse with leftover gap cells.
	m.layoutPage(1, w, h)
	empty := 0
	for _, c := range m.raster {
		if c == -1 {
			empty++
		}
	}
	if empty == 0 {
		t.Fatal("shared scale should leave empty gaps on a sparse tail page")
	}
	for _, r := range m.rects {
		if r.Index < 0 {
			continue
		}
		if r.W*r.H > 2*minTileArea {
			t.Fatalf("tail tile %#v inflated beyond the minimum, breaking the shared scale", r)
		}
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
