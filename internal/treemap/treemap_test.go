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
