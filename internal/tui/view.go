package tui

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

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

	w, h := m.treemapSize()
	var idxBuf [][]int
	var buf [][]rune
	if w > 0 && h > 0 {
		idxBuf, buf = m.frame(w, h)
		if m.mode == modeConfirm {
			m.drawModal(idxBuf, buf, w, h)
		}
	}

	lines := []string{m.breadcrumbLine()}
	for _, row := range compose(m.rects, idxBuf, buf, w, h, m.sel) {
		lines = append(lines, row)
	}
	lines = append(lines, m.statusLine())
	return strings.Join(lines, "\n")
}

// frame renders the treemap into a per-cell index matrix (parallel to the
// raster) and a rune matrix holding border and label characters.
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
	if len(m.rects) == 0 || len(m.raster) != w*h {
		return idxBuf, buf
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idxBuf[y][x] = m.raster[y*w+x]
		}
	}

	bounds := make([]rBounds, len(m.rects))
	for i := range m.rects {
		bounds[i] = cellBounds(m.rects[i])
	}

	// Labels for every real entry.
	for i := range m.rects {
		ri := m.rects[i].Index
		if ri < 0 || m.current == nil || ri >= len(m.current.Children) {
			continue
		}
		node := m.current.Children[ri]
		b := bounds[i]
		drawLabel(buf, b, node.Name)
		if b.y1-b.y0 >= 5 {
			mid := b.y0 + (b.y1-b.y0)/2
			drawLabelAt(buf, b, mid+1, formatBytes(node.Size))
		}
	}
	// Border around the selected rectangle.
	if m.sel >= 0 && m.sel < len(m.rects) {
		drawBorder(buf, bounds[m.sel])
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
	if avail < 1 || row < b.y0 || row >= b.y1 {
		return
	}
	runes := []rune(text)
	if len(runes) > avail {
		runes = runes[:avail]
	}
	start := b.x0 + (b.x1-b.x0-len(runes))/2
	for i, r := range runes {
		buf[row][start+i] = r
	}
}

// drawBorder outlines a rectangle with box-drawing runes.
func drawBorder(buf [][]rune, b rBounds) {
	top, bottom, left, right := b.y0, b.y1-1, b.x0, b.x1-1
	if bottom < top || right < left {
		return
	}
	buf[top][left] = '┌'
	buf[top][right] = '┐'
	buf[bottom][left] = '└'
	buf[bottom][right] = '┘'
	for x := left + 1; x < right; x++ {
		buf[top][x] = '─'
		if bottom != top {
			buf[bottom][x] = '─'
		}
	}
	for y := top + 1; y < bottom; y++ {
		buf[y][left] = '│'
		if right != left {
			buf[y][right] = '│'
		}
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
		if n := len([]rune(l)); n > boxW {
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
	// Full-width scrim so neighboring labels do not bleed into the dialog rows.
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
		runes := []rune(line)
		if len(runes) > boxW-2 {
			runes = runes[:boxW-2]
		}
		row := oy + 1 + i
		start := ox + 1 + (boxW-2-len(runes))/2
		idx := idxModalFG
		if i == len(text)-1 && m.confirmErr {
			idx = idxModalErr
		}
		if i == 0 {
			idx = idxModalFG
		}
		for j, r := range runes {
			paint(start+j, row, r, idx)
		}
	}
}

// compose turns the frame matrices into styled terminal lines, batching
// consecutive cells that share a rectangle and character.
func compose(rects []treemap.Rect, idxBuf [][]int, buf [][]rune, w, h, sel int) []string {
	if len(idxBuf) != h {
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
			if ch != 0 {
				fill = strings.Repeat(string(ch), n)
			}
			sb.WriteString(styleOf(rects, idx, sel, ch).Render(fill))
			x += n
		}
		rows[y] = sb.String()
	}
	return rows
}

func styleOf(rects []treemap.Rect, idx, sel int, ch rune) lipgloss.Style {
	s := lipgloss.NewStyle()
	switch {
	case idx == idxModalBG:
		return s.Background(lipgloss.Color("232"))
	case idx == idxModalFG, idx == idxModalErr:
		s = s.Background(lipgloss.Color("232"))
		fg := colLabelFG
		if idx == idxModalErr {
			fg = colErr
		}
		return s.Foreground(lipgloss.Color(fg)).Bold(true)
	case idx == sel:
		s = s.Background(lipgloss.Color(colSelBG))
		if ch != 0 {
			s = s.Foreground(lipgloss.Color(colSelFG)).Bold(true)
		}
		return s
	case idx < 0:
		// Gap cell; no background.
		return s
	default:
		isOther := idx < len(rects) && rects[idx].Index < 0
		if isOther {
			s = s.Background(lipgloss.Color(colOtherBG))
			if ch != 0 {
				s = s.Foreground(lipgloss.Color(colOtherFG)).Bold(true)
			}
			return s
		}
		s = s.Background(lipgloss.Color(palette[idx%len(palette)]))
		if ch != 0 {
			s = s.Foreground(lipgloss.Color(colLabelFG)).Bold(true)
		}
		return s
	}
}

func (m *Model) breadcrumbLine() string {
	path := ""
	if m.current != nil {
		path = m.current.Path
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))
	return " " + dim.Render("▸ "+path)
}

func (m *Model) statusLine() string {
	right := "↑↓←→/hjkl move · ⏎ drill · esc up · del delete · r rescan · q quit"
	var left string
	if n := m.selectedNode(); n != nil {
		pct := ""
		if m.current != nil && m.current.Size > 0 {
			pct = fmt.Sprintf(" · %.1f%%", 100*float64(n.Size)/float64(m.current.Size))
		}
		left = fmt.Sprintf(" ▸ %s [%s]%s", n.Name, formatBytes(n.Size), pct)
	}
	fill := m.width - displayWidth(left) - displayWidth(right)
	if fill < 1 {
		fill = 1
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(colDim))
	return left + dim.Render(strings.Repeat("·", fill)) + dim.Render(right)
}

func displayWidth(s string) int {
	return utf8.RuneCountInString(s)
}

func (m *Model) splashView() string {
	lines := []string{
		"",
		accent().Render(m.spinner.View()) + "  Scanning " + lipgloss.NewStyle().Bold(true).Render(m.rootPath),
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render(m.progress.Current),
		lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render(
			fmt.Sprintf("visited %d · errors %d · %s", m.progress.Visited, m.progress.Errors, time.Since(m.start).Round(time.Second)),
		),
	}
	// Vertically center within the terminal.
	pad := (m.height - len(lines)) / 2
	if pad < 1 {
		pad = 1
	}
	out := make([]string, 0, pad+len(lines))
	for i := 0; i < pad; i++ {
		out = append(out, "")
	}
	return strings.Join(append(out, lines...), "\n")
}

// measuringView is shown briefly while a drilled directory's next level is
// measured on demand (usually under a second).
func (m *Model) measuringView() string {
	lines := []string{
		"",
		accent().Render(m.spinner.View()) + "  Measuring " + lipgloss.NewStyle().Bold(true).Render(m.pending.Path),
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render("this expands one level; sizes below are already known from the scan"),
	}
	pad := (m.height - len(lines)) / 2
	if pad < 1 {
		pad = 1
	}
	out := make([]string, 0, pad+len(lines))
	for i := 0; i < pad; i++ {
		out = append(out, "")
	}
	return strings.Join(append(out, lines...), "\n")
}

func (m *Model) errorView() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colErr)).
		Padding(1, 2).
		Width(50).
		Render("spacefinder: " + m.errMessage + "\n\nPress q to quit.")
	pad := (m.height - 6) / 2
	if pad < 1 {
		pad = 1
	}
	out := make([]string, 0, pad+1)
	for i := 0; i < pad; i++ {
		out = append(out, "")
	}
	return strings.Join(append(out, box), "\n")
}
