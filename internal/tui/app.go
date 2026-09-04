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

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/metruzanca/spacefinder/internal/logging"
	"github.com/metruzanca/spacefinder/internal/scan"
	"github.com/metruzanca/spacefinder/internal/treemap"
)

const maxRects = 200

// minTileCells is the smallest footprint a directory may occupy in the
// treemap before it is considered insignificant and folded into the (hidden,
// non-selectable) "other" bucket instead of being rendered.
const minTileCells = 4

type mode int

const (
	modeSplash mode = iota
	modeMeasuring
	modeBrowse
	modeConfirm
	modeError
	modePicker
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

type scanProgressMsg scan.Progress

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

	// free-space gutter (only shown at the scan root)
	freeBytes int64

	// mouse state
	lastClick     time.Time
	lastClickTile int

	// delete-confirm state
	confirmNode *scan.Node
	input       textinput.Model
	confirmErr  bool

	// filesystem picker (modePicker, shown when spacefinder runs without a path)
	picker list.Model
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
		mode:       modeSplash,
		start:      time.Now(),
		spinner:    sp,
		input:      input,
		sel:        -1,
		width:      80,
		height:     24,
		progressCh: make(chan scan.Progress, 128),
	}
	if rootPath == "" {
		m.rootPath = ""
		m.mode = modePicker
		m.picker = newPicker()
	}
	return m
}

// pickerItem is one entry in the filesystem picker: the friendly row shows the
// path to scan with a description naming the kind of entry and its free space.
type pickerItem struct {
	title, desc, path string
}

func (i pickerItem) Title() string       { return i.title }
func (i pickerItem) Description() string { return i.desc }
func (i pickerItem) FilterValue() string { return i.title + " " + i.path }

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

// newPicker builds the picker list: home (pre-selected), the filesystem root,
// the current directory, then every detected mount worth scanning.
func newPicker() list.Model {
	home, _ := os.UserHomeDir()
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	root := fsRoot(home, cwd)

	var items []list.Item
	add := func(path, kind string) {
		if path == "" {
			return
		}
		desc := kind
		if n := freeOn(path); n > 0 {
			desc += " · " + formatBytes(n) + " free"
		}
		items = append(items, pickerItem{title: path, desc: desc, path: path})
	}

	// Home is pre-selected: it is the most common scan target.
	add(home, "home")
	add(root, "root")
	if cwd != "" && cwd != home && filepath.Clean(cwd) != root {
		add(cwd, "current directory")
	}
	for _, mo := range scan.Mounts() {
		if mo.Path == home || mo.Path == root || mo.Path == cwd {
			continue // already offered above
		}
		desc := mo.Type
		if mo.Device != "" && mo.Device != mo.Path {
			desc = mo.Device + " (" + mo.Type + ")"
		}
		if mo.Free > 0 {
			desc += " · " + formatBytes(mo.Free) + " free"
		}
		items = append(items, pickerItem{title: mo.Path, desc: desc, path: mo.Path})
	}

	p := list.New(items, list.NewDefaultDelegate(), pickerWidth(80), pickerHeight(24))
	p.SetFilteringEnabled(false)
	p.SetShowTitle(false)
	p.SetShowStatusBar(false)
	p.SetShowPagination(false)
	p.SetShowHelp(false)
	p.SetShowFilter(false)
	p.DisableQuitKeybindings()
	if len(items) > 0 {
		p.Select(0)
	}
	return p
}

// pickerSize maps the current terminal size onto the picker list's frame,
// leaving room for the title above and the hints below.
func (m *Model) pickerSize() (w, h int) {
	return pickerWidth(m.width), pickerHeight(m.height)
}

func pickerWidth(w int) int {
	if w < 1 {
		w = 80
	}
	if w-4 < 10 {
		return 10
	}
	return w - 4
}

func pickerHeight(h int) int {
	if h < 1 {
		h = 24
	}
	if h-6 < 2 {
		return 2
	}
	return h - 6
}

// Run starts the TUI in the alt-screen and blocks until it exits.
func Run(rootPath string) error {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		return errors.New("spacefinder is an interactive terminal app; run it in a terminal")
	}
	p := tea.NewProgram(newModel(rootPath), tea.WithAltScreen(), tea.WithMouseCellMotion())
	// Mirror stray stdout writes (bubbletea's own recovered-panic traces,
	// debug.PrintStack output) into the debug log so a panic outside our
	// recover handlers is still captured. The renderer writes through the fd
	// it captured at NewProgram, so normal rendering is not mirrored.
	if lw := logging.LogWriter(); lw != nil {
		defer teeStdout(lw)()
	}
	_, err := p.Run()
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
		return nil // the picker needs no startup commands
	}
	return tea.Batch(m.spinner.Tick, m.startScan(0), m.progressCmd())
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
		return m, nil

	case tea.MouseMsg:
		return m.updateMouse(msg)

	case tea.KeyMsg:
		return m.updateKeys(msg)

	case spinner.TickMsg:
		if m.mode == modeSplash || m.mode == modeMeasuring {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case scanProgressMsg:
		m.progress = scan.Progress(msg)
		// Re-arm the consumer so the drain loop keeps running; otherwise the
		// scan's progress channel fills and the measure goroutine blocks.
		return m, m.progressCmd()

	case expandDoneMsg:
		if msg.node != m.pending {
			return m, nil // stale or cancelled
		}
		m.pending = nil
		if msg.err != nil {
			m.mode = modeError
			m.errMessage = msg.err.Error()
			return m, nil
		}
		m.crumbs = append(m.crumbs, m.current)
		m.current = msg.node
		m.mode = modeBrowse
		m.buildLayout()
		return m, nil

	case openDoneMsg:
		if msg.err != nil {
			m.mode = modeError
			m.errMessage = fmt.Sprintf("open %s: %v", msg.path, msg.err)
		}
		return m, nil

	case internalErrMsg:
		m.mode = modeError
		m.errMessage = msg.err.Error()
		return m, nil

	case scanDoneMsg:
		if msg.gen != m.scanGen {
			return m, nil // result of a cancelled rescan
		}
		if msg.err != nil {
			m.mode = modeError
			m.errMessage = msg.err.Error()
			return m, nil
		}
		m.scanner = msg.scanner
		m.tree = msg.root
		m.current = msg.root
		m.crumbs = nil
		m.freeBytes = freeOn(m.rootPath)
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

func (m *Model) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeSplash, modeError:
		if isQuit(msg) {
			return m.quit()
		}
	case modePicker:
		return m.updatePicker(msg)
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
	case modeBrowse:
		return m.updateBrowse(msg)
	}
	return m, nil
}

// updatePicker handles the filesystem picker: enter starts the scan of the
// highlighted entry, q/esc/ctrl+c quits, and everything else (arrows, j/k,
// pgup/pgdn, mouse) is delegated to the list component.
func (m *Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case isQuit(msg), msg.Type == tea.KeyEsc:
		return m.quit()
	case msg.Type == tea.KeyEnter:
		item := m.picker.SelectedItem()
		if item == nil {
			return m, nil
		}
		return m.beginScan(item.(pickerItem).path)
	default:
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		return m, cmd
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
	m.mode = modeSplash
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
	case msg.Type == tea.KeyEnter:
		return m, m.drill()
	case msg.String() == "r":
		return m, m.rescan()
	case msg.Type == tea.KeyEsc:
		m.up()
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
// launching a file in the OS default app), right-click goes up, and the wheel
// moves the selection.
func (m *Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
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
		m.mode = modeError
		m.errMessage = fmt.Sprintf("delete %s: %v", n.Path, err)
		m.confirmNode = nil
		return m, nil
	}
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
	if m.cancelScan != nil {
		m.cancelScan()
	}
	m.scanGen++
	m.progress = scan.Progress{}
	m.progressCh = make(chan scan.Progress, 128)
	m.mode = modeSplash
	m.scanner = nil
	m.tree = nil
	m.current = nil
	m.crumbs = nil
	m.pending = nil
	return tea.Batch(m.spinner.Tick, m.startScan(m.scanGen), m.progressCmd())
}

// drill opens the selected directory. If its children have not been expanded
// yet, it starts a measurement pass first (measure-then-open) and returns the
// command to run it.
func (m *Model) drill() tea.Cmd {
	n := m.selectedNode()
	if n == nil || !n.IsDir {
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
	// Restore the selection to the rectangle that was previously drilled.
	for ri := range m.rects {
		idx := m.rects[ri].Index
		if idx >= 0 && idx < len(m.current.Children) && m.current.Children[idx] == old {
			m.sel = ri
			return
		}
	}
}

// buildLayout sorts the current directory's children and re-computes the
// treemap rectangles for the current terminal size.
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
	items := make([]treemap.Item, len(children))
	for i, c := range children {
		// sqrt-scaled area: monotonic and ranking-preserving, but a dominant
		// folder no longer swamps the view, so small entries stay square-ish.
		// Labels and status percentages keep the true byte counts.
		items[i] = treemap.Item{Name: c.Name, Size: layoutSize(c.Size), Selectable: true}
	}
	m.rects = treemap.LayoutWith(items, w, h, maxRects, minTileCells)
	m.raster = treemap.Raster(m.rects, w, h)
	// Precompute every tile's style set (per-child colour) and count the
	// children that end up not rendered at all (zero-size or too small to
	// matter): they are neither drawn nor selectable.
	m.tiles = make([]tileStyle, len(m.rects))
	rendered := make(map[int]bool, len(m.rects))
	for i := range m.rects {
		idx := m.rects[i].Index
		if idx < 0 {
			m.tiles[i] = otherStyle
			continue
		}
		rendered[idx] = true
		m.tiles[i] = styleFor(children[idx].Name)
	}
	m.hidden = 0
	for i, c := range children {
		if c.Size <= 0 || !rendered[i] {
			m.hidden++
		}
	}
	if m.sel >= len(m.rects) {
		m.sel = -1
	}
	if m.sel < 0 {
		m.sel = firstSelectable(m.rects)
	}
}

func (m *Model) treemapSize() (int, int) {
	w, h := m.width-2, m.height-2
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
	if idx < 0 || idx >= len(m.current.Children) {
		return nil
	}
	return m.current.Children[idx]
}

func firstSelectable(rects []treemap.Rect) int {
	for i := range rects {
		if rects[i].Index >= 0 {
			return i
		}
	}
	return -1
}

func isQuit(msg tea.KeyMsg) bool {
	return msg.String() == "q" || msg.Type == tea.KeyCtrlC
}
