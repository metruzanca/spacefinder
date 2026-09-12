package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"
	"github.com/mattn/go-isatty"

	"github.com/metruzanca/spacefinder/internal/logging"
	"github.com/metruzanca/spacefinder/internal/scan"
	"github.com/metruzanca/spacefinder/internal/treemap"
)

const maxRects = 0 // no cap: every non-empty entry is reachable across pages

// pageGutterCols is the width in columns of each pagination arrow gutter strip.
const pageGutterCols = 3

// Floor tile dimensions. Every rendered tile is at least minTileRows tall and
// minTileCols wide, so its name label is legible (drawLabel needs ~3 rows and a
// few columns). Sizes are guaranteed by paginating the level: children are
// chunked into pages, each laid out over the full grid, so nothing ever falls
// below the floor. A smaller tile's size line is omitted (name only).
const minTileRows = 3
const minTileCols = 6

// minTileArea is the smallest area a tile may occupy at the shared scale, so
// tiny items render as chunky blocks rather than hairlines.
const minTileArea = minTileRows * minTileCols

// minLabelRows/minLabelCols are the smallest tile dimensions that can still
// carry a (truncated) name label. drawLabel needs ≥3 rows and ≥3 columns for
// at least one character; squarify's natural aspect for a dense tail is ~4
// wide, so a strict 6-column floor would split every tail item onto its own
// page. The area floor keeps tiles chunky; this guards only against hairlines.
const minLabelRows = 3
const minLabelCols = 3

// minTileCells is the area equivalent of the floor, used by the single-screen
// filesystem picker (LayoutWith folds too-small tiles into "other").
const minTileCells = minTileArea

type mode int

const (
	modeScanning mode = iota
	modeMeasuring
	modeBrowse
	modeConfirm
	modeError
	modePicker
	modeHelp
	modeErrors
)

type scanDoneMsg struct {
	gen     int
	scanner *scan.Scanner
	root    *scan.Node
	err     error
}

// internalErrMsg is delivered when an internal background task panics. It
// surfaces as the error view instead of killing the program, and the stack
// trace is recorded in the debug log.
type internalErrMsg struct {
	op  string
	err error
}

type expandDoneMsg struct {
	node *scan.Node
	err  error
}

type openDoneMsg struct {
	path string
	err  error
}

// cwdSizeMsg carries a rough top-level du estimate for the picker's
// current-directory tile, so its size is real (not the filesystem's free
// bytes) even before anything is scanned.
type cwdSizeMsg struct {
	size int64
}

type scanProgressMsg scan.Progress

// scanFrameMsg ticks the growth animation forward at a fixed rate. Each frame
// eases every completed block's size toward its real value with a harmonica
// spring, so the treemap visibly builds out as the walk explores the folder.
type scanFrameMsg time.Time

// Spring character for the block-growth animation: near-critical damping, so
// tiles grow to size with a smooth settle and no overshoot bounce.
const (
	scanFPS        = 30
	scanFrequency  = 6.0
	scanDamping    = 0.8
	scanSettleEps  = 0.05
	scanInitialPos = 1 // new blocks start as a sliver and grow in
)

// scanSpring is the animation state of one completed root child: its eased
// layout size (pos) is driven toward the child's real layout size (target).
type scanSpring struct {
	pos, vel float64
	target   float64
}

// Model is the bubbletea model for spacefinder.
type Model struct {
	rootPath string

	mode       mode
	errMessage string

	start time.Time

	// scan state
	scanGen    int
	cancelScan context.CancelFunc
	progressCh chan scan.Progress
	progress   scan.Progress
	spinner    spinner.Model
	scanner    *scan.Scanner
	tree       *scan.Node
	pending    *scan.Node // directory being expanded on demand (modeMeasuring)

	// scan animation: the treemap builds out block by block as root children
	// complete. scanRoot is a synthetic root whose Children grow with each
	// progress event; scanSprings eases each child's layout size from a sliver
	// to its real value. scanTicking tracks whether a frame ticker is armed.
	scanRoot    *scan.Node
	scanSprings []scanSpring
	scanSpring  harmonica.Spring
	scanTicking bool

	// browse state
	current *scan.Node
	crumbs  []*scan.Node // ancestors from root down to the parent of current
	width   int
	height  int
	rects   []treemap.Rect
	raster  []int
	tiles   []tileStyle
	sel     int
	hidden  int // children with a zero block size, dropped from the layout

	// paging: children are chunked into full-screen pages so every non-empty
	// entry gets a legible (floored) tile. ordered holds the layout-order
	// (sorted descending) child indices with a positive size; pageStart are the
	// boundaries of each page into ordered; pageIdx is the current page's
	// ordered slice; pageOf maps a global child index to its page number;
	// page/pageCount are the current and total page count.
	page      int
	pageCount int
	ordered   []int
	pageStart []int
	pageIdx   []int
	pageOf    []int
	pageScale float64   // bytes→cells scale shared by every page of the level
	pageMin   []float64 // per-page minimum tile area (raised for partial tails)

	// pagination arrow gutter rects: positions in m.rects of the next/prev
	// arrow strips, or -1 when absent. Arrows are selectable elements that
	// flip pages on Enter or mouse double-click.
	nextArrow int
	prevArrow int

	// free-space gutter (only shown at the scan root)
	freeBytes int64

	// mouse state
	lastClick     time.Time
	lastClickTile int

	// delete-confirm state
	confirmNode *scan.Node
	input       textinput.Model
	confirmErr  bool

	// scan errors surfaced after the measure (modeErrors shows the full list)
	scanErrors   []scan.ScanError
	scanErrTotal int
	errScroll    int

	// filesystem picker (modePicker, shown when spacefinder runs without a path)
	pickerNodes []*scan.Node          // one equal-area tile per scan target
	pickerDesc  map[*scan.Node]string // status-line detail (device, type) per tile
	pickerUsed  map[*scan.Node]bool   // nodes whose Size is an estimate, not free bytes
	pickerCwd   *scan.Node            // the current-directory tile (rough du size)
	pickerRoot  *scan.Node            // fake root so the treemap renderer is reused

	// help modal (modeHelp): which mode to return to on close
	helpFrom mode
}

func newModel(rootPath string) *Model {
	abs := ""
	if rootPath != "" {
		if a, err := filepath.Abs(rootPath); err == nil {
			abs = a
		}
	}
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(accent()))
	input := textinput.New()
	input.Placeholder = "type the exact name"
	input.CharLimit = 256
	input.Width = 36
	m := &Model{
		rootPath:   filepath.Clean(abs),
		mode:       modeScanning,
		start:      time.Now(),
		spinner:    sp,
		input:      input,
		sel:        -1,
		width:      80,
		height:     24,
		progressCh: make(chan scan.Progress, 128),
		scanSpring: harmonica.NewSpring(harmonica.FPS(scanFPS), scanFrequency, scanDamping),
	}
	if rootPath == "" {
		m.rootPath = ""
		m.mode = modePicker
		m.setupPicker()
		logging.Debugf("tui starting in picker mode (%d targets)", len(m.pickerNodes))
	} else {
		logging.Debugf("tui starting in scan mode (root=%q)", m.rootPath)
	}
	return m
}

// pickerOption is one scan target offered by the picker.
type pickerOption struct {
	label    string // short name drawn on the tile
	path     string // scan root on selection
	desc     string // status-line detail (device, type, kind)
	free     int64  // free bytes, shown until a real scan happens
	estimate bool   // Size should instead carry a rough du estimate
}

// pickerOptions collects the scan targets in preference order: home first (the
// most common target, and the pre-selected tile), then the filesystem root,
// the directory spacefinder was launched from, then every detected drive or
// partition.
func pickerOptions() []pickerOption {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	root := fsRoot(home, cwd)

	var opts []pickerOption
	if home != "" {
		opts = append(opts, pickerOption{label: "home", path: home, desc: "home directory", free: freeOn(home)})
	}
	opts = append(opts, pickerOption{label: "root", path: root, desc: "filesystem root", free: freeOn(root)})
	if cwd != "" && cwd != home && filepath.Clean(cwd) != root {
		opts = append(opts, pickerOption{label: "cwd", path: cwd, desc: "current directory", free: freeOn(cwd), estimate: true})
	}
	for _, mo := range scan.Mounts() {
		if mo.Path == home || mo.Path == root || mo.Path == cwd {
			continue // already offered above
		}
		label := filepath.Base(mo.Path)
		if label == "." || label == string(os.PathSeparator) {
			label = mo.Path
		}
		desc := mo.Type
		if mo.Device != "" && mo.Device != mo.Path {
			desc = mo.Device + " (" + mo.Type + ")"
		}
		opts = append(opts, pickerOption{label: label, path: mo.Path, desc: desc, free: mo.Free})
	}
	return opts
}

// fsRoot is the "root" entry of the picker: / on unix, the volume containing
// home (then cwd) on windows, where a lone backslash is not a real root.
func fsRoot(home, cwd string) string {
	if home != "" {
		if v := filepath.VolumeName(home); v != "" {
			return v + string(os.PathSeparator)
		}
	}
	if cwd != "" {
		if v := filepath.VolumeName(cwd); v != "" {
			return v + string(os.PathSeparator)
		}
	}
	return string(os.PathSeparator)
}

// setupPicker turns the picker options into a fake scan tree so the existing
// treemap renderer, hit-testing, and edge navigation apply verbatim. Each tile
// is one option; Size carries its free byte count (or a du estimate for the
// current directory) for the label line.
func (m *Model) setupPicker() {
	opts := pickerOptions()
	root := &scan.Node{Name: "picker", Path: "", IsDir: true, Children: make([]*scan.Node, 0, len(opts))}
	m.pickerDesc = make(map[*scan.Node]string, len(opts))
	m.pickerUsed = make(map[*scan.Node]bool, len(opts))
	for _, o := range opts {
		n := &scan.Node{Name: o.label, Path: o.path, IsDir: true, Size: o.free}
		m.pickerDesc[n] = o.desc
		if o.estimate {
			m.pickerUsed[n] = true
			m.pickerCwd = n
		}
		root.Children = append(root.Children, n)
	}
	m.pickerNodes = root.Children
	m.pickerRoot = root
	m.tree = root
	m.current = root
}

// tileAreaSize is the equal, arbitrary area every picker tile gets so all
// options render at a comparable size regardless of free space.
const tileAreaSize = 1 << 30

// buildPickerLayout lays every picker option out as an equal-area treemap tile
// for the current terminal size.
func (m *Model) buildPickerLayout() {
	children := m.pickerNodes
	if len(children) == 0 {
		m.rects = nil
		m.raster = nil
		m.tiles = nil
		m.nextArrow = -1
		m.prevArrow = -1
		return
	}
	w, h := m.treemapSize()
	items := make([]treemap.Item, len(children))
	for i, c := range children {
		items[i] = treemap.Item{Name: c.Name, Size: tileAreaSize, Selectable: true}
	}
	m.rects = treemap.LayoutWith(items, w, h, maxRects, minTileCells)
	m.raster = treemap.Raster(m.rects, w, h)
	// The picker is a single page; LayoutWith preserves input order, so each
	// rect Index equals its position in pickerNodes.
	m.page, m.pageCount = 0, 1
	m.nextArrow, m.prevArrow = -1, -1
	m.pageIdx = make([]int, len(children))
	for i := range m.pageIdx {
		m.pageIdx[i] = i
	}
	m.ordered, m.pageStart, m.pageOf = nil, nil, nil
	m.tiles = colorTiles(m.rects, m.raster, w, h)
	if m.sel >= len(m.rects) {
		m.sel = -1
	}
	if m.sel < 0 {
		m.sel = firstSelectable(m.rects)
	}
}

// selectedPickerNode returns the scan target behind the current tile, or nil
// when the selection is out of range.
func (m *Model) selectedPickerNode() *scan.Node {
	return m.selectedNode()
}

// tileMetric is the secondary label drawn under a tile's name: free bytes for
// filesystems, a "~" rough du estimate for the current directory. Excluded
// subtrees (Windows system dirs) also carry "~" to mark their size as an
// approximation, not a measured total.
func (m *Model) tileMetric(node *scan.Node) string {
	s := formatBytes(node.Size)
	if node.Approx {
		return "~" + s
	}
	if node.Size > 0 && m.pickerUsed[node] {
		return "~" + s
	}
	return s
}

// Run starts the TUI in the alt-screen and blocks until it exits.
func Run(rootPath string) error {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		logging.Errorf("refusing to run: stdout is not a terminal")
		return errors.New("spacefinder is an interactive terminal app; run it in a terminal")
	}
	logging.Debugf("tui starting (root=%q, terminal detected)", rootPath)
	p := tea.NewProgram(newModel(rootPath), tea.WithAltScreen(), tea.WithMouseCellMotion())
	// Mirror stray stdout writes (bubbletea's own recovered-panic traces,
	// debug.PrintStack output) into the debug log so a panic outside our
	// recover handlers is still captured. The renderer writes through the fd
	// it captured at NewProgram, so normal rendering is not mirrored.
	if lw := logging.LogWriter(); lw != nil {
		defer teeStdout(lw)()
	}
	_, err := p.Run()
	if err != nil {
		logging.Errorf("tui program error: %v", err)
	} else {
		logging.Debugf("tui program exited cleanly")
	}
	return err
}

// teeStdout redirects writes made through the os.Stdout package variable into
// w as well as the real stdout, while leaving fd 1 itself untouched. Returns
// a func that restores os.Stdout and stops the drain when Run exits.
func teeStdout(w io.Writer) func() {
	r, pw, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	old := os.Stdout
	os.Stdout = pw
	var once sync.Once
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer r.Close()
		buf := make([]byte, 32*1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				old.Write(buf[:n])
				w.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	return func() {
		once.Do(func() {
			os.Stdout = old
			pw.Close() // EOF on the reader; stops the drain
			wg.Wait()
		})
	}
}

// runSafe executes a background command, converting any panic into an
// internalErrMsg so a bug in a scan or file task can never kill the program.
// The stack trace is recorded in the debug log; without it, bubbletea would
// swallow the panic and report a generic "program experienced a panic".
func runSafe(op string, fn func() tea.Msg) (out tea.Msg) {
	defer func() {
		if r := recover(); r != nil {
			logging.Errorf("panic in %s: %v\n%s", op, r, debug.Stack())
			out = internalErrMsg{op: op, err: fmt.Errorf("panic in %s: %v", op, r)}
		}
	}()
	return fn()
}

func (m *Model) Init() tea.Cmd {
	if m.mode == modePicker {
		return m.cwdSizeCmd()
	}
	return tea.Batch(m.spinner.Tick, m.startScan(0), m.progressCmd())
}

// cwdSizeCmd runs the rough du estimate for the current-directory tile in the
// background (the picker never blocks on it); the estimate replaces the free
// byte count when it arrives.
func (m *Model) cwdSizeCmd() tea.Cmd {
	if m.pickerCwd == nil {
		return nil
	}
	path := m.pickerCwd.Path
	return func() tea.Msg {
		return runSafe("du "+path, func() tea.Msg {
			return cwdSizeMsg{size: duSize(path)}
		})
	}
}

// startScan runs a fresh measure pass of the root and reports its result. The
// generation allows stale results (from a cancelled rescan) to be ignored.
func (m *Model) startScan(gen int) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelScan = cancel
	ch := m.progressCh
	return func() tea.Msg {
		return runSafe("measure "+m.rootPath, func() tea.Msg {
			scanner, root, err := scan.Measure(ctx, m.rootPath, ch)
			return scanDoneMsg{gen: gen, scanner: scanner, root: root, err: err}
		})
	}
}

// progressCmd forwards one throttled progress event per invocations; it returns
// nil (a no-op) once the progress channel closes, ending the redeliver loop.
func (m *Model) progressCmd() tea.Cmd {
	ch := m.progressCh
	return func() tea.Msg {
		return runSafe("progress", func() tea.Msg {
			p, ok := <-ch
			if !ok {
				return nil
			}
			return scanProgressMsg(p)
		})
	}
}

// expandCmd measures the next level of a directory (usually instant, since the
// totals were recorded by the initial pass) before it is opened.
func (m *Model) expandCmd(node *scan.Node) tea.Cmd {
	sc := m.scanner
	return func() tea.Msg {
		return runSafe("expand "+node.Path, func() tea.Msg {
			if sc == nil {
				return expandDoneMsg{node: node}
			}
			return expandDoneMsg{node: node, err: sc.Expand(context.Background(), node)}
		})
	}
}

// Update drives the model from incoming messages. A panic while processing a
// message is recovered here — instead of killing the program it shows the
// error view and records the stack in the debug log. bubbletea would otherwise
// swallow the panic and report a generic "program experienced a panic".
func (m *Model) Update(msg tea.Msg) (tm tea.Model, cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			logging.Errorf("panic in Update: %v\n%s", r, debug.Stack())
			m.mode = modeError
			m.errMessage = fmt.Sprintf("internal error: %v", r)
			tm = m
		}
	}()
	return m.update(msg)
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Ignore zero reports (e.g. a pty with no size); the model starts with
		// sane defaults so a 0x0 query never blanks the UI.
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		if m.mode == modeBrowse && m.current != nil {
			m.buildLayout()
		}
		if m.mode == modeScanning && len(m.scanSprings) > 0 {
			m.buildScanLayout()
		}
		return m, nil

	case tea.MouseMsg:
		switch m.mode {
		case modeHelp:
			return m, nil // the modal swallows mouse input
		case modePicker:
			return m.updatePickerMouse(msg)
		case modeErrors:
			return m.updateErrorsMouse(msg)
		}
		return m.updateMouse(msg)

	case tea.KeyMsg:
		return m.updateKeys(msg)

	case spinner.TickMsg:
		if m.mode == modeScanning || m.mode == modeMeasuring {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case scanProgressMsg:
		m.progress = scan.Progress(msg)
		var anim tea.Cmd
		if m.mode == modeScanning && len(msg.Completed) > 0 {
			m.addScanChildren(msg.Completed)
			anim = m.armScanTicker()
		}
		// Re-arm the consumer so the drain loop keeps running; otherwise the
		// scan's progress channel fills and the measure goroutine blocks.
		return m, tea.Batch(anim, m.progressCmd())

	case scanFrameMsg:
		if m.mode != modeScanning {
			m.scanTicking = false
			return m, nil
		}
		moving := m.stepScanSprings()
		m.buildScanLayout()
		if moving {
			return m, m.scanFrameCmd()
		}
		m.scanTicking = false
		return m, nil

	case expandDoneMsg:
		if msg.node != m.pending {
			return m, nil // stale or cancelled
		}
		m.pending = nil
		if msg.err != nil {
			logging.Errorf("expand failed (dir=%q): %v", msg.node.Path, msg.err)
			m.mode = modeError
			m.errMessage = msg.err.Error()
			return m, nil
		}
		logging.Debugf("expanded dir=%q children=%d size=%d", msg.node.Path, len(msg.node.Children), msg.node.Size)
		m.crumbs = append(m.crumbs, m.current)
		m.current = msg.node
		m.mode = modeBrowse
		m.refreshScanErrors() // the expand may have hit fresh unreadable paths
		m.buildLayout()
		return m, nil

	case cwdSizeMsg:
		if m.pickerCwd != nil && msg.size > 0 {
			logging.Debugf("cwd du estimate arrived: size=%d", msg.size)
			m.pickerCwd.Size = msg.size
			m.pickerUsed[m.pickerCwd] = true
			m.buildPickerLayout()
		}
		return m, nil

	case openDoneMsg:
		if msg.err != nil {
			logging.Errorf("open failed (path=%q): %v", msg.path, msg.err)
			m.mode = modeError
			m.errMessage = fmt.Sprintf("open %s: %v", msg.path, msg.err)
		} else {
			logging.Debugf("launched path=%q", msg.path)
		}
		return m, nil

	case internalErrMsg:
		logging.Errorf("internal error surfaced to UI: op=%q err=%v", msg.op, msg.err)
		m.mode = modeError
		m.errMessage = msg.err.Error()
		return m, nil

	case scanDoneMsg:
		if msg.gen != m.scanGen {
			logging.Debugf("ignoring stale scan result (gen=%d, want %d)", msg.gen, m.scanGen)
			return m, nil // result of a cancelled rescan
		}
		if msg.err != nil {
			logging.Errorf("measure failed (root=%q): %v", m.rootPath, msg.err)
			m.mode = modeError
			m.errMessage = msg.err.Error()
			return m, nil
		}
		if msg.scanner != nil {
			logging.Debugf("measure complete: root=%q size=%d children=%d errTotal=%d took=%s",
				m.rootPath, msg.root.Size, len(msg.root.Children), msg.scanner.TotalErrors(), time.Since(m.start))
		} else {
			logging.Debugf("measure complete: root=%q size=%d children=%d took=%s",
				m.rootPath, msg.root.Size, len(msg.root.Children), time.Since(m.start))
		}
		m.scanner = msg.scanner
		m.tree = msg.root
		m.current = msg.root
		m.crumbs = nil
		m.freeBytes = freeOn(m.rootPath)
		logging.Debugf("free bytes on scan root: %d", m.freeBytes)
		m.refreshScanErrors()
		// Drop the animation scaffolding; the browse layout takes over.
		m.scanRoot = nil
		m.scanSprings = nil
		m.scanTicking = false
		m.mode = modeBrowse
		m.buildLayout()
		return m, nil
	}
	return m, nil
}

// freeOn reports free bytes on the scan root's filesystem, or 0 when
// unavailable (e.g. windows).
func freeOn(path string) int64 {
	if n, err := scan.Free(path); err == nil {
		return n
	}
	return 0
}

// refreshScanErrors pulls the unreadable-path sample off the scanner so the
// browse view can report, and on 'e' show, what the scan could not read.
func (m *Model) refreshScanErrors() {
	if m.scanner == nil {
		m.scanErrors = nil
		m.scanErrTotal = 0
		return
	}
	m.scanErrors = m.scanner.Errors()
	m.scanErrTotal = m.scanner.TotalErrors()
	if m.scanErrTotal > 0 {
		logging.Errorf("scan encountered %d unreadable path(s)", m.scanErrTotal)
		for _, se := range m.scanErrors {
			logging.Errorf("  unreadable path=%q err=%v", se.Path, se.Err)
		}
	}
}

func (m *Model) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeScanning, modeError:
		if isQuit(msg) {
			return m.quit()
		}
	case modePicker:
		return m.updatePicker(msg)
	case modeHelp:
		return m.updateHelp(msg)
	case modeMeasuring:
		switch {
		case isQuit(msg):
			return m.quit()
		case msg.Type == tea.KeyEsc:
			m.pending = nil
			m.mode = modeBrowse
			return m, nil
		}
	case modeConfirm:
		return m.updateConfirm(msg)
	case modeErrors:
		return m.updateErrors(msg)
	case modeBrowse:
		return m.updateBrowse(msg)
	}
	return m, nil
}

// updatePicker handles the blocky filesystem picker. The tiles are navigated
// with the same arrow/vim edge movement as the treemap browser; enter (or a
// double-click) starts the scan of the highlighted target, q/esc/ctrl+c quits.
func (m *Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEnter:
		return m.choosePicker()
	case msg.String() == "?":
		return m.openHelp(), nil
	case isQuit(msg): // esc intentionally does not quit from the picker
		return m.quit()
	case msg.Type == tea.KeyUp, msg.String() == "k":
		m.moveSel(0, -1)
	case msg.Type == tea.KeyDown, msg.String() == "j":
		m.moveSel(0, 1)
	case msg.Type == tea.KeyLeft, msg.String() == "h":
		m.moveSel(-1, 0)
	case msg.Type == tea.KeyRight, msg.String() == "l":
		m.moveSel(1, 0)
	}
	return m, nil
}

// updatePickerMouse selects a picker tile on click; a same-tile double-click
// within the debounce window starts the scan. The wheel moves the selection.
func (m *Model) updatePickerMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		tile := m.cellAt(msg.X, msg.Y)
		if tile < 0 || tile >= len(m.rects) || m.rects[tile].Index < 0 {
			return m, nil
		}
		now := time.Now()
		double := tile == m.lastClickTile && now.Sub(m.lastClick) < 350*time.Millisecond
		m.lastClickTile = tile
		m.lastClick = now
		m.sel = tile
		if double {
			return m.choosePicker()
		}
	case msg.Button == tea.MouseButtonWheelUp:
		m.moveSel(0, -1)
	case msg.Button == tea.MouseButtonWheelDown:
		m.moveSel(0, 1)
	}
	return m, nil
}

// choosePicker starts the scan of the selected picker tile.
func (m *Model) choosePicker() (tea.Model, tea.Cmd) {
	n := m.selectedPickerNode()
	if n == nil {
		return m, nil
	}
	return m.beginScan(n.Path)
}

// openHelp overlays the keybind/mouse help modal on the current screen. It may
// only be entered from the treemap browser or the picker.
func (m *Model) openHelp() *Model {
	if m.mode != modeBrowse && m.mode != modePicker {
		return m
	}
	m.helpFrom = m.mode
	m.mode = modeHelp
	return m
}

// updateHelp handles keys while the help modal is open: esc, enter, or another
// "?" closes it back to the mode it came from; q (or ctrl+c) quits.
func (m *Model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc, msg.Type == tea.KeyEnter, msg.String() == "?":
		m.mode = m.helpFrom
		m.helpFrom = 0
		return m, nil
	case isQuit(msg):
		return m.quit()
	}
	return m, nil
}

// updateErrors handles keys while the scan-errors modal is open: j/k (or
// arrows) scroll the list, esc/e/enter close it back to the browser, q (or
// ctrl+c) quits.
func (m *Model) updateErrors(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isQuit(msg):
		return m.quit()
	case msg.Type == tea.KeyEsc, msg.String() == "e", msg.Type == tea.KeyEnter:
		m.mode = modeBrowse
		return m, nil
	case msg.Type == tea.KeyUp, msg.String() == "k":
		m.scrollErrors(-1)
	case msg.Type == tea.KeyDown, msg.String() == "j":
		m.scrollErrors(1)
	}
	return m, nil
}

// updateErrorsMouse scrolls the errors modal with the wheel; a click anywhere
// on the scrim is ignored (it would only land on the dimmed treemap behind).
func (m *Model) updateErrorsMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.scrollErrors(-1)
	case tea.MouseButtonWheelDown:
		m.scrollErrors(1)
	}
	return m, nil
}

// scrollErrors moves the errors-modal viewport by delta lines, clamped to the
// stored sample.
func (m *Model) scrollErrors(delta int) {
	m.errScroll += delta
	if m.errScroll < 0 || len(m.scanErrors) == 0 {
		m.errScroll = 0
		return
	}
	if last := len(m.scanErrors) - 1; m.errScroll > last {
		m.errScroll = last
	}
}

// beginScan points the model at path and starts measuring it, transitioning
// through the splash screen. Used by the picker after a selection.
func (m *Model) beginScan(path string) (tea.Model, tea.Cmd) {
	m.rootPath = path
	m.scanGen = 0
	m.progress = scan.Progress{}
	m.progressCh = make(chan scan.Progress, 128)
	m.scanner = nil
	m.tree = nil
	m.current = nil
	m.crumbs = nil
	m.pending = nil
	m.freeBytes = 0
	m.scanErrors = nil
	m.scanErrTotal = 0
	m.errScroll = 0
	m.sel = -1 // a fresh layout starts from the first tile, not the old picker position
	m.page = 0
	m.scanRoot = nil
	m.scanSprings = nil
	m.scanTicking = false
	m.mode = modeScanning
	m.start = time.Now()
	logging.Debugf("tui root path: %s", path)
	return m, tea.Batch(m.spinner.Tick, m.startScan(0), m.progressCmd())
}

func (m *Model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isQuit(msg):
		return m.quit()
	case msg.Type == tea.KeyDelete || msg.Type == tea.KeyBackspace:
		m.openConfirm()
		return m, nil
	case msg.String() == "?":
		return m.openHelp(), nil
	case msg.Type == tea.KeyEnter:
		if dir := m.selectedArrow(); dir != 0 {
			m.activateArrow(dir)
			return m, nil
		}
		return m, m.drill()
	case msg.String() == "r":
		return m, m.rescan()
	case msg.String() == "e":
		if m.scanErrTotal > 0 {
			m.mode = modeErrors
			m.errScroll = 0
			return m, nil
		}
	case msg.Type == tea.KeyEsc:
		m.up()
	case msg.Type == tea.KeyPgDown:
		if m.nextPage() {
			m.sel = firstSelectable(m.rects)
		}
	case msg.Type == tea.KeyPgUp:
		if m.prevPage() {
			m.sel = lastSelectable(m.rects)
		}
	case msg.Type == tea.KeyUp, msg.String() == "k":
		m.moveSel(0, -1)
	case msg.Type == tea.KeyDown, msg.String() == "j":
		m.moveSel(0, 1)
	case msg.Type == tea.KeyLeft, msg.String() == "h":
		m.moveSel(-1, 0)
	case msg.Type == tea.KeyRight, msg.String() == "l":
		m.moveSel(1, 0)
	}
	return m, nil
}

func (m *Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.closeConfirm()
		return m, nil
	case tea.KeyCtrlC:
		return m.quit()
	case tea.KeyEnter:
		if m.input.Value() == m.confirmNode.Name {
			return m.doDelete()
		}
		m.confirmErr = true
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m *Model) quit() (tea.Model, tea.Cmd) {
	if m.cancelScan != nil {
		m.cancelScan()
	}
	return m, tea.Quit
}

// updateMouse handles pointer interaction: click selects, a same-tile click
// within the debounce window opens the entry (drilling into a directory,
// launching a file in the OS default app, or flipping a page from a pagination
// arrow), right-click goes up, and the wheel moves the selection.
func (m *Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		tile := m.cellAt(msg.X, msg.Y)
		if tile < 0 || tile >= len(m.rects) || !isNavigableRect(m.rects[tile].Index) {
			return m, nil
		}
		now := time.Now()
		double := tile == m.lastClickTile && now.Sub(m.lastClick) < 350*time.Millisecond
		m.lastClickTile = tile
		m.lastClick = now
		m.sel = tile
		if double {
			if dir, ok := arrowOf(m.rects[tile].Index); ok {
				m.activateArrow(dir)
				return m, nil
			}
			n := m.selectedNode()
			if n != nil && !n.IsDir {
				return m, m.openFile()
			}
			return m, m.drill()
		}
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight:
		m.up()
	case msg.Button == tea.MouseButtonWheelUp:
		m.moveSel(0, -1)
	case msg.Button == tea.MouseButtonWheelDown:
		m.moveSel(0, 1)
	}
	return m, nil
}

// openConfirm opens the delete-confirmation modal for the selected node. It
// does nothing when there is nothing deletable selected (e.g. the "other"
// bucket or the scan root itself).
func (m *Model) openConfirm() {
	n := m.selectedNode()
	if n == nil || n.Path == m.rootPath {
		return
	}
	m.mode = modeConfirm
	m.confirmNode = n
	m.confirmErr = false
	m.input.SetValue("")
	m.input.Focus()
}

func (m *Model) closeConfirm() {
	m.mode = modeBrowse
	m.confirmNode = nil
	m.input.Blur()
}

// doDelete removes the confirmed node, updates ancestry sizes, and re-lays-out
// the current view. It only runs after the user typed the exact name.
func (m *Model) doDelete() (tea.Model, tea.Cmd) {
	n := m.confirmNode
	if n == nil {
		m.closeConfirm()
		return m, nil
	}
	if err := os.RemoveAll(n.Path); err != nil {
		logging.Errorf("delete failed (path=%q): %v", n.Path, err)
		m.mode = modeError
		m.errMessage = fmt.Sprintf("delete %s: %v", n.Path, err)
		m.confirmNode = nil
		return m, nil
	}
	logging.Debugf("deleted path=%q size=%d", n.Path, n.Size)
	// Detach the node from its parent and subtract its size from every ancestor
	// (the parent `current` and everything above it in the crumb stack).
	for i, c := range m.current.Children {
		if c == n {
			m.current.Children = append(m.current.Children[:i], m.current.Children[i+1:]...)
			break
		}
	}
	m.current.Size -= n.Size
	for _, anc := range m.crumbs {
		anc.Size -= n.Size
	}
	// Drop the stale recorded total so a future repeat of this directory is
	// re-measured on the spot.
	if m.scanner != nil {
		m.scanner.Forget(n.Path)
	}
	m.closeConfirm()
	m.buildLayout()
	return m, nil
}

// rescan starts a fresh measure of the root and returns the command batch to
// run it. Existing scan work is cancelled and its result will be ignored.
func (m *Model) rescan() tea.Cmd {
	logging.Debugf("rescan requested (root=%q)", m.rootPath)
	if m.cancelScan != nil {
		m.cancelScan()
	}
	m.scanGen++
	m.progress = scan.Progress{}
	m.progressCh = make(chan scan.Progress, 128)
	m.mode = modeScanning
	m.scanner = nil
	m.tree = nil
	m.current = nil
	m.crumbs = nil
	m.pending = nil
	m.sel = -1 // a fresh scan restarts the selection at the first tile
	m.page = 0
	m.scanRoot = nil
	m.scanSprings = nil
	m.scanTicking = false
	m.scanErrors = nil
	m.scanErrTotal = 0
	m.errScroll = 0
	return tea.Batch(m.spinner.Tick, m.startScan(m.scanGen), m.progressCmd())
}

// drill opens the selected directory. If its children have not been expanded
// yet, it starts a measurement pass first (measure-then-open) and returns the
// command to run it. Excluded subtrees (system dirs) are never traversed.
func (m *Model) drill() tea.Cmd {
	n := m.selectedNode()
	if n == nil || !n.IsDir {
		return nil
	}
	if n.Approx {
		return nil
	}
	if n.Children == nil && m.scanner != nil {
		m.mode = modeMeasuring
		m.pending = n
		return m.expandCmd(n)
	}
	m.crumbs = append(m.crumbs, m.current)
	m.current = n
	m.buildLayout()
	return nil
}

// openFile launches the selected file in the OS default application. The
// command is started detached and only its launch errors are reported, so an
// app that stays open (a media player, an editor) does not tie up the TUI.
func (m *Model) openFile() tea.Cmd {
	n := m.selectedNode()
	if n == nil || n.IsDir {
		return nil
	}
	path := n.Path
	logging.Debugf("opening file in default app: %q", path)
	return func() tea.Msg {
		return runSafe("open "+path, func() tea.Msg {
			if err := openDefault(path).Start(); err != nil {
				return openDoneMsg{path: path, err: err}
			}
			return nil
		})
	}
}

func (m *Model) up() {
	if len(m.crumbs) == 0 {
		return
	}
	old := m.current
	m.current = m.crumbs[len(m.crumbs)-1]
	m.crumbs = m.crumbs[:len(m.crumbs)-1]
	m.buildLayout()
	// Restore the selection to the rectangle that was previously drilled, even
	// when it sits on a later page of the parent.
	for ci, c := range m.current.Children {
		if c != old {
			continue
		}
		if p := m.pageOf[ci]; p >= 0 && p < m.pageCount && p != m.page {
			w, h := m.treemapSize()
			m.layoutPage(p, w, h)
		}
		for ri := range m.rects {
			idx := m.rects[ri].Index
			if idx >= 0 && idx < len(m.pageIdx) && m.current.Children[m.pageIdx[idx]] == old {
				m.sel = ri
				return
			}
		}
	}
}

// buildLayout sorts the current directory's children and re-computes the
// treemap rectangles for the current page and terminal size.
func (m *Model) buildLayout() {
	if m.current == nil {
		return
	}
	children := m.current.Children
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].Size != children[j].Size {
			return children[i].Size > children[j].Size
		}
		return children[i].Name < children[j].Name
	})
	w, h := m.treemapSize()

	// Non-empty children in layout order (global indices); zero-size children
	// are never rendered and are counted as hidden.
	ordered := make([]int, 0, len(children))
	m.hidden = 0
	for i, c := range children {
		if c.Size <= 0 {
			m.hidden++
			continue
		}
		ordered = append(ordered, i)
	}
	if len(ordered) == 0 {
		m.page, m.pageCount = 0, 0
		m.ordered, m.pageStart, m.pageOf, m.pageIdx = nil, nil, nil, nil
		m.rects, m.raster, m.tiles = nil, nil, nil
		m.nextArrow, m.prevArrow = -1, -1
		return
	}

	// Chunk into pages, each keeping every tile at or above the legibility
	// floor; pages are rebuilt whenever the layout is (resize, drill, delete).
	m.buildPages(ordered, w, h, children)
	if m.page < 0 || m.page >= m.pageCount {
		m.page = 0
	}
	m.layoutPage(m.page, w, h)

	if m.sel >= len(m.rects) {
		m.sel = -1
	}
	if m.sel < 0 {
		m.sel = firstSelectable(m.rects)
	}
}

// buildPages partitions ordered (sorted-descending child indices) into pages.
// All pages share one bytes→cells scale (the whole level's), so items keep
// their true relative size across pages: page 1 holds the biggest entries, the
// last page the smallest, drawn at that shared scale down to the minimum tile
// size. Feasibility at a fixed scale is not monotonic in the run length
// (uniform tiles only pack into complete rows), so each page is found by
// scanning down from the largest possible run to the first feasible one, and a
// remainder too small to form a legible row becomes its own page whose tiles
// are lifted to the floor.
//
// The layout is first chunked at the full width. If the level needs more than
// one page, pages are re-chunked at the content width a single pagination
// arrow leaves (w − 3 columns), so every page stays legible both inside its
// arrow gutter and at the tighter boxes pages share.
func (m *Model) buildPages(ordered []int, w, h int, children []*scan.Node) {
	m.chunkPages(ordered, w, h, w, children)
	if m.pageCount <= 1 {
		return
	}
	cw := w - pageGutterCols
	if cw < 1 {
		cw = 1
	}
	m.chunkPages(ordered, w, h, cw, children)
	if m.pageCount <= 1 {
		// The tighter scale fit the whole level on one page, so no arrows are
		// needed after all; restore the full-width layout.
		m.chunkPages(ordered, w, h, w, children)
	}
}

// chunkPages runs one page-chunking pass: bounds every page by grid area at the
// given content width, trims to the layout-feasible prefix, and records the
// shared scale and per-page minimum tile area for that width.
func (m *Model) chunkPages(ordered []int, w, h, cw int, children []*scan.Node) {
	n := len(ordered)
	m.ordered = ordered
	m.pageOf = make([]int, len(children))
	for i := range m.pageOf {
		m.pageOf[i] = -1
	}
	m.pageStart = make([]int, 0, n)
	m.pageMin = nil
	gridArea := float64(cw * h)
	m.pageScale = gridArea / float64(m.orderedTotal(children))

	// Cumulative clamped area: page boundaries are first bounded by area (a
	// feasible page can never exceed the grid), then trimmed by a real layout.
	cum := make([]float64, n+1)
	for i, id := range ordered {
		a := float64(layoutSize(children[id].Size)) * m.pageScale
		if a < minTileArea {
			a = minTileArea
		}
		cum[i+1] = cum[i] + a
	}

	start := 0
	for start < n {
		m.pageStart = append(m.pageStart, start)
		page := len(m.pageStart) - 1
		// Largest end whose clamped area fits the grid (cumulative is sorted).
		lo, hi := start+1, n
		for lo <= hi {
			mid := (lo + hi) / 2
			if cum[mid]-cum[start] <= gridArea {
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
		end := lo - 1
		if end <= start {
			end = start + 1
		}
		// Trim past the trailing partial row (usually a handful of items).
		for end > start && !pageFeasible(ordered[start:end], cw, h, children, m.pageScale, minTileArea) {
			end--
		}
		minArea := float64(minTileArea)
		if end == start {
			// The remainder cannot form a legible row at the floor; take it all
			// and lift its tiles until they lay out legibly. The lift is
			// bounded: on a grid too small to ever hold a legible tile (the
			// view already reports "terminal too small") no min area works, so
			// give up and render whatever fits.
			end = n
			for iters := 0; iters < 24 && !pageFeasible(ordered[start:end], cw, h, children, m.pageScale, minArea); iters++ {
				minArea *= 2
			}
		}
		for i := start; i < end; i++ {
			m.pageOf[ordered[i]] = page
		}
		m.pageMin = append(m.pageMin, minArea)
		start = end
	}
	m.pageStart = append(m.pageStart, n)
	m.pageCount = len(m.pageStart) - 1
}

// orderedTotal sums the layout sizes of the level's non-zero children.
func (m *Model) orderedTotal(children []*scan.Node) int64 {
	var total int64
	for _, c := range children {
		total += layoutSize(c.Size)
	}
	return total
}

// pageFeasible reports whether laying the given items out at the shared scale,
// clamping each to at least minArea cells, keeps every tile at or above the
// label floor and stays within the grid. An empty page trivially does; on a
// grid too small for the floor, the floor is waived.
func pageFeasible(ids []int, w, h int, children []*scan.Node, scale, minArea float64) bool {
	if len(ids) == 0 {
		return true
	}
	if w < minTileCols || h < minTileRows {
		return true
	}
	items := make([]treemap.Item, len(ids))
	for i, id := range ids {
		items[i] = treemap.Item{Name: "", Size: layoutSize(children[id].Size), Selectable: true}
	}
	rs := treemap.LayoutFixed(items, w, h, scale, minArea)
	if len(rs) != len(ids) {
		return false // some items would be crushed off the grid
	}
	for _, r := range rs {
		if r.W < minLabelCols || r.H < minLabelRows {
			return false
		}
	}
	return true
}

// layoutPage lays page p of the current level out at the shared scale and
// fills the rect/raster/tile buffers. rect Index refers to the position in the
// page's item list, which resolves through m.pageIdx to a global child index.
// When the level is paged, full-height gutter strips with pagination arrows
// are appended to the right and/or left edges of the grid.
func (m *Model) layoutPage(p, w, h int) {
	m.page = p
	ids := m.ordered[m.pageStart[p]:m.pageStart[p+1]]
	m.pageIdx = ids
	items := make([]treemap.Item, len(ids))
	for i, id := range ids {
		c := m.current.Children[id]
		items[i] = treemap.Item{Name: c.Name, Size: layoutSize(c.Size), Selectable: true}
	}

	// Compute content box: arrows take a 3-column gutter on each edge.
	m.nextArrow = -1
	m.prevArrow = -1
	prev := p > 0
	next := p < m.pageCount-1
	xoff := 0
	contentW := w
	if prev {
		contentW -= pageGutterCols
		xoff = pageGutterCols
	}
	if next {
		contentW -= pageGutterCols
	}
	if contentW < 1 {
		contentW = 1
	}
	// Lay tiles out in the content box, then shift by the x offset.
	m.rects = treemap.LayoutFixed(items, contentW, h, m.pageScale, m.pageMin[p])
	for i := range m.rects {
		m.rects[i].X += float64(xoff)
	}
	// Append gutter strip rects for pagination arrows.
	if prev {
		m.prevArrow = len(m.rects)
		m.rects = append(m.rects, treemap.Rect{
			Index: idxPagePrev,
			X: 0, Y: 0,
			W: float64(pageGutterCols), H: float64(h),
		})
	}
	if next {
		m.nextArrow = len(m.rects)
		m.rects = append(m.rects, treemap.Rect{
			Index: idxPageNext,
			X: float64(w - pageGutterCols), Y: 0,
			W: float64(pageGutterCols), H: float64(h),
		})
	}
	m.raster = treemap.Raster(m.rects, w, h)
	m.tiles = colorTiles(m.rects, m.raster, w, h)
}

// addScanChildren grows the animation tree with the root children whose sizes
// completed since the last event. Each new block starts as a sliver (its spring
// eases it up to the real size), so the map builds out in exploration order.
func (m *Model) addScanChildren(completed []scan.ChildSize) {
	if m.scanRoot == nil {
		m.scanRoot = &scan.Node{Name: m.rootPath, Path: m.rootPath, IsDir: true}
	}
	for _, cs := range completed {
		n := &scan.Node{
			Name: cs.Name,
			Path: filepath.Join(m.rootPath, cs.Name),
			Size: cs.Size,
		}
		m.scanRoot.Children = append(m.scanRoot.Children, n)
		m.scanSprings = append(m.scanSprings, scanSpring{
			pos:    scanInitialPos,
			vel:    0,
			target: float64(layoutSize(cs.Size)),
		})
	}
	m.current = m.scanRoot
	m.tree = m.scanRoot
	m.buildScanLayout()
}

// buildScanLayout lays the completed children out at a scale that fills the
// grid, so the first completed block occupies the whole screen and each later
// one takes its proportional share. Tiles are eased (spring) sizes, no area
// floor, so new blocks grow smoothly from a sliver instead of snapping in.
//
// LayoutFixed sorts items by size and rect.Index is the position in that
// sorted list, so items are passed already sorted (the same invariant the
// browse layout relies on). ordered maps a sorted position back to a
// scanChildren index; it doubles as pageIdx so frame/selectedNode resolve
// rects to the right child.
func (m *Model) buildScanLayout() {
	children := m.scanChildren()
	w, h := m.treemapSize()
	if len(children) == 0 {
		m.page, m.pageCount = 0, 0
		m.pageIdx = nil
		m.rects, m.raster, m.tiles = nil, nil, nil
		m.nextArrow, m.prevArrow = -1, -1
		m.sel = -1
		return
	}
	ordered := make([]int, len(children))
	for i := range ordered {
		ordered[i] = i
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return m.scanSprings[ordered[i]].pos > m.scanSprings[ordered[j]].pos
	})
	items := make([]treemap.Item, len(children))
	var total float64
	for k, ci := range ordered {
		size := m.scanSprings[ci].pos
		if size < 1 {
			size = 1
		}
		items[k] = treemap.Item{Name: children[ci].Name, Size: int64(math.Ceil(size)), Selectable: true}
		total += size
	}
	scale := float64(w*h) / total
	m.rects = treemap.LayoutFixed(items, w, h, scale, 0)
	m.raster = treemap.Raster(m.rects, w, h)
	// A single page; rect Index equals the position in ordered, which maps to
	// the matching scan child.
	m.page, m.pageCount = 0, 1
	m.nextArrow, m.prevArrow = -1, -1
	m.pageIdx = ordered
	m.ordered, m.pageStart, m.pageOf = nil, nil, nil
	m.tiles = colorTiles(m.rects, m.raster, w, h)
	if m.sel >= len(m.rects) {
		m.sel = -1
	}
	if m.sel < 0 {
		m.sel = firstSelectable(m.rects)
	}
}

// scanChildren returns the animation tree's completed children.
func (m *Model) scanChildren() []*scan.Node {
	if m.scanRoot == nil {
		return nil
	}
	return m.scanRoot.Children
}

// scanTotal sums the real byte sizes of the completed children so far.
func (m *Model) scanTotal() int64 {
	var total int64
	for _, c := range m.scanChildren() {
		total += c.Size
	}
	return total
}

// armScanTicker starts the growth-animation frame loop if it is not already
// running. Returns nil when one is already armed.
func (m *Model) armScanTicker() tea.Cmd {
	if m.scanTicking {
		return nil
	}
	m.scanTicking = true
	return m.scanFrameCmd()
}

// scanFrameCmd schedules the next growth frame. It is one-shot: the handler
// re-arms it while blocks are still moving.
func (m *Model) scanFrameCmd() tea.Cmd {
	return tea.Tick(time.Second/scanFPS, func(t time.Time) tea.Msg {
		return scanFrameMsg(t)
	})
}

// stepScanSprings eases every block one frame toward its real size. It reports
// whether any block is still moving.
func (m *Model) stepScanSprings() bool {
	moving := false
	for i := range m.scanSprings {
		s := &m.scanSprings[i]
		s.pos, s.vel = m.scanSpring.Update(s.pos, s.vel, s.target)
		if math.Abs(s.pos-s.target) > scanSettleEps {
			moving = true
		}
	}
	return moving
}

// settleScanSprings snaps every block to its final size and stops the ticker.
// Used by tests to reach a deterministic end state.
func (m *Model) settleScanSprings() {
	for i := range m.scanSprings {
		m.scanSprings[i].pos = m.scanSprings[i].target
		m.scanSprings[i].vel = 0
	}
	m.scanTicking = false
	m.buildScanLayout()
}

func (m *Model) treemapSize() (int, int) {
	// The screen always reserves a breadcrumb row, an info row, and the bottom
	// keybind bar; the free-space gutter eats into the remaining treemap space.
	w, h := m.width-2, m.height-3
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	h -= m.freeRows()
	if h < 1 {
		h = 1
	}
	return w, h
}

// freeRows is the height in rows reserved for the free-space gutter: shown
// only at the scan root when free space is known, and capped small so it never
// eats the map.
func (m *Model) freeRows() int {
	if m.freeBytes <= 0 || m.current != m.tree {
		return 0
	}
	n := (m.height - 2) / 12
	if n < 1 {
		n = 1
	}
	if n > 3 {
		n = 3
	}
	return n
}

// layoutSize maps a byte count to a monotonic, ranking-preserving area input;
// the square root tames dominant entries for legible square tiles.
func layoutSize(n int64) int64 {
	if n <= 0 {
		return 0
	}
	return 1 + int64(math.Sqrt(float64(n)))
}

// selectedNode returns the scan node behind the current selection, or nil when
// the selection is on the "other" bucket or out of range.
func (m *Model) selectedNode() *scan.Node {
	if m.sel < 0 || m.sel >= len(m.rects) || m.current == nil {
		return nil
	}
	idx := m.rects[m.sel].Index
	if idx < 0 || idx >= len(m.pageIdx) {
		return nil
	}
	return m.current.Children[m.pageIdx[idx]]
}

// selectedArrow reports which pagination arrow strip the selection is on: +1
// for the next-page arrow, -1 for the previous-page arrow, 0 when the selection
// is on a real tile or nowhere.
func (m *Model) selectedArrow() int {
	if m.sel < 0 || m.sel >= len(m.rects) {
		return 0
	}
	switch m.rects[m.sel].Index {
	case idxPageNext:
		return 1
	case idxPagePrev:
		return -1
	}
	return 0
}

// arrowOf maps a rect index to its pagination direction (+1 next, -1 prev);
// ok is false when the rect is not an arrow strip.
func arrowOf(index int) (dir int, ok bool) {
	switch index {
	case idxPageNext:
		return 1, true
	case idxPagePrev:
		return -1, true
	}
	return 0, false
}

// activateArrow flips the page in the given arrow direction and parks the
// selection on a real tile of the new page (the first going forward, the last
// going back). It reports whether a page flipped.
func (m *Model) activateArrow(dir int) bool {
	var ok bool
	if dir > 0 {
		ok = m.nextPage()
	} else {
		ok = m.prevPage()
	}
	if !ok {
		return false
	}
	if dir > 0 {
		m.sel = firstSelectable(m.rects)
	} else {
		m.sel = lastSelectable(m.rects)
	}
	return true
}

// arrowRune is the glyph for a pagination arrow direction (dir > 0 → next).
func arrowRune(dir int) string {
	if dir > 0 {
		return "→"
	}
	return "←"
}

func firstSelectable(rects []treemap.Rect) int {
	for i := range rects {
		if rects[i].Index >= 0 {
			return i
		}
	}
	return -1
}

// lastSelectable is the trailing real tile: the smallest, laid out last and
// sitting at the bottom-right of the map. It anchors backward page landing.
func lastSelectable(rects []treemap.Rect) int {
	for i := len(rects) - 1; i >= 0; i-- {
		if rects[i].Index >= 0 {
			return i
		}
	}
	return -1
}

// nextPage flips to the following page of the current level, relaying it out.
// It reports whether a page existed to flip to. The caller decides the new
// selection.
func (m *Model) nextPage() bool {
	if m.page >= m.pageCount-1 {
		return false
	}
	w, h := m.treemapSize()
	m.layoutPage(m.page+1, w, h)
	return true
}

// prevPage flips to the previous page of the current level, relaying it out.
// It reports whether a page existed to flip to. The caller decides the new
// selection.
func (m *Model) prevPage() bool {
	if m.page <= 0 {
		return false
	}
	w, h := m.treemapSize()
	m.layoutPage(m.page-1, w, h)
	return true
}

func isQuit(msg tea.KeyMsg) bool {
	return msg.String() == "q" || msg.Type == tea.KeyCtrlC
}
