package tui

import (
	"bytes"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func pickerModel() *Model {
	m := newModel("")
	m.width, m.height = 80, 24
	return m
}

func TestNewModelEmptyStartsPicker(t *testing.T) {
	m := pickerModel()
	if m.mode != modePicker {
		t.Fatalf("mode = %v, want modePicker", m.mode)
	}
	if m.picker.Items() == nil || len(m.picker.Items()) < 2 {
		t.Fatalf("picker has %d items, want at least home+root", len(m.picker.Items()))
	}
}

func TestPickerHasHomeAndRoot(t *testing.T) {
	m := pickerModel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no $HOME")
	}
	seen := map[string]bool{}
	for _, it := range m.picker.Items() {
		seen[it.(pickerItem).path] = true
	}
	if !seen[home] {
		t.Fatal("picker missing home entry")
	}
	if !seen[fsRoot(home, "")] {
		t.Fatal("picker missing root entry")
	}
	// Home is pre-selected.
	if it := m.picker.SelectedItem(); it == nil || it.(pickerItem).path != home {
		t.Fatal("home not pre-selected")
	}
}

func TestPickerDownThenEnterStartsScan(t *testing.T) {
	m := pickerModel()
	// Move down one and select: the second entry must become the scan root.
	m.picker.CursorDown()
	want := m.picker.SelectedItem().(pickerItem).path
	got, cmd := m.Update(keyType(tea.KeyEnter))
	mm := got.(*Model)
	if mm.mode != modeSplash {
		t.Fatalf("mode = %v, want modeSplash after picking %s", mm.mode, want)
	}
	if mm.rootPath != want {
		t.Fatalf("rootPath = %q, want %q", mm.rootPath, want)
	}
	if cmd == nil {
		t.Fatal("beginScan returned no startup command")
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
	if !strings.Contains(v, "Where should we look?") {
		t.Fatal("picker title missing")
	}
	if !strings.Contains(v, "free") && !strings.Contains(v, "home") {
		t.Fatal("picker view missing entries; got:\n" + v)
	}
	if !strings.Contains(v, "enter to scan") {
		t.Fatal("picker hint missing")
	}
}

func TestPickerArrowsMoveSelection(t *testing.T) {
	m := pickerModel()
	before := m.picker.SelectedItem().(pickerItem).path
	m.Update(keyRunes("j"))
	after := m.picker.SelectedItem().(pickerItem).path
	if after == before {
		t.Fatal("j did not move the picker selection")
	}
	m.Update(keyRunes("k"))
	want := m.picker.SelectedItem().(pickerItem).path
	if want != before {
		t.Fatalf("k did not move back: want start %q, got %q", before, want)
	}
}

func TestPickerSmallTerminalSafe(t *testing.T) {
	m := pickerModel()
	m.width, m.height = 12, 5
	if v := m.View(); v == "" {
		t.Fatal("picker crashed on a tiny terminal")
	}
}
