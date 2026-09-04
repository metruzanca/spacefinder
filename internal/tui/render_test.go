package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/metruzanca/spacefinder/internal/scan"
)

var update = flag.Bool("update", false, "update golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func TestViewBrowseGolden(t *testing.T) {
	m := browseModel()
	golden(t, "browse_root", m.View())
}

func TestViewSplashNoPanic(t *testing.T) {
	m := newModel("/")
	m.width, m.height = 80, 24
	if m.View() == "" {
		t.Fatal("splash view empty")
	}
}

func TestViewConfirmGolden(t *testing.T) {
	m := browseModel()
	m.openConfirm()
	golden(t, "confirm_modal", m.View())
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
		{5 * 1024 * 1024 * 1024 * 1024, "5.0 TB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.in); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// fixtureModel builds a browsable model over the games/videos/projects/misc
// fixture the user used to describe the squarified squares.
func fixtureModel(w, h int) *Model {
	m := newModel("/")
	m.mode = modeBrowse
	m.width, m.height = w, h
	m.tree = &scan.Node{Name: "fs", Path: "/", IsDir: true, Children: []*scan.Node{
		{Name: "games", Path: "/games", IsDir: true, Size: 50},
		{Name: "videos", Path: "/videos", IsDir: true, Size: 30},
		{Name: "projects", Path: "/projects", IsDir: true, Size: 15},
		{Name: "misc", Path: "/misc", IsDir: true, Size: 5},
	}}
	m.current = m.tree
	m.buildLayout()
	return m
}

// TestSquaresFixtureGolden is the byte-exact regression for the renaming
// symptom the user reported ("just edges of squares"): tile interiors must be
// solid blocks, not empty space.
func TestSquaresFixtureGolden(t *testing.T) {
	golden(t, "squares_4", fixtureModel(44, 22).View())
}

// TestNoColorStillShowsBlocks reproduces the user's environment (NO_COLOR=1
// pushes termenv to the Ascii profile, dropping background fills) and asserts
// the treemap still renders as solid squares rather than a wireframe.
func TestNoColorStillShowsBlocks(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	v := fixtureModel(44, 22).View()
	if n := strings.Count(v, "█"); n < 100 {
		t.Fatalf("interior blocks missing under NO_COLOR: found %d █ runes", n)
	}
	if strings.Count(v, "█") <= strings.Count(v, "└") {
		t.Fatal("treemap is still mostly edges; interiors are not filled")
	}
}
