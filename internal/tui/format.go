package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/metruzanca/spacefinder/internal/treemap"
)

const (
	colDim     = "245" // dim text (hints, secondary info)
	colAccent  = "39"  // accent (selection outline, highlights)
	colErr     = "196" // error red
	colOtherBG = "#3a3f4b"
	colOtherFG = "#9aa0aa"
	colFreeBG  = "#2b3138" // muted backdrop for the free-space gutter

	// bottom keybind bar: black background, white keys, neutral gray labels
	colHelpBG    = "0"
	colHelpKey   = "255"
	colHelpLabel = "250"
)

var accentColor = lipgloss.Color(colAccent)

func accent() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(accentColor).Bold(true)
}

// freeStyle is the muted backdrop for the free-space gutter row.
func freeStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(colFreeBG)).
		Foreground(lipgloss.Color(colDim)).
		Bold(true)
}

// tileStyle is the set of precomputed styles for one rectangle: solid-block
// interior fills (foreground + background so they stay visible even when a
// terminal disables colours) and label glyph styles. The selected block uses
// a brighter fill, distinguished further by a marker on its label.
type tileStyle struct {
	fill     lipgloss.Style // solid block interior, normal
	glyph    lipgloss.Style // label glyphs, normal
	selFill  lipgloss.Style // solid block interior, selected (brighter)
	selGlyph lipgloss.Style // label glyphs, selected
}

func styleFor(name string) tileStyle {
	normal, bright := nameColor(name)
	normalC := lipgloss.Color(normal)
	brightC := lipgloss.Color(bright)
	return tileStyle{
		fill:     lipgloss.NewStyle().Background(normalC).Foreground(normalC),
		glyph:    lipgloss.NewStyle().Background(normalC).Foreground(labelFG(normal)).Bold(true),
		selFill:  lipgloss.NewStyle().Background(brightC).Foreground(brightC),
		selGlyph: lipgloss.NewStyle().Background(brightC).Foreground(labelFG(bright)).Bold(true),
	}
}

// labelFG picks the more readable of white or near-black for text drawn on a
// given hex fill, by comparing WCAG contrast ratios. Bold labels stay readable
// on every tile, light or dark (yellow, lime, cyan... included).
func labelFG(hex string) lipgloss.Color {
	L := luminance(hex)
	white := 1.05 / (L + 0.05)
	dark := (L + 0.05) / 0.0585 // relative luminance of #14181c
	if white >= dark {
		return lipgloss.Color("#ffffff")
	}
	return lipgloss.Color("#14181c")
}

// luminance returns the WCAG relative luminance of an sRGB hex colour.
func luminance(hex string) float64 {
	r, g, b := parseHex(hex)
	lin := func(c byte) float64 {
		v := float64(c) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

func parseHex(hex string) (byte, byte, byte) {
	var r, g, b uint8
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

var otherStyle = func() tileStyle {
	bg := lipgloss.Color(colOtherBG)
	fg := lipgloss.Color(colOtherFG)
	return tileStyle{
		fill:     lipgloss.NewStyle().Background(bg).Foreground(bg),
		glyph:    lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(true),
		selFill:  lipgloss.NewStyle().Background(bg).Foreground(bg),
		selGlyph: lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(true),
	}
}()

// nameColor maps an item name to a stable pair of 24-bit background colours
// (normal and a brighter selected twin) via an FNV hash over the name, so the
// same directory keeps the same hue at every depth.
func nameColor(name string) (string, string) {
	h := uint32(2166136261)
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 16777619
	}
	hue := float64(h % 360)
	normal := hsvHex(hue, 0.55, 0.72)
	bright := hsvHex(hue, 0.55, 0.95)
	return normal, bright
}

func hsvHex(h, s, v float64) string {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60.0, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	ri := int(math.Round((r + m) * 255))
	gi := int(math.Round((g + m) * 255))
	bi := int(math.Round((b + m) * 255))
	return fmt.Sprintf("#%02x%02x%02x", ri, gi, bi)
}

// truncate shortens s to at most max terminal cells, appending an ellipsis
// when cut. It never splits a rune.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	var b strings.Builder
	w := 0
	max -= 1 // room for the ellipsis
	for _, r := range s {
		rw := runeWidth(r)
		if w+rw > max {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

func runeWidth(r rune) int {
	if r < 128 {
		return 1
	}
	if r >= 0x2190 && r <= 0x21ff { // arrows/ambiguous symbols are width 1
		return 1
	}
	return 1
}

// moveSel moves the selection one step in the given direction (+x right, +y
// up). It navigates by block edges: candidates must lie strictly beyond the
// current block in that axis, and the chosen one is the nearest such block
// (smallest gap) best aligned with the current block's centre along the other
// axis. That gives natural neighbour-to-neighbour movement across the tiles.
func (m *Model) moveSel(dx, dy int) {
	if m.sel < 0 || len(m.rects) == 0 {
		return
	}
	cur := &m.rects[m.sel]
	best := -1
	var bestGap, bestAlign float64
	for i := range m.rects {
		r := &m.rects[i]
		if r.Index < 0 || i == m.sel {
			continue
		}
		gap, align, ok := directionScore(cur, r, dx, dy)
		if !ok {
			continue
		}
		if best < 0 || gap < bestGap || (gap == bestGap && align < bestAlign) {
			best, bestGap, bestAlign = i, gap, align
		}
	}
	if best >= 0 {
		m.sel = best
	}
}

// directionScore reports how far block r lies beyond cur in the direction
// (dx,dy): gap is the separation along the movement axis, align is how well
// the candidate lines up with cur along the other axis. ok is false when r is
// not strictly beyond cur in the requested direction.
func directionScore(cur, r *treemap.Rect, dx, dy int) (gap, align float64, ok bool) {
	cx := cur.X + cur.W/2
	cy := cur.Y + cur.H/2
	rx := r.X + r.W/2
	ry := r.Y + r.H/2
	switch {
	case dx > 0:
		if r.X < cur.X+cur.W-1e-6 {
			return 0, 0, false
		}
		return r.X - (cur.X + cur.W), math.Abs(ry - cy), true
	case dx < 0:
		if r.X+r.W > cur.X+1e-6 {
			return 0, 0, false
		}
		return cur.X - (r.X + r.W), math.Abs(ry - cy), true
	case dy > 0:
		if r.Y < cur.Y+cur.H-1e-6 {
			return 0, 0, false
		}
		return r.Y - (cur.Y + cur.H), math.Abs(rx - cx), true
	case dy < 0:
		if r.Y+r.H > cur.Y+1e-6 {
			return 0, 0, false
		}
		return cur.Y - (r.Y + r.H), math.Abs(rx - cx), true
	}
	return 0, 0, false
}

// formatBytes renders a byte count in human units (B/KB/MB/GB/TB).
func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(n)
	for _, u := range units {
		v /= 1024
		if v < 1024 {
			return fmt.Sprintf("%.1f %s", v, u)
		}
	}
	return fmt.Sprintf("%.1f PB", v/1024)
}
