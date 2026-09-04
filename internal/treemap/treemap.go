// Package treemap lays out a set of weighted items as rectangles that tile a
// container, using the squarified treemap algorithm of Bruls, Huizing and van
// Wijk — the technique behind spacesniffer. Layout is deterministic: items are
// sorted by size descending (ties broken by input order) before laying out.
package treemap

import (
	"math"
	"sort"
)

// Item is one rectangle to lay out. Selectable marks entries the user may
// navigate to; the synthesized "other" bucket is not selectable.
type Item struct {
	Name       string
	Size       int64
	Selectable bool
}

// Rect is one laid-out rectangle in cell units (floats, since sizes rarely
// divide a cell grid evenly). Index is the position of the item in the input
// slice, or -1 for the synthesized "other" bucket.
type Rect struct {
	Index int
	X, Y  float64
	W, H  float64
}

// cell is a scaled item during layout: area is the item's size mapped into
// cell units so all areas sum to the container's area, and index is the item's
// position in the original input slice (-1 for the "other" bucket).
type cell struct {
	Item
	area  float64
	index int
}

// Layout places items in a w×h cell grid. At most maxRects items get their own
// rectangle (the largest first); any remainder is summed into a single
// non-selectable "other" rectangle. A maxRects of 0 disables the cap. Items
// with non-positive size are dropped. The result tiles the grid: the returned
// rectangles are non-overlapping, cover the container, and appear in the same
// order as laid out.
func Layout(items []Item, w, h int, maxRects int) []Rect {
	return LayoutWith(items, w, h, maxRects, 0)
}

// LayoutWith is Layout plus a minimum per-tile footprint: items that would end
// up smaller than minCells cells are considered insignificant and are folded
// into the "other" bucket instead of being rendered (and selectable). minCells
// of 0 renders everything. The largest item is always kept so a directory
// never lays out as empty.
func LayoutWith(items []Item, w, h, maxRects, minCells int) []Rect {
	if w <= 0 || h <= 0 || len(items) == 0 {
		return nil
	}
	work := make([]Item, 0, len(items))
	var total int64
	for _, it := range items {
		if it.Size <= 0 {
			continue
		}
		work = append(work, it)
		total += it.Size
	}
	if total <= 0 || len(work) == 0 {
		return nil
	}

	// Descending by size with input order preserved for ties.
	sort.SliceStable(work, func(i, j int) bool { return work[i].Size > work[j].Size })

	kept := work
	var leftover int64
	if maxRects > 0 && len(work) > maxRects {
		m := maxRects
		kept = work[:m]
		for _, it := range work[m:] {
			leftover += it.Size
		}
	}

	// Scale areas so they sum to the container area in cell units.
	cellArea := float64(w) * float64(h)
	scale := cellArea / float64(total)

	// Fold items too small to matter into the "other" bucket.
	var cells []cell
	idx := 0
	for _, it := range kept {
		area := float64(it.Size) * scale
		if minCells > 0 && area < float64(minCells) {
			leftover += it.Size
			continue
		}
		cells = append(cells, cell{Item: it, area: area, index: idx})
		idx++
	}
	// Keep at least the largest item so an all-tiny directory still renders.
	if len(cells) == 0 && len(kept) > 0 {
		big := kept[0]
		cells = append(cells, cell{Item: big, area: float64(big.Size) * scale, index: 0})
	}
	if leftover > 0 {
		cells = append(cells, cell{Item: Item{Name: "other", Size: leftover, Selectable: false}, area: float64(leftover) * scale, index: -1})
	}

	out := make([]Rect, 0, len(cells))
	squarify(cells, 0, 0, float64(w), float64(h), &out)
	// Floating-point scale arithmetic can nudge a rectangle a hair past the
	// container. Clamp so no rendered extent ever escapes the grid.
	for i := range out {
		if out[i].X < 0 {
			out[i].X = 0
		}
		if out[i].Y < 0 {
			out[i].Y = 0
		}
		if out[i].X+out[i].W > float64(w) {
			out[i].W = float64(w) - out[i].X
		}
		if out[i].Y+out[i].H > float64(h) {
			out[i].H = float64(h) - out[i].Y
		}
	}
	return out
}

// squarify lays out cells into strips inside the free box x,y,w,h, appending
// to out. cells must be sorted by decreasing area.
func squarify(cells []cell, x, y, w, h float64, out *[]Rect) {
	var row []cell
	for len(cells) > 0 {
		next := cells[0]
		// Extend the row while the next cell does not make the aspect ratio
		// worse; otherwise lay the current strip out and start a new one.
		if len(row) == 0 || worst(row, w, h) >= worst(append(row, next), w, h) {
			row = append(row, next)
			cells = cells[1:]
			continue
		}
		nx, ny, nw, nh := layoutRow(row, x, y, w, h, out)
		row = row[:0]
		x, y, w, h = nx, ny, nw, nh
	}
	layoutRow(row, x, y, w, h, out)
}

// worst reports the worst aspect ratio of a strip that hangs off the longest
// side L = max(w, h) of the free box. For a strip of total area S containing a
// smallest area sMin and largest area sMax, the paper derives
//
//	r = max(L²·sMin/S², S²/(L²·sMax)).
func worst(row []cell, w, h float64) float64 {
	var sum, max, min float64
	min = math.Inf(1)
	for _, c := range row {
		if c.area > max {
			max = c.area
		}
		if c.area < min {
			min = c.area
		}
		sum += c.area
	}
	if sum <= 0 || min == 0 {
		return math.Inf(1)
	}
	L := math.Max(w, h)
	sq := L * L
	return math.Max(sq*min/(sum*sum), sum*sum/(sq*max))
}

// layoutRow places a strip of row cells along one edge of the free box
// x,y,w,h: along the top for a landscape box (items span the width), along the
// left for a portrait box (items span the height). It appends the resulting
// rectangles to out and returns the remaining free box.
func layoutRow(row []cell, x, y, w, h float64, out *[]Rect) (float64, float64, float64, float64) {
	var sum float64
	for _, c := range row {
		sum += c.area
	}
	if sum <= 0 || w <= 0 || h <= 0 {
		// Degenerate remainder; nothing to place.
		return x, y, 0, 0
	}
	if w >= h {
		// Landscape: strip along the top, thickness measured down the height.
		thick := sum / w
		cx := x
		for i := range row {
			rw := row[i].area / thick
			*out = append(*out, Rect{Index: row[i].index, X: cx, Y: y, W: rw, H: thick})
			cx += rw
		}
		return x, y + thick, w, h - thick
	}
	// Portrait: strip along the left, thickness measured across the width.
	thick := sum / h
	cy := y
	for i := range row {
		rh := row[i].area / thick
		*out = append(*out, Rect{Index: row[i].index, X: x, Y: cy, W: thick, H: rh})
		cy += rh
	}
	return x + thick, y, w - thick, h
}

// Raster assigns every cell of a w×h grid to the rectangle that contains the
// cell's centre. The result is length w*h in row-major order, holding each
// rectangle's Rect index or -1 for cells belonging to no rectangle. This is the
// single source of truth for rendering and is cheap enough to run per frame.
func Raster(rects []Rect, w, h int) []int {
	if w <= 0 || h <= 0 {
		return nil
	}
	cells := make([]int, w*h)
	for i := range cells {
		cells[i] = -1
	}
	for i := range rects {
		r := &rects[i]
		x0 := int(math.Floor(r.X))
		x1 := int(math.Ceil(r.X + r.W))
		y0 := int(math.Floor(r.Y))
		y1 := int(math.Ceil(r.Y + r.H))
		if x0 < 0 {
			x0 = 0
		}
		if y0 < 0 {
			y0 = 0
		}
		if x1 > w {
			x1 = w
		}
		if y1 > h {
			y1 = h
		}
		for yy := y0; yy < y1; yy++ {
			for xx := x0; xx < x1; xx++ {
				if cells[yy*w+xx] != -1 {
					continue
				}
				cx := float64(xx) + 0.5
				cy := float64(yy) + 0.5
				if cx >= r.X && cx < r.X+r.W && cy >= r.Y && cy < r.Y+r.H {
					cells[yy*w+xx] = i
				}
			}
		}
	}
	return cells
}
