package tui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
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
