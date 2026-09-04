package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpOpensFromBrowse(t *testing.T) {
	m := browseModel()
	got, cmd := m.Update(keyRunes("?"))
	mm := got.(*Model)
	if mm.mode != modeHelp {
		t.Fatalf("mode = %v, want modeHelp", mm.mode)
	}
	if cmd != nil {
		t.Fatal("opening help should not start commands")
	}
	if mm.helpFrom != modeBrowse {
		t.Fatalf("helpFrom = %v, want modeBrowse", mm.helpFrom)
	}
}

func TestHelpOpensFromPicker(t *testing.T) {
	m := pickerModel()
	got, _ := m.Update(keyRunes("?"))
	if got.(*Model).mode != modeHelp {
		t.Fatalf("mode = %v, want modeHelp from picker", got.(*Model).mode)
	}
	if got.(*Model).helpFrom != modePicker {
		t.Fatalf("helpFrom = %v, want modePicker", got.(*Model).helpFrom)
	}
}

func TestHelpClosesBackToOrigin(t *testing.T) {
	for _, origin := range []mode{modeBrowse, modePicker} {
		var m *Model
		if origin == modeBrowse {
			m = browseModel()
		} else {
			m = pickerModel()
		}
		m.Update(keyRunes("?"))
		if m.mode != modeHelp {
			t.Fatalf("origin %v: did not open help", origin)
		}
		// esc, enter, and ? each close the modal back to the origin mode.
		for _, key := range []tea.KeyMsg{keyType(tea.KeyEsc), keyType(tea.KeyEnter), keyRunes("?")} {
			m = pickerModel()
			if origin == modeBrowse {
				m = browseModel()
			}
			m.Update(keyRunes("?"))
			got, _ := m.Update(key)
			mm := got.(*Model)
			if mm.mode != origin {
				t.Fatalf("origin %v: %v closed to %v, want %v", origin, key, mm.mode, origin)
			}
		}
	}
}

func TestHelpViewShowsKeybindsAndMouse(t *testing.T) {
	m := browseModel()
	m.Update(keyRunes("?"))
	v := m.View()
	for _, want := range []string{"KEYBINDS", "MOUSE", "↑↓←→ / hjkl", "double-click", "right-click", "wheel", "delete the selection"} {
		if !strings.Contains(v, want) {
			t.Fatalf("help modal missing %q; got:\n%s", want, v)
		}
	}
}

func TestHelpModalBorderless(t *testing.T) {
	m := browseModel()
	m.Update(keyRunes("?"))
	v := m.View()
	// The help reference is a borderless table: no box glyphs anywhere in it.
	for _, glyph := range []string{"┌", "┐", "└", "┘", "─"} {
		if strings.Contains(v, glyph) {
			t.Fatalf("help modal renders a border glyph %q; want borderless:\n%s", glyph, v)
		}
	}
}

func TestHelpModalShowsVimKeysButBarDoesNot(t *testing.T) {
	m := browseModel()
	if strings.Contains(m.helpLine(), "hjkl") {
		t.Fatal("bottom bar still shows vim keys")
	}
	m.Update(keyRunes("?"))
	if !strings.Contains(m.View(), "↑↓←→ / hjkl") {
		t.Fatal("help modal should document vim keys")
	}
}

func TestHelpGlobalFromPicker(t *testing.T) {
	m := pickerModel()
	m.Update(keyRunes("?"))
	v := m.View()
	// The help is global: picking from the first screen still shows the full
	// reference (the drill/rescan/delete bindings, mouse details).
	for _, want := range []string{"drill into a folder", "rescan the current root", "right-click", "delete the selection"} {
		if !strings.Contains(v, want) {
			t.Fatalf("help opened from the picker is missing the global reference %q; got:\n%s", want, v)
		}
	}
	// The picker chrome (info row + keybind bar) stays picker-flavored behind
	// the modal.
	if !strings.Contains(v, "↑↓←→") {
		t.Fatal("picker left its keybind bar behind the help modal")
	}
	if !strings.Contains(v, "free") || strings.Contains(v, "children") {
		t.Fatal("info row lost the picker flavour behind the help modal")
	}
}

func TestHelpBarNoDoubleClick(t *testing.T) {
	for _, m := range []*Model{browseModel(), pickerModel()} {
		if strings.Contains(m.helpLine(), "2×") {
			t.Fatal("help bar still advertises a double-click binding")
		}
	}
}

func TestHelpBarMentionsMouseAndQuestion(t *testing.T) {
	for _, m := range []*Model{browseModel(), pickerModel()} {
		bar := m.helpLine()
		if !strings.Contains(bar, "mouse supported") {
			t.Fatal("help bar does not mention mouse support")
		}
		if !strings.Contains(bar, "help") {
			t.Fatal("help bar does not advertise the ? binding")
		}
	}
}
