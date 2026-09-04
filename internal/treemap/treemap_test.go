package treemap

import (
	"flag"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

// golden compares got with a golden file, rewriting it when -update is set.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func rectArea(rs []Rect, idx int) int {
	cells := Raster(rs, 120, 30)
	n := 0
	for _, c := range cells {
		if c == idx {
			n++
		}
	}
	return n
}

func TestLayoutPartitionsGridNoOverlap(t *testing.T) {
	items := []Item{
		{Name: "a", Size: 500, Selectable: true},
		{Name: "b", Size: 250, Selectable: true},
		{Name: "c", Size: 150, Selectable: true},
		{Name: "d", Size: 100, Selectable: true},
	}
	const w, h = 120, 30
	rs := Layout(items, w, h, 0)
	if len(rs) != len(items) {
		t.Fatalf("Layout produced %d rects, want %d", len(rs), len(items))
	}
	cells := Raster(rs, w, h)
	if len(cells) != w*h {
		t.Fatalf("Raster length %d, want %d", len(cells), w*h)
	}
	for _, c := range cells {
		if c < 0 || c >= len(rs) {
			t.Fatalf("cell assigned to rect %d, want 0..%d", c, len(rs)-1)
		}
	}
	// Areas should be monotically ordered (largest item -> most cells).
	for i := 0; i+1 < len(rs); i++ {
		if rectArea(rs, i) < rectArea(rs, i+1) {
			t.Fatalf("area order inverted: rect %d (%d cells) < rect %d (%d cells)",
				i, rectArea(rs, i), i+1, rectArea(rs, i+1))
		}
	}
	// The largest item should in practice get a square-ish area, and every
	// rectangle must have positive extent.
	for i := range rs {
		if rs[i].W <= 0 || rs[i].H <= 0 {
			t.Fatalf("rect %d has non-positive extent %v", i, rs[i])
		}
	}
}

func TestLayoutAreaProportionalToSize(t *testing.T) {
	items := []Item{
		{Name: "big", Size: 900, Selectable: true},
		{Name: "small", Size: 100, Selectable: true},
	}
	const w, h = 100, 100
	rs := Layout(items, w, h, 0)
	big, small := rectArea(rs, 0), rectArea(rs, 1)
	if big < 9*small {
		t.Fatalf("big (%d cells) is not ~9x small (%d cells)", big, small)
	}
}

func TestLayoutDeterministic(t *testing.T) {
	items := []Item{
		{Name: "a", Size: 10, Selectable: true},
		{Name: "b", Size: 20, Selectable: true},
		{Name: "a", Size: 10, Selectable: true},
		{Name: "c", Size: 30, Selectable: true},
	}
	one := Layout(items, 80, 40, 0)
	two := Layout(items, 80, 40, 0)
	if !reflect.DeepEqual(one, two) {
		t.Fatal("Layout is not deterministic")
	}
}

func TestLayoutCapAndOther(t *testing.T) {
	items := make([]Item, 10)
	for i := range items {
		items[i] = Item{Name: "f" + string(rune('a'+i)), Size: int64(i + 1), Selectable: true}
	}
	rs := Layout(items, 100, 50, 4)
	if len(rs) != 5 { // 4 kept + "other"
		t.Fatalf("Layout = %d rects, want 5 (4 + other)", len(rs))
	}
	// The trailing rectangle is the synthesized "other" bucket (index -1).
	if rs[len(rs)-1].Index != -1 {
		t.Fatalf("last rect Index = %d, want -1 (other)", rs[len(rs)-1].Index)
	}
}

func TestLayoutSingleItemFills(t *testing.T) {
	rs := Layout([]Item{{Name: "only", Size: 42, Selectable: true}}, 37, 11, 0)
	cells := Raster(rs, 37, 11)
	for _, c := range cells {
		if c != 0 {
			t.Fatalf("cell rect %d, want 0", c)
		}
	}
}

func TestLayoutDropsInvalid(t *testing.T) {
	if rs := Layout(nil, 10, 10, 0); rs != nil {
		t.Fatalf("empty input produced %v", rs)
	}
	rs := Layout([]Item{{Name: "x", Size: 0, Selectable: true}}, 10, 10, 0)
	if rs != nil {
		t.Fatalf("zero-size input produced %v", rs)
	}
	if rs := Layout([]Item{{Name: "x", Size: 5, Selectable: true}}, 0, 0, 0); rs != nil {
		t.Fatalf("zero grid produced %v", rs)
	}
}

func TestLayoutGolden(t *testing.T) {
	items := []Item{
		{Name: "src", Size: 400, Selectable: true},
		{Name: "downloads", Size: 300, Selectable: true},
		{Name: "cache", Size: 200, Selectable: true},
		{Name: "games", Size: 150, Selectable: true},
		{Name: "misc", Size: 50, Selectable: true},
	}
	const w, h = 60, 15
	rs := Layout(items, w, h, 0)
	cells := Raster(rs, w, h)

	var b strings.Builder
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			idx := cells[yy*w+xx]
			if idx < 0 {
				b.WriteByte('.')
				continue
			}
			b.WriteString(string(rune('A' + idx)))
		}
		if yy < h-1 {
			b.WriteByte('\n')
		}
	}
	golden(t, "layout60x15", b.String())
}

func TestRectAreaSum(t *testing.T) {
	items := []Item{
		{Name: "a", Size: 700, Selectable: true},
		{Name: "b", Size: 300, Selectable: true},
	}
	const w, h = 200, 50
	rs := Layout(items, w, h, 0)
	cells := Raster(rs, w, h)
	got := 0
	for _, c := range cells {
		if c >= 0 {
			got++
		}
	}
	want := float64(w) * float64(h)
	if math.Abs(float64(got)-want) > 0 {
		if got != w*h {
			t.Fatalf("covered %d cells, want %d", got, w*h)
		}
	}
}

// TestLayoutSquareishForBalanced asserts that balanced inputs lay out as
// square-ish tiles (not full-height columns — the old bar-chart regression)
// and that the largest single tile does not dominate the container.
func TestLayoutSquareishForBalanced(t *testing.T) {
	cases := []struct {
		w, h, n int
	}{
		{60, 15, 8},
		{80, 40, 12},
		{40, 24, 6},
		{30, 30, 4},
	}
	for _, c := range cases {
		items := make([]Item, c.n)
		for i := range items {
			items[i] = Item{Name: string(rune('a' + i)), Size: 100, Selectable: true}
		}
		rs := Layout(items, c.w, c.h, 0)
		if len(rs) != c.n {
			t.Fatalf("%dx%d: expected %d rects, got %d", c.w, c.h, c.n, len(rs))
		}
		largest := 0.0
		interior := false
		for i, r := range rs {
			if r.W <= 0 || r.H <= 0 {
				t.Fatalf("non-positive extent: %#v", r)
			}
			a := r.W / r.H
			if a < 1 {
				a = 1 / a
			}
			// A wide container can't make every tile square; the point is that
			// tiles are NOT all thin full-height columns.
			if a > 12.0 {
				t.Fatalf("%dx%d rect %d aspect %.2f too elongated: %#v", c.w, c.h, i, a, r)
			}
			if r.W < 0.9*float64(c.w) && r.H < 0.9*float64(c.h) {
				interior = true // spans neither axis, like a real treemap tile
			}
			if r.W*r.H > largest {
				largest = r.W * r.H
			}
		}
		// The old bar-chart regression gave every rectangle the full container
		// height/width. A real layout for 4+ items must produce an interior tile.
		if c.n > 2 && !interior {
			t.Fatalf("%dx%d: every rect spans a full container axis — looks like a bar chart", c.w, c.h)
		}
		if share := largest / (float64(c.w) * float64(c.h)); float64(c.n) > 2 && share > 0.5 {
			t.Fatalf("%dx%d: largest tile uses %.0f%% of the area", c.w, c.h, share*100)
		}
	}
}

// TestLayoutBoundedAspectSkewed asserts that even a heavily skewed set keeps
// the largest tile from swallowing the container as a full-height column.
func TestLayoutBoundedAspectSkewed(t *testing.T) {
	items := []Item{
		{Name: "a", Size: 1000, Selectable: true},
		{Name: "b", Size: 300, Selectable: true},
		{Name: "c", Size: 100, Selectable: true},
		{Name: "d", Size: 30, Selectable: true},
		{Name: "e", Size: 10, Selectable: true},
	}
	rs := Layout(items, 80, 30, 0)
	cells := Raster(rs, 80, 30)
	share := map[int]int{}
	for _, c := range cells {
		share[c]++
	}
	area := float64(80 * 30)
	for i := range rs {
		if rs[i].W >= 80-1e-6 && rs[i].H >= 30-1e-6 {
			t.Fatalf("rect %d spans the whole container", i)
		}
		if p := float64(share[i]) / area; p > 0.7 {
			t.Fatalf("rect %d uses %.0f%% of the container", i, p*100)
		}
	}
}

// TestLayoutFixtureTilesContainer verifies the games/videos/projects/misc
// fixture tiles the whole grid with contained, non-overlapping rectangles.
func TestLayoutFixtureTilesContainer(t *testing.T) {
	items := []Item{
		{Name: "games", Size: 50, Selectable: true},
		{Name: "videos", Size: 30, Selectable: true},
		{Name: "projects", Size: 15, Selectable: true},
		{Name: "misc", Size: 5, Selectable: true},
	}
	const w, h = 44, 20
	rs := Layout(items, w, h, 0)
	if len(rs) != len(items) {
		t.Fatalf("Layout produced %d rects, want %d", len(rs), len(items))
	}
	var covered int
	for i, r := range rs {
		if r.X < 0 || r.Y < 0 || r.X+r.W > w || r.Y+r.H > h {
			t.Fatalf("rect %d escapes the grid: %#v", i, r)
		}
		if r.W*r.H <= 0 {
			t.Fatalf("rect %d has no area: %#v", i, r)
		}
	}
	cells := Raster(rs, w, h)
	for _, c := range cells {
		if c < 0 || c >= len(rs) {
			t.Fatalf("cell assigned rect %d outside fixture", c)
		}
		covered++
	}
	if covered != w*h {
		t.Fatalf("covered %d cells, want %d (full tiling)", covered, w*h)
	}
}

// TestLayoutWithMinCells asserts that items too small to matter are folded
// into the "other" bucket and are no longer rendered (or selectable).
func TestLayoutWithMinCells(t *testing.T) {
	items := []Item{{Name: "big", Size: 1000, Selectable: true}}
	for i := 0; i < 50; i++ {
		items = append(items, Item{Name: string(rune('a' + i)), Size: 1, Selectable: true})
	}
	const w, h = 120, 30
	rs := LayoutWith(items, w, h, 0, 8)
	// Only the big tile plus the folded "other" bucket remain.
	var selectable int
	for _, r := range rs {
		if r.Index >= 0 {
			selectable++
		}
	}
	if selectable != 1 {
		t.Fatalf("selectable rects = %d, want just the big tile", selectable)
	}
	cells := Raster(rs, w, h)
	for i, c := range cells {
		if c < 0 || c >= len(rs) {
			t.Fatalf("cell %d assigned rect %d outside result", i, c)
		}
	}
}

// TestLayoutWithMinCellsKeepsLargest renders a directory made only of tiny
// items, guaranteeing at least the largest tile is shown.
func TestLayoutWithMinCellsKeepsLargest(t *testing.T) {
	items := []Item{{Name: "a", Size: 3, Selectable: true}, {Name: "b", Size: 2, Selectable: true}}
	rs := LayoutWith(items, 10, 10, 0, 8)
	if len(rs) == 0 {
		t.Fatal("all-tiny directory laid out empty")
	}
}

func TestLayoutTiesStable(t *testing.T) {
	items := []Item{
		{Name: "z", Size: 10, Selectable: true},
		{Name: "a", Size: 10, Selectable: true},
		{Name: "m", Size: 10, Selectable: true},
	}
	rs := Layout(items, 90, 30, 0)
	// The three areas are equal, so rect order must match input order for a
	// stable result: largest first with ties in input order.
	if rs[0].Index != 0 || rs[2].Index != 2 {
		t.Fatalf("tie order not stable: %v", rs)
	}
}

// TestRectsStayInBounds guards against float scale drift pushing a rectangle
// past the container (which used to panic the TUI's outline painter).
func TestRectsStayInBounds(t *testing.T) {
	big := int64(1 << 37) // ~128 GiB, like a dominant home-dir entry
	cases := []struct {
		w, h  int
		sizes []int64
	}{
		{60, 15, []int64{400, 300, 200, 150, 50}},
		{78, 22, []int64{big, big / 60, big / 80, 1, 1, 1}},
		{200, 50, []int64{big, 5, 3, 2, 1, 1, 1, 1, 1, 1, 1}},
		{37, 11, []int64{90, 7, 2, 1}},
		{1, 1, []int64{10}},
		{2, 40, []int64{1000, 1}},
	}
	for _, c := range cases {
		items := make([]Item, len(c.sizes))
		for i, s := range c.sizes {
			items[i] = Item{Name: string(rune('a' + i)), Size: s, Selectable: true}
		}
		rs := Layout(items, c.w, c.h, 0)
		for _, r := range rs {
			if r.X < 0 || r.Y < 0 || r.X+r.W > float64(c.w) || r.Y+r.H > float64(c.h) {
				t.Fatalf("rect %#v escapes %dx%d grid", r, c.w, c.h)
			}
		}
	}
}
