package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/metruzanca/spacefinder/internal/treemap"
)

// Special frame indices (besides treemap rect indices and -1 for gaps).
const (
	idxModalBG  = -2 // modal backdrop
	idxModalFG  = -3 // modal text
	idxModalErr = -4
)

type rBounds struct{ x0, y0, x1, y1 int }

func cellBounds(r treemap.Rect) rBounds {
	return rBounds{
		x0: int(math.Floor(r.X)),
		y0: int(math.Floor(r.Y)),
		x1: int(math.Ceil(r.X + r.W)),
		y1: int(math.Ceil(r.Y + r.H)),
	}
}

func (m *Model) View() string {
	switch m.mode {
	case modeSplash:
		return m.splashView()
	case modeMeasuring:
		return m.measuringView()
	case modeError:
		return m.errorView()
	}

	if m.width < minWidth || m.height < minHeight {
		return strings.Join(centerRows(m.height, []string{
			lipgloss.NewStyle().Bold(true).Render("terminal too small"),
			"spacefinder needs at least 20x10 cells",
		}), "\n")
	}

	w, h := m.treemapSize()
	var idxBuf [][]int
	var buf [][]rune
	if w > 0 && h > 0 {
		idxBuf, buf = m.frame(w, h)
		if m.mode == modeConfirm {
			m.drawModal(idxBuf, buf, w, h)
		}
	}

	rows := []string{m.breadcrumbLine()}
	switch {
	case m.mode == modeConfirm:
		rows = append(rows, compose(m.tiles, idxBuf, buf, w, h, m.sel)...)
	case len(m.rects) == 0:
		rows = append(rows, centerRows(h, []string{
			lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render("empty directory"),
			lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render("press esc to go up"),
		})...)
	default:
		rows = append(rows, compose(m.tiles, idxBuf, buf, w, h, m.sel)...)
	}
	if n := m.freeRows(); n > 0 {
		rows = append(rows, m.freeGutterRows(n, m.width)...)
	}
	rows = append(rows, m.statusLine())
	return strings.Join(rows, "\n")
}

// freeGutterRows renders the free-space gutter: a muted band, labeled with the
// real free byte count, below the content treemap.
func (m *Model) freeGutterRows(n, width int) []string {
	style := freeStyle()
	label := fmt.Sprintf(" free · %s ", formatBytes(m.freeBytes))
	rows := make([]string, n)
	pad := width - lipgloss.Width(label)
	if pad < 1 {
		pad = 1
	}
	left := pad / 2
	for i := 0; i < n; i++ {
		if i == 0 {
			rows[i] = style.Render(strings.Repeat(" ", left) + label + strings.Repeat(" ", pad-left))
		} else {
			rows[i] = style.Render(strings.Repeat(" ", width))
		}
	}
	return rows
}

const (
	minWidth  = 20
	minHeight = 10
)

// frame renders the treemap into a per-cell index matrix (parallel to the
// raster) and a rune matrix holding label characters. There are no borders:
// the selection is indicated by a brighter fill and a marker on its label.
func (m *Model) frame(w, h int) ([][]int, [][]rune) {
	idxBuf := make([][]int, h)
	buf := make([][]rune, h)
	for y := 0; y < h; y++ {
		idxBuf[y] = make([]int, w)
		buf[y] = make([]rune, w)
		for x := 0; x < w; x++ {
			idxBuf[y][x] = -1
		}
	}
	if len(m.raster) != w*h || len(m.tiles) != len(m.rects) {
		return idxBuf, buf
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idxBuf[y][x] = m.raster[y*w+x]
		}
	}

	bounds := make([]rBounds, len(m.rects))
	for i := range m.rects {
		b := cellBounds(m.rects[i])
		// Clamp floats that drifted past the grid; label painting must never
		// index outside the frame matrices.
		if b.x1 > w {
			b.x1 = w
		}
		if b.y1 > h {
			b.y1 = h
		}
		if b.x0 < 0 {
			b.x0 = 0
		}
		if b.y0 < 0 {
			b.y0 = 0
		}
		bounds[i] = b
	}

	// Labels for every real entry, centred inside the block; the selected
	// block's name carries a marker so the selection reads without borders.
	for i := range m.rects {
		ri := m.rects[i].Index
		if ri < 0 || m.current == nil || ri >= len(m.current.Children) {
			continue
		}
		node := m.current.Children[ri]
		b := bounds[i]
		name := node.Name
		if i == m.sel {
			name = "▸ " + name
		}
		drawLabel(buf, b, name)
		if b.y1-b.y0 >= 5 {
			mid := b.y0 + (b.y1-b.y0)/2
			drawLabelAt(buf, b, mid+1, formatBytes(node.Size))
		}
	}
	return idxBuf, buf
}

// drawLabel places text centered on the middle row of the rectangle if it
// fits.
func drawLabel(buf [][]rune, b rBounds, text string) {
	if b.y1-b.y0 < 3 {
		return
	}
	mid := b.y0 + (b.y1-b.y0)/2
	drawLabelAt(buf, b, mid, text)
}

func drawLabelAt(buf [][]rune, b rBounds, row int, text string) {
	avail := b.x1 - b.x0 - 2
	if avail < 1 || row <= b.y0 || row >= b.y1-1 {
		return
	}
	runes := []rune(text)
	if len(runes) > avail {
		runes = runes[:avail]
	}
	start := b.x0 + 1 + (avail-len(runes))/2
	for i, r := range runes {
		buf[row][start+i] = r
	}
}

// drawModal paints the delete-confirmation dialog over the frame.
func (m *Model) drawModal(idxBuf [][]int, buf [][]rune, w, h int) {
	var text []string
	text = append(text, "DELETE")
	text = append(text, m.confirmNode.Name)
	text = append(text, "Type this exact name to confirm, or esc to cancel.")
	if m.input.Value() == "" {
		text = append(text, m.input.Placeholder)
	} else {
		text = append(text, m.input.Value())
	}
	if m.confirmErr {
		text = append(text, "Name does not match.")
	}

	boxW := 0
	for _, l := range text {
		if n := lipgloss.Width(l); n > boxW {
			boxW = n
		}
	}
	boxW += 4 // padding + border
	if boxW > w {
		boxW = w
	}
	boxH := len(text) + 3 // border + text + one blank row of breathing room
	if boxH > h {
		boxH = h
	}
	ox := (w - boxW) / 2
	oy := (h - boxH) / 2
	if ox < 0 {
		ox = 0
	}
	if oy < 0 {
		oy = 0
	}

	paint := func(x, y int, r rune, idx int) {
		if x >= 0 && x < w && y >= 0 && y < h {
			idxBuf[y][x] = idx
			buf[y][x] = r
		}
	}
	// Full-width scrim so neighbouring labels do not bleed into the dialog rows.
	for yy := oy; yy < oy+boxH && yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			paint(xx, yy, ' ', idxModalBG)
		}
	}
	// Border.
	for yy := 0; yy < boxH; yy++ {
		for xx := 0; xx < boxW; xx++ {
			var ch rune
			switch {
			case (yy == 0 || yy == boxH-1) && (xx == 0 || xx == boxW-1):
				switch {
				case yy == 0 && xx == 0:
					ch = '┌'
				case yy == 0:
					ch = '┐'
				case xx == 0:
					ch = '└'
				default:
					ch = '┘'
				}
			case yy == 0 || yy == boxH-1:
				ch = '─'
			case xx == 0 || xx == boxW-1:
				ch = '│'
			default:
				continue
			}
			paint(ox+xx, oy+yy, ch, idxModalFG)
		}
	}
	// Backdrop fill.
	for yy := 1; yy < boxH-1; yy++ {
		for xx := 1; xx < boxW-1; xx++ {
			paint(ox+xx, oy+yy, ' ', idxModalBG)
		}
	}
	// Text rows.
	for i, line := range text {
		if i >= boxH-2 {
			break
		}
		runes := []rune(truncate(line, boxW-2))
		row := oy + 1 + i
		start := ox + 1 + (boxW-2-len(runes))/2
		idx := idxModalFG
		if i == len(text)-1 && m.confirmErr {
			idx = idxModalErr
		}
		for j, r := range runes {
			paint(start+j, row, r, idx)
		}
	}
}

// compose turns the frame matrices into styled terminal lines, batching
// consecutive cells that share a rectangle and character.
func compose(tiles []tileStyle, idxBuf [][]int, buf [][]rune, w, h, sel int) []string {
	if len(idxBuf) != h || len(buf) != h {
		return nil
	}
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		var sb strings.Builder
		x := 0
		for x < w {
			idx := idxBuf[y][x]
			ch := buf[y][x]
			n := 1
			for x+n < w && idxBuf[y][x+n] == idx && buf[y][x+n] == ch {
				n++
			}
			fill := " "
			switch {
			case ch != 0:
				fill = strings.Repeat(string(ch), n)
			case idx >= 0:
				// Solid blocks keep tiles visible even when the terminal
				// skips background colours.
				fill = strings.Repeat("█", n)
			}
			sb.WriteString(styleOf(tiles, idx, sel, ch).Render(fill))
			x += n
		}
		rows[y] = sb.String()
	}
	return rows
}

func styleOf(tiles []tileStyle, idx, sel int, ch rune) lipgloss.Style {
	switch {
	case idx == idxModalBG:
		return lipgloss.NewStyle().Background(lipgloss.Color("232"))
	case idx == idxModalFG, idx == idxModalErr:
		s := lipgloss.NewStyle().Background(lipgloss.Color("232"))
		if idx == idxModalErr {
			return s.Foreground(lipgloss.Color(colErr)).Bold(true)
		}
		return s.Foreground(lipgloss.Color("255")).Bold(true)
	case idx == sel && idx >= 0 && idx < len(tiles):
		ts := tiles[idx]
		if ch != 0 {
			return ts.selGlyph
		}
		return ts.selFill
	case idx >= 0 && idx < len(tiles):
		ts := tiles[idx]
		if ch != 0 {
			return ts.glyph
		}
		return ts.fill
	default:
		// Gap cell; no background, plain.
		return lipgloss.NewStyle()
	}
}

func (m *Model) breadcrumbLine() string {
	if m.current == nil {
		return ""
	}
	max := m.width - 1
	if max < 1 {
		max = 1
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))
	segs := []string{"⌂ " + m.rootPath}
	rest := m.crumbs
	if len(rest) > 0 && rest[0] == m.tree {
		rest = rest[1:] // the root is already shown by its absolute path
	}
	for _, c := range rest {
		segs = append(segs, c.Name)
	}
	if m.current != m.tree {
		segs = append(segs, m.current.Name)
	}

	join := func(ss []string, folded bool) string {
		var b strings.Builder
		if folded {
			b.WriteString(dim.Render("… / "))
		}
		for i, s := range ss {
			if i > 0 {
				b.WriteString(dim.Render(" / "))
			}
			st := dim
			if i == len(ss)-1 {
				st = accent()
			}
			b.WriteString(st.Render(s))
		}
		return b.String()
	}
	s := join(segs, false)
	for lipgloss.Width(s) > max && len(segs) > 1 {
		segs = segs[1:]
		s = join(segs, true)
	}
	return s
}

func (m *Model) statusLine() string {
	max := m.width
	if max < 1 {
		max = 1
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))
	hint := "click=sel · 2x=drill/open · esc=up · del=delete · q=quit"

	var left string
	if n := m.selectedNode(); n != nil {
		left = fmt.Sprintf(" ▸ %s [%s]", n.Name, formatBytes(n.Size))
		if m.current != nil && m.current.Size > 0 {
			left += fmt.Sprintf(" · %.1f%%", 100*float64(n.Size)/float64(m.current.Size))
		}
		if m.current != nil {
			left += fmt.Sprintf(" · %d children", len(m.current.Children))
			if m.hidden > 0 {
				left += fmt.Sprintf(" · %d hidden", m.hidden)
			}
		}
	}
	left = truncate(left, max)
	if left == "" {
		return dim.Render(hint)
	}

	const gap = 1
	fill := max - lipgloss.Width(left) - lipgloss.Width(hint) - 2*gap
	if fill < 1 {
		avail := max - lipgloss.Width(left) - gap
		if avail < 1 {
			avail = 1
		}
		hint = truncate(hint, avail)
		fill = max - lipgloss.Width(left) - lipgloss.Width(hint) - 2*gap
		if fill < 0 {
			fill = 0
		}
	}
	out := left
	if fill > 0 {
		out += " " + dim.Render(strings.Repeat("·", fill)) + " "
	} else {
		out += " "
	}
	out += dim.Render(hint)
	return out
}

// cellAt maps terminal (0-based) coordinates to the rectangle under the
// cursor, or -1 when the point is outside the treemap. The treemap body starts
// on the line below the breadcrumb, so the y offset is adjusted by one.
func (m *Model) cellAt(x, y int) int {
	w, h := m.treemapSize()
	if len(m.raster) != w*h || w <= 0 || h <= 0 {
		return -1
	}
	ty := y - 1
	if ty < 0 || ty >= h || x < 0 || x >= w {
		return -1
	}
	return m.raster[ty*w+x]
}

// centerRows pads lines into areaH rows, vertically centered.
func centerRows(areaH int, lines []string) []string {
	n := areaH
	if n < len(lines) {
		n = len(lines)
	}
	pad := (n - len(lines)) / 2
	if pad < 1 {
		pad = 1
	}
	out := make([]string, 0, pad+len(lines))
	for i := 0; i < pad; i++ {
		out = append(out, "")
	}
	return append(out, lines...)
}

func (m *Model) splashView() string {
	path := truncate(m.progress.Current, max(20, m.width-10))
	lines := []string{
		"",
		accent().Render(m.spinner.View()) + "  Scanning " + lipgloss.NewStyle().Bold(true).Render(truncate(m.rootPath, m.width-14)),
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render(path),
		lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render(
			fmt.Sprintf("visited %d · errors %d · %s", m.progress.Visited, m.progress.Errors, time.Since(m.start).Round(time.Second)),
		),
	}
	return strings.Join(centerRows(m.height, lines), "\n")
}

// measuringView is shown briefly while a drilled directory's next level is
// measured on demand (usually under a second).
func (m *Model) measuringView() string {
	lines := []string{
		"",
		accent().Render(m.spinner.View()) + "  Measuring " + lipgloss.NewStyle().Bold(true).Render(truncate(m.pending.Path, m.width-12)),
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render("this expands one level; sizes below are already known from the scan"),
	}
	return strings.Join(centerRows(m.height, lines), "\n")
}

func (m *Model) errorView() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colErr)).
		Padding(1, 2).
		Width(50).
		Render("spacefinder: " + m.errMessage + "\n\nPress q to quit.")
	return strings.Join(centerRows(m.height, []string{box}), "\n")
}
