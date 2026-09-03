package tui

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
)

// palette is a deterministic set of background colours (256-color indices)
// cycled through by rectangle. Kept deliberately dark so white labels read
// well.
var palette = []string{
	"25", "27", "33", "39", "45", "63", "69", "75",
	"24", "31", "61", "67", "73", "81", "97", "103",
	"202", "208", "214", "220", "124", "130", "136", "142",
	"166", "172", "178", "184", "65", "71", "77", "83",
}

const (
	colOtherBG = "238"
	colOtherFG = "244"
	colSelBG   = "15" // white highlight for the selected rectangle
	colSelFG   = "0"
	colLabelFG = "255"
	colAccent  = "39"
	colDim     = "245"
	colErr     = "196"
)

var accentColor = lipgloss.Color(colAccent)

func accent() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(accentColor).Bold(true)
}

// moveSel moves the selection one step in the given direction (+x right, +y
// up). Among selectable rectangles strictly beyond the current one in that
// axis, it prefers the nearest (smallest perpendicular distance).
func (m *Model) moveSel(dx, dy int) {
	if m.sel < 0 || len(m.rects) == 0 {
		return
	}
	cur := m.rects[m.sel]
	cx := cur.X + cur.W/2
	cy := cur.Y + cur.H/2
	best := -1
	var bestD, bestO float64
	for i := range m.rects {
		r := &m.rects[i]
		if r.Index < 0 || i == m.sel {
			continue
		}
		rx := r.X + r.W/2
		ry := r.Y + r.H/2
		d := (rx-cx)*float64(dx) + (ry-cy)*float64(dy)
		if d <= 0 {
			continue
		}
		off := math.Abs(rx - cx)
		if dx != 0 {
			off = math.Abs(ry - cy)
		}
		if best < 0 || d < bestD || (d == bestD && off < bestO) {
			best, bestD, bestO = i, d, off
		}
	}
	if best >= 0 {
		m.sel = best
	}
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
