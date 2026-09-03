package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanSumsRecursive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "12345")
	writeFile(t, filepath.Join(dir, "sub", "b.txt"), "123")
	writeFile(t, filepath.Join(dir, "sub", "deep", "c.txt"), "12")

	root, err := Scan(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if root.Size != 5+3+2 {
		t.Fatalf("root.Size = %d, want %d", root.Size, 5+3+2)
	}
	if len(root.Children) != 2 { // a.txt + sub
		t.Fatalf("root children = %d, want 2", len(root.Children))
	}
}

func TestScanIgnoresSymlinkDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privileges on windows")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real", "a.txt"), "1234567890")
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	linkInfo, err := os.Lstat(filepath.Join(dir, "link"))
	if err != nil {
		t.Fatal(err)
	}

	root, err := Scan(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range root.Children {
		if c.Name == "link" {
			// The symlink must be a leaf (not expanded into "real"'s children).
			if c.IsDir {
				t.Fatal("symlinked dir should not be marked IsDir")
			}
			if len(c.Children) != 0 {
				t.Fatalf("symlinked dir leaked %d children", len(c.Children))
			}
		}
	}
	// real's contents counted once, plus the symlink's own lstat size.
	if root.Size != 10+linkInfo.Size() {
		t.Fatalf("root.Size = %d, want real(10) + link(%d)", root.Size, linkInfo.Size())
	}
}

func TestScanMissingRoot(t *testing.T) {
	_, err := Scan(context.Background(), filepath.Join(t.TempDir(), "nope"), nil)
	if err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestScanContextCancel(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(dir, "sub", "f"+string(rune(i+48)), "x"), "12345")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Scan(ctx, dir, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan err = %v, want context.Canceled", err)
	}
}

func TestScanProgressReported(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a"), "1")
	writeFile(t, filepath.Join(dir, "b"), "22")

	ch := make(chan Progress, 100)
	done := make(chan error, 1)
	go func() {
		_, err := Scan(context.Background(), dir, ch)
		done <- err
	}()
	var visited int
	for p := range ch {
		visited += p.Visited
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if visited < 2 {
		t.Fatalf("visited = %d, want >= 2 entries", visited)
	}
}
