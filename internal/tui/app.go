package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/metruzanca/spacefinder/internal/scan"
	"github.com/metruzanca/spacefinder/internal/treemap"
)

const maxRects = 128

type mode int

const (
	modeSplash mode = iota
	modeMeasuring
	modeBrowse
	modeConfirm
	modeError
)

type scanDoneMsg struct {
	gen     int
	scanner *scan.Scanner
	root    *scan.Node
	err     error
}

type expandDoneMsg struct {
	node *scan.Node
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
	sel     int

	// delete-confirm state
	confirmNode *scan.Node
	input       textinput.Model
	confirmErr  bool
}

func newModel(rootPath string) *Model {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		abs = rootPath
	}
	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(accent()))
	input := textinput.New()
	input.Placeholder = "type the exact name"
	input.CharLimit = 256
	input.Width = 36
	return &Model{
		rootPath:   filepath.Clean(abs),
		mode:       modeSplash,
		start:      time.Now(),
		spinner:    sp,
		input:      input,
		sel:        -1,
		progressCh: make(chan scan.Progress, 128),
	}
}

// Run starts the TUI in the alt-screen and blocks until it exits.
func Run(rootPath string) error {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		return errors.New("spacefinder is an interactive terminal app; run it in a terminal")
	}
	p := tea.NewProgram(newModel(rootPath), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.startScan(0), m.progressCmd())
}

// startScan runs a fresh measure pass of the root and reports its result. The
// generation allows stale results (from a cancelled rescan) to be ignored.
func (m *Model) startScan(gen int) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelScan = cancel
	ch := m.progressCh
	return func() tea.Msg {
		scanner, root, err := scan.Measure(ctx, m.rootPath, ch)
		return scanDoneMsg{gen: gen, scanner: scanner, root: root, err: err}
	}
}

// progressCmd forwards one throttled progress event per invocations; it returns
// nil (a no-op) once the progress channel closes, ending the redeliver loop.
func (m *Model) progressCmd() tea.Cmd {
	ch := m.progressCh
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return scanProgressMsg(p)
	}
}

// expandCmd measures the next level of a directory (usually instant, since the
// totals were recorded by the initial pass) before it is opened.
func (m *Model) expandCmd(node *scan.Node) tea.Cmd {
	sc := m.scanner
	return func() tea.Msg {
		if sc == nil {
			return expandDoneMsg{node: node}
		}
		return expandDoneMsg{node: node, err: sc.Expand(context.Background(), node)}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.mode == modeBrowse && m.current != nil {
			m.buildLayout()
		}
		return m, nil

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
		m.mode = modeBrowse
		m.buildLayout()
		return m, nil
	}
	return m, nil
}

func (m *Model) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeSplash, modeError:
		if isQuit(msg) {
			return m.quit()
		}
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
		items[i] = treemap.Item{Name: c.Name, Size: c.Size, Selectable: true}
	}
	m.rects = treemap.Layout(items, w, h, maxRects)
	m.raster = treemap.Raster(m.rects, w, h)
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
	return w, h
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
