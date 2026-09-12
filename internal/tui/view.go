package tui

import (
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/metruzanca/spacefinder/internal/logging"
	"github.com/metruzanca/spacefinder/internal/treemap"
)

// Special frame indices (besides treemap rect indices and -1 for gaps).
const (
	idxModalBG  = -2 // modal backdrop
	idxModalFG  = -3 // modal text
	idxModalErr = -4
	idxModalDim = -5 // dim description text in the help table

	// Pagination arrow gutter strips (selectable, but not real directory tiles).
	idxPageNext = -6 // arrow pointing to the next page
	idxPagePrev = -7 // arrow pointing to the previous page
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

// View renders the current model. A panic while painting is recovered here so
// a rendering bug can never kill the program: it shows the error view instead
// and records the stack in the debug log.
func (m *Model) View() (out string) {
	defer func() {
		if r := recover(); r != nil {
			logging.Errorf("panic in View: %v\n%s", r, debug.Stack())
			m.mode = modeError
			m.errMessage = fmt.Sprintf("internal error: %v", r)
			out = m.errorView()
		}
	}()
	return m.view()
}

func (m *Model) view() string {
	switch m.mode {
	case modeScanning:
		// Before any child completes, show the centered scanning splash; once
		// blocks exist the view falls through to the live treemap below.
		if len(m.rects) == 0 {
			return m.scanningView()
		}
	case modeMeasuring:
		return m.measuringView()
	case modeError:
		return m.errorView()
	case modePicker:
		return m.pickerView()
	}
	// The help modal overlays the screen it came from, so a help opened from
	// the picker must render the picker (tiles, info, keybind bar) behind it.
	if m.mode == modeHelp && m.helpFrom == modePicker {
		return m.pickerView()
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
		switch {
		case m.mode == modeConfirm:
			m.drawModal(idxBuf, buf, w, h)
		case m.mode == modeHelp:
			m.drawHelp(idxBuf, buf, w, h)
		case m.mode == modeErrors:
			m.drawErrors(idxBuf, buf, w, h)
		}
	}

	rows := []string{m.breadcrumbLine(), m.infoLine()}
	switch {
	case m.mode == modeConfirm, m.mode == modeHelp, m.mode == modeErrors:
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
	rows = append(rows, m.helpLine())
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
		if ri < 0 || m.current == nil || ri >= len(m.pageIdx) {
			continue
		}
		node := m.current.Children[m.pageIdx[ri]]
		b := bounds[i]
		name := node.Name
		if i == m.sel && b.x1-b.x0-2 >= 4 {
			name = "▸ " + name
		}
		drawLabel(buf, b, name)
		if b.y1-b.y0 >= 5 {
			mid := b.y0 + (b.y1-b.y0)/2
			drawLabelAt(buf, b, mid+1, m.tileMetric(node))
		}
	}

	// Pagination arrows: a single glyph at the vertical centre of each strip.
	if m.prevArrow >= 0 && m.prevArrow < len(m.rects) {
		drawArrow(buf, bounds[m.prevArrow], '←')
	}
	if m.nextArrow >= 0 && m.nextArrow < len(m.rects) {
		drawArrow(buf, bounds[m.nextArrow], '→')
	}
	return idxBuf, buf
}

// drawArrow places a pagination arrow glyph at the vertical centre of its
// gutter strip.
func drawArrow(buf [][]rune, b rBounds, ch rune) {
	if b.y1-b.y0 < 3 {
		return
	}
	row := b.y0 + (b.y1-b.y0)/2
	if row < 0 || row >= len(buf) {
		return
	}
	x := b.x0 + (b.x1-b.x0)/2
	if x < 0 || x >= len(buf[row]) {
		return
	}
	buf[row][x] = ch
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
	cut := false
	if len(runes) > avail {
		runes = runes[:avail]
		cut = true
	}
	if cut && len(runes) > 0 {
		runes[len(runes)-1] = '…'
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
	errLine := -1
	if m.confirmErr {
		text = append(text, "Name does not match.")
		errLine = len(text) - 1
	}
	paintModal(idxBuf, buf, w, h, text, errLine, true)
}

// drawHelp paints the keybind/mouse reference as a borderless, scrim-backed
// table over the current frame.
func (m *Model) drawHelp(idxBuf [][]int, buf [][]rune, w, h int) {
	paintTable(idxBuf, buf, w, h, m.helpRows())
}

// helpTableRow is one row of the help table: a key cell and a description
// cell. header rows are section titles; footer rows are a single centered
// hint line.
type helpTableRow struct {
	left, right string
	header      bool
	footer      bool
}

// helpRows returns the single, global keybind reference shown by the help
// modal wherever it is opened from: the full set of controls plus a mouse
// primer, laid out as a two-column table.
func (m *Model) helpRows() []helpTableRow {
	kb := []helpTableRow{
		{left: "↑↓←→ / hjkl", right: "move the selection"},
		{left: "pgup / pgdn", right: "previous / next page of tiles"},
		{left: "enter", right: "drill into a folder / flip a page arrow"},
		{left: "esc", right: "go up a level"},
		{left: "r", right: "rescan the current root"},
		{left: "e", right: "show unreadable paths from the scan"},
		{left: "del", right: "delete the selection (type its name to confirm)"},
		{left: "q", right: "quit"},
	}

	rows := []helpTableRow{{left: "KEYBINDS", header: true}}
	rows = append(rows, kb...)
	rows = append(rows, helpTableRow{}) // breathing room
	rows = append(rows, helpTableRow{left: "MOUSE", header: true})
	rows = append(rows,
		helpTableRow{left: "click", right: "select a block or a page arrow"},
		helpTableRow{left: "double-click", right: "drill into a folder / open a file / flip a page arrow"},
		helpTableRow{left: "right-click", right: "go up a level"},
		helpTableRow{left: "wheel", right: "move the selection"},
	)
	return append(rows,
		helpTableRow{},
		helpTableRow{right: "esc, enter, or ? to close", footer: true},
	)
}

// drawErrors paints the scan-errors modal as a bordered, left-aligned list of
// wrapped "path: reason" lines. Each error is kept whole (never split mid-way)
// and scrolled by errScroll; long lines wrap to the box width instead of being
// truncated.
func (m *Model) drawErrors(idxBuf [][]int, buf [][]rune, w, h int) {
	body := m.scanErrors
	if len(body) == 0 {
		paintModal(idxBuf, buf, w, h, []string{
			"SCAN ERRORS",
			"no unreadable paths",
			"press esc to close",
		}, -1, true)
		return
	}
	contentW := w - 4 // box borders + padding
	if contentW < 12 {
		contentW = w - 2
	}
	if contentW < 1 {
		contentW = 1
	}
	wrapped := make([][]string, len(body))
	for i, e := range body {
		wrapped[i] = wrapText(e.Path+": "+e.Err.Error(), contentW)
	}
	// Rows inside the box: borders(2) + header(1) + up to two footer hints, so
	// the box fits h with the footer still visible.
	bodyFit := h - 5
	if bodyFit < 1 {
		bodyFit = 1
	}
	start := m.errScroll
	if start < 0 {
		start = 0
	}
	if start >= len(body) {
		start = len(body) - 1
	}
	// Show whole errors only: advance until the next error's wrapped lines
	// would overflow the box.
	end := start
	used := 0
	for end < len(body) {
		if used+len(wrapped[end]) > bodyFit {
			break
		}
		used += len(wrapped[end])
		end++
	}
	if end == start {
		end = start + 1 // a single error taller than the box still shows
	}

	text := []string{"UNREADABLE"}
	for _, l := range wrapped[start:end] {
		text = append(text, l...)
	}
	if shown := end - start; shown < m.scanErrTotal {
		text = append(text, fmt.Sprintf("showing %d of %d · scroll for more", shown, m.scanErrTotal))
	}
	text = append(text, "esc close")
	paintModal(idxBuf, buf, w, h, text, -1, false)
}

// paintTable draws a borderless two-column table on a scrim backdrop: the key
// cell is left-aligned in its own column, the description sits in the column
// beside it, section headers and the footer span the width.
func paintTable(idxBuf [][]int, buf [][]rune, w, h int, rows []helpTableRow) {
	leftW, rightW := 0, 0
	for _, r := range rows {
		if r.header || r.footer {
			continue
		}
		if n := lipgloss.Width(r.left); n > leftW {
			leftW = n
		}
		if n := lipgloss.Width(r.right); n > rightW {
			rightW = n
		}
	}
	const gap = 4
	const pad = 2
	bodyW := leftW + gap + rightW
	for _, r := range rows {
		var wl int
		switch {
		case r.footer:
			wl = lipgloss.Width(r.right)
		case r.header:
			wl = lipgloss.Width(r.left)
		}
		if wl > bodyW {
			bodyW = wl
		}
	}
	boxW := bodyW + 2*pad
	if boxW > w {
		boxW = w
	}
	boxH := len(rows) + 2
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
	for yy := 0; yy < boxH && oy+yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			paint(xx, oy+yy, ' ', idxModalBG)
		}
	}
	writeText := func(x, y int, txt string, idx int) {
		for j, r := range []rune(txt) {
			paint(x+j, y, r, idx)
		}
	}
	for i, r := range rows {
		y := oy + 1 + i
		if y < 0 || y >= h {
			break
		}
		switch {
		case r.footer:
			txt := truncate(r.right, boxW-2*pad)
			writeText(ox+pad+(boxW-2*pad-lipgloss.Width(txt))/2, y, txt, idxModalDim)
		case r.header:
			txt := truncate(r.left, boxW-2*pad)
			writeText(ox+pad, y, txt, idxModalFG)
		default:
			left := truncate(r.left, leftW)
			right := truncate(r.right, boxW-pad-leftW-gap-pad)
			writeText(ox+pad, y, left, idxModalFG)
			writeText(ox+pad+leftW+gap, y, right, idxModalDim)
		}
	}
}

// paintModal draws a message box over the frame: full-width scrim so
// neighbouring labels do not bleed in, a bordered box, and the given text rows.
// errLine marks one row to render in the error color (-1 for none). center
// aligns each row horizontally; when false rows are left-aligned (for lists).
func paintModal(idxBuf [][]int, buf [][]rune, w, h int, text []string, errLine int, center bool) {
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
		start := ox + 1
		if center {
			start = ox + 1 + (boxW-2-len(runes))/2
		}
		idx := idxModalFG
		if i == errLine {
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
			default:
				// Background runs stay width-preserving so edge strips on
				// sparse rows keep their column position.
				fill = strings.Repeat(" ", n)
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
	case idx == idxModalDim:
		return lipgloss.NewStyle().
			Background(lipgloss.Color("232")).
			Foreground(lipgloss.Color(colHelpLabel))
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

// effectiveMode is the screen the current view belongs to: when the help modal
// is open it is the mode it overlays, so the info and keybind rows keep the
// flavour of the underlying view.
func (m *Model) effectiveMode() mode {
	if m.mode == modeHelp && m.helpFrom != 0 {
		return m.helpFrom
	}
	if m.mode == modeErrors {
		return modeBrowse // overlays the browser, not a distinct screen
	}
	return m.mode
}

// infoLine is a single row just below the breadcrumbs describing the current
// selection (browse: the selected entry and its share; picker: the selected
// target). It never carries hints — the keybinds own the bottom line.
func (m *Model) infoLine() string {
	max := m.width
	if max < 1 {
		max = 1
	}
	var left, right string
	switch m.effectiveMode() {
	case modePicker:
		if n := m.selectedPickerNode(); n != nil {
			left = fmt.Sprintf(" ▸ %s", n.Path)
			if n.Size > 0 {
				if m.pickerUsed[n] {
					left += fmt.Sprintf(" [~%s rough]", formatBytes(n.Size))
				} else {
					left += fmt.Sprintf(" [%s free]", formatBytes(n.Size))
				}
			}
			if d := m.pickerDesc[n]; d != "" {
				left += " · " + d
			}
		}
	case modeScanning:
		if n := m.selectedNode(); n != nil {
			left = fmt.Sprintf(" ▸ %s [%s]", n.Name, formatBytes(n.Size))
		}
		right = fmt.Sprintf("scanning · %d children", len(m.scanChildren()))
		if total := m.scanTotal(); total > 0 {
			right += fmt.Sprintf(" · %s so far", formatBytes(total))
		}
		right += fmt.Sprintf(" · visited %d", m.progress.Visited)
		if m.progress.Errors > 0 {
			right += fmt.Sprintf(" · %d errors", m.progress.Errors)
		}
		right += fmt.Sprintf(" · %s", time.Since(m.start).Round(time.Second))
	default:
		if n := m.selectedNode(); n != nil {
			left = fmt.Sprintf(" ▸ %s [%s]", n.Name, formatBytes(n.Size))
			if n.Approx {
				left += " ~ excluded (system, not scanned)"
			}
			if m.current != nil && m.current.Size > 0 {
				left += fmt.Sprintf(" · %.1f%%", 100*float64(n.Size)/float64(m.current.Size))
			}
		} else if dir := m.selectedArrow(); dir != 0 {
			label := "previous page"
			if dir > 0 {
				label = "next page"
			}
			left = fmt.Sprintf(" %s %s", arrowRune(dir), label)
		}
		if m.current != nil {
			right = fmt.Sprintf("%d children", len(m.current.Children))
			if m.hidden > 0 {
				right += fmt.Sprintf(" · %d hidden", m.hidden)
			}
			if m.pageCount > 1 {
				right += fmt.Sprintf(" · page %d/%d", m.page+1, m.pageCount)
			}
		}
		if m.current == m.tree && m.scanErrTotal > 0 {
			err := lipgloss.NewStyle().
				Foreground(lipgloss.Color(colErr)).
				Bold(true).
				Render(fmt.Sprintf("%d unreadable", m.scanErrTotal))
			if right == "" {
				right = err
			} else {
				right += " · " + err
			}
		}
	}
	// The right group (children/hidden) reflects the drilled directory, not the
	// selection, so it is pinned to the right edge while the selection group
	// occupies the left.
	if right == "" {
		return truncate(left, max)
	}
	rightW := lipgloss.Width(right)
	if max <= rightW {
		return truncate(right, max)
	}
	left = truncate(left, max-rightW)
	pad := max - lipgloss.Width(left) - rightW
	if pad < 0 {
		pad = 0
	}
	return left + strings.Repeat(" ", pad) + right
}

// keybind is one keybord/mouse shortcut shown in the bottom help bar.
type keybind struct {
	key  string // the actual key(s), rendered white
	desc string // what they do, rendered gray
}

// helpLineBindings returns the shortcuts shown for the current mode.
func (m *Model) helpLineBindings() []keybind {
	if m.effectiveMode() == modePicker {
		return []keybind{
			{"↑↓←→", "select"},
			{"enter", "scan"},
			{"?", "help"},
			{"q", "quit"},
		}
	}
	if m.effectiveMode() == modeScanning {
		return []keybind{
			{"q", "quit"},
		}
	}
	kb := []keybind{
		{"click", "select"},
		{"esc", "up"},
		{"del", "delete"},
		{"?", "help"},
		{"q", "quit"},
	}
	if m.pageCount > 1 {
		kb = append([]keybind{{"pgdn", "next page"}}, kb...)
	}
	if m.scanErrTotal > 0 {
		kb = append([]keybind{{"e", fmt.Sprintf("errors(%d)", m.scanErrTotal)}}, kb...)
	}
	return kb
}

// helpLine is the bottom row of the screen: keybinds on a black bar, with the
// keys themselves in white and their descriptions in neutral gray. On narrow
// terminals trailing bindings are dropped (with an ellipsis) so the bar always
// fits.
func (m *Model) helpLine() string {
	max := m.width
	if max < 1 {
		max = 1
	}
	key := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colHelpKey)).
		Background(lipgloss.Color(colHelpBG)).
		Bold(true)
	lbl := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colHelpLabel)).
		Background(lipgloss.Color(colHelpBG))
	fill := lipgloss.NewStyle().Background(lipgloss.Color(colHelpBG))

	var segs []struct {
		s string
		w int
	}
	for i, h := range m.helpLineBindings() {
		var b strings.Builder
		if i > 0 {
			b.WriteString(lbl.Render(" · "))
		}
		b.WriteString(key.Render(h.key))
		b.WriteString(lbl.Render("  " + h.desc))
		s := b.String()
		segs = append(segs, struct {
			s string
			w int
		}{s, lipgloss.Width(s)})
	}
	// Plain text (no key) announcing mouse support, styled gray like the rest.
	note := lbl.Render(" · mouse supported")
	segs = append(segs, struct {
		s string
		w int
	}{note, lipgloss.Width(note)})

	var b strings.Builder
	var used int
	var overflow bool
	for _, sg := range segs {
		if used+sg.w > max {
			overflow = true
			break
		}
		used += sg.w
		b.WriteString(sg.s)
	}
	if overflow && used+lipgloss.Width("…") <= max {
		b.WriteString(lbl.Render("…"))
	}
	row := b.String()
	pad := max - lipgloss.Width(row)
	left := pad / 2
	return fill.Render(strings.Repeat(" ", left)) + row + fill.Render(strings.Repeat(" ", pad-left))
}

// cellAt maps terminal (0-based) coordinates to the rectangle under the
// cursor, or -1 when the point is outside the treemap. The treemap body starts
// on the line below the breadcrumbs and info row, so the y offset is two.
func (m *Model) cellAt(x, y int) int {
	w, h := m.treemapSize()
	if len(m.raster) != w*h || w <= 0 || h <= 0 {
		return -1
	}
	ty := y - 2
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

// scanningView is the placeholder shown while the scan runs but before the
// first child completes: a centered spinner with the root path and progress
// counts. Once blocks exist, view() renders the live growing treemap instead.
func (m *Model) scanningView() string {
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

// pickerView renders the filesystem picker as a treemap: one equal-area tile
// per scan target, labeled with its free space, navigated and selected like
// the browser. Replaces the treemap until a path is chosen.
func (m *Model) pickerView() string {
	if m.width < minWidth || m.height < minHeight {
		return strings.Join(centerRows(m.height, []string{
			lipgloss.NewStyle().Bold(true).Render("terminal too small"),
			"spacefinder needs at least 20x10 cells",
		}), "\n")
	}

	w, h := m.treemapSize()
	m.buildPickerLayout()
	var idxBuf [][]int
	var buf [][]rune
	if w > 0 && h > 0 {
		idxBuf, buf = m.frame(w, h)
		if m.mode == modeHelp {
			m.drawHelp(idxBuf, buf, w, h)
		}
	}

	rows := []string{m.pickerHeader(), m.infoLine()}
	switch {
	case len(m.rects) == 0:
		rows = append(rows, centerRows(h, []string{
			lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render("no scan targets found"),
			lipgloss.NewStyle().Foreground(lipgloss.Color(colDim)).Render("press q to quit"),
		})...)
	default:
		rows = append(rows, compose(m.tiles, idxBuf, buf, w, h, m.sel)...)
	}
	rows = append(rows, m.helpLine())
	return strings.Join(rows, "\n")
}

// pickerHeader is the title row above the picker tiles.
func (m *Model) pickerHeader() string {
	return "⌂ " + accent().Render("pick a location to scan")
}
