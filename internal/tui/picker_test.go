package tui

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func pickerModel() *Model {
	m := newModel("")
	m.width, m.height = 80, 24
	m.buildPickerLayout()
	return m
}

func pickerPaths(m *Model) []string {
	paths := make([]string, len(m.pickerNodes))
	for i, n := range m.pickerNodes {
		paths[i] = n.Path
	}
	return paths
}

func TestNewModelEmptyStartsPicker(t *testing.T) {
	m := pickerModel()
	if m.mode != modePicker {
		t.Fatalf("mode = %v, want modePicker", m.mode)
	}
	if len(m.pickerNodes) < 2 {
		t.Fatalf("picker has %d options, want at least home+root", len(m.pickerNodes))
	}
}

func TestPickerHasHomeAndRoot(t *testing.T) {
	m := pickerModel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no $HOME")
	}
	paths := pickerPaths(m)
	if paths[0] != home {
		t.Fatalf("first picker option = %q, want home %q (pre-selected)", paths[0], home)
	}
	for _, p := range paths {
		if p == fsRoot(home, "") {
			return
		}
	}
	t.Fatal("picker missing root entry")
}

func TestPickerHomePreSelected(t *testing.T) {
	m := pickerModel()
	n := m.selectedPickerNode()
	if n == nil {
		t.Fatal("no picker tile selected")
	}
	home, _ := os.UserHomeDir()
	if n.Path != home {
		t.Fatalf("selected tile = %q, want home %q", n.Path, home)
	}
}

func TestPickerEnterStartsScan(t *testing.T) {
	m := pickerModel()
	m.moveSel(0, 1)
	want := m.selectedPickerNode().Path
	got, cmd := m.Update(keyType(tea.KeyEnter))
	mm := got.(*Model)
	if mm.mode != modeSplash {
		t.Fatalf("mode = %v, want modeSplash after picking %s", mm.mode, want)
	}
	if mm.rootPath != want {
		t.Fatalf("rootPath = %q, want %q", mm.rootPath, want)
	}
	if cmd == nil {
		t.Fatal("choosePicker returned no startup command")
	}
}

// TestScanResetsSelectionToFirst checks that picking a volume does not carry
// the picker's grid position into the scanned volume's treemap: after the scan
// completes, the selection snaps to the first tile.
func TestScanResetsSelectionToFirst(t *testing.T) {
	m := pickerModel()
	m.moveSel(1, 0) // leave the initial tile
	stale := m.sel
	if stale < 1 {
		t.Skip("picker layout has no second tile to select")
	}
	got, _ := m.Update(keyType(tea.KeyEnter)) // choose -> beginScan
	mm := got.(*Model)
	// Complete the scan with a disposable tree so buildLayout runs.
	got, _ = mm.Update(scanDoneMsg{gen: mm.scanGen, root: testTree()})
	mm = got.(*Model)
	if mm.mode != modeBrowse {
		t.Fatalf("mode = %v, want modeBrowse after scan", mm.mode)
	}
	first := firstSelectable(mm.rects)
	if mm.sel != first {
		t.Fatalf("selection = %d, want first selectable %d (stale picker position was %d)", mm.sel, first, stale)
	}
}

func TestPickerQuitsOnQ(t *testing.T) {
	var out bytes.Buffer
	p := tea.NewProgram(pickerModel(),
		tea.WithInput(strings.NewReader("q")),
		tea.WithOutput(&out),
		tea.WithoutRenderer(),
	)
	if _, err := p.Run(); err != nil {
		t.Fatalf("program exited with error: %v", err)
	}
}

func TestPickerEscDoesNotQuit(t *testing.T) {
	m := pickerModel()
	got, cmd := m.Update(keyType(tea.KeyEsc))
	mm := got.(*Model)
	if mm.mode != modePicker {
		t.Fatalf("esc left the picker: mode = %v", mm.mode)
	}
	if cmd != nil {
		t.Fatal("esc started a command on the picker")
	}
}

func TestPickerQuitsOnCtrlC(t *testing.T) {
	var out bytes.Buffer
	p := tea.NewProgram(pickerModel(),
		tea.WithInput(strings.NewReader("\x03")),
		tea.WithOutput(&out),
		tea.WithoutRenderer(),
	)
	if _, err := p.Run(); err != nil {
		t.Fatalf("program exited with error: %v", err)
	}
}

func TestPickerViewRenders(t *testing.T) {
	m := pickerModel()
	v := m.View()
	if v == "" {
		t.Fatal("picker view empty")
	}
	if !strings.Contains(v, "pick a location to scan") {
		t.Fatal("picker title missing")
	}
	if !strings.Contains(v, "home") {
		t.Fatalf("picker tiles missing; got:\n%s", v)
	}
	if !strings.Contains(v, "mouse supported") {
		t.Fatal("picker hint missing")
	}
}

func TestPickerArrowsMoveSelection(t *testing.T) {
	start := pickerModel().selectedPickerNode().Path
	moved := false
	for _, key := range []string{"l", "h", "j", "k"} {
		m := pickerModel()
		m.Update(keyRunes(key))
		if m.selectedPickerNode().Path != start {
			moved = true
		}
	}
	if !moved {
		t.Fatal("no arrow key moved the picker selection")
	}
}

func TestPickerNavStaysValid(t *testing.T) {
	m := pickerModel()
	for _, key := range []string{"l", "h", "j", "k", "l", "h", "j", "k"} {
		m.Update(keyRunes(key))
		if m.sel < 0 || m.sel >= len(m.rects) {
			t.Fatalf("selection out of range after %q: %d", key, m.sel)
		}
		if m.selectedPickerNode() == nil {
			t.Fatalf("selection on a non-tile after %q", key)
		}
	}
}

// TestPickerTilesSimilarSized verifies the "all options like similar sizes"
// requirement: equal-area tiles, so no single drive dominates the picker map.
func TestPickerTilesSimilarSized(t *testing.T) {
	m := pickerModel()
	if len(m.rects) < 2 {
		t.Skip("need at least two tiles")
	}
	minA, maxA := 1e18, 0.0
	for _, r := range m.rects {
		if r.Index < 0 {
			continue
		}
		area := r.W * r.H
		if area < minA {
			minA = area
		}
		if area > maxA {
			maxA = area
		}
	}
	if ratio := maxA / minA; ratio > 2.0 {
		t.Fatalf("tile areas differ by %.2fx; equal-area layout expected (min %.1f, max %.1f)", ratio, minA, maxA)
	}
}

func TestPickerMouseSelectStartsScan(t *testing.T) {
	m := pickerModel()
	m.sel = 0
	b := cellBounds(m.rects[0])
	cx := b.x0 + (b.x1-b.x0)/2
	cy := b.y0 + (b.y1-b.y0)/2
	want := m.selectedPickerNode().Path
	// A double-click (second press of tile 0 within the debounce window).
	m.lastClickTile = 0
	m.lastClick = time.Now()
	got, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: cx, Y: cy + 1})
	mm := got.(*Model)
	if mm.mode != modeSplash {
		t.Fatalf("double-click did not start the scan; mode = %v", mm.mode)
	}
	if mm.rootPath != want {
		t.Fatalf("double-click scanned %q, want %q", mm.rootPath, want)
	}
}

// TestPickerCwdSizeEstimate feeds the async du result through Update and
// checks the current-directory tile swaps its free bytes for the rough size.
func TestPickerCwdSizeEstimate(t *testing.T) {
	if _, err := exec.LookPath("du"); err != nil {
		t.Skip("du not available")
	}
	m := pickerModel()
	if m.pickerCwd == nil {
		t.Skip("no current-directory tile in this cwd")
	}
	cmd := m.cwdSizeCmd()
	if cmd == nil {
		t.Fatal("expected a du command for the picker")
	}
	got, _ := m.Update(cmd()) // runs du synchronously for the test
	mm := got.(*Model)
	if mm.pickerCwd.Size <= 0 {
		t.Fatalf("cwd estimate not applied: %d", mm.pickerCwd.Size)
	}
	if !mm.pickerUsed[mm.pickerCwd] {
		t.Fatal("cwd tile not marked as an estimate")
	}
	for ri, r := range mm.rects {
		if r.Index >= 0 && r.Index < len(mm.pickerNodes) && mm.pickerNodes[r.Index] == mm.pickerCwd {
			mm.sel = ri
			break
		}
	}
	if !strings.Contains(mm.infoLine(), "rough") {
		t.Fatal("status line does not label the estimate")
	}
}

func TestDuSizeMissingPath(t *testing.T) {
	if got := duSize("/nonexistent/spacefinder/xyz"); got != 0 {
		t.Fatalf("duSize on a missing path = %d, want 0", got)
	}
}

func TestPickerSmallTerminalSafe(t *testing.T) {
	m := pickerModel()
	m.width, m.height = 12, 5
	if v := m.View(); v == "" {
		t.Fatal("picker crashed on a tiny terminal")
	}
}
