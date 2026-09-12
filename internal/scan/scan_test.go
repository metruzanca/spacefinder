package scan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// fileBlocks returns the du-style allocated size of path.
func fileBlocks(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return infoSize(info)
}

func TestMeasureTotals(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "12345")
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "123")
	writeFile(t, filepath.Join(root, "sub", "deep", "c.txt"), "12")

	sc, tree, err := Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}

	want := fileBlocks(t, filepath.Join(root, "a.txt")) +
		fileBlocks(t, filepath.Join(root, "sub", "b.txt")) +
		fileBlocks(t, filepath.Join(root, "sub", "deep", "c.txt"))
	// The directory entries' own blocks are not counted; only file content.
	if tree.Size != want {
		t.Fatalf("tree.Size = %d, want %d (allocated blocks)", tree.Size, want)
	}
	if got := sc.totals[filepath.Join(root, "sub")]; got != want-fileBlocks(t, filepath.Join(root, "a.txt")) {
		t.Fatalf("sub total = %d, want %d", got, want-fileBlocks(t, filepath.Join(root, "a.txt")))
	}

	// Root level is materialized; grandchildren are lazy.
	if len(tree.Children) != 2 { // a.txt + sub
		t.Fatalf("root children = %d, want 2", len(tree.Children))
	}
	var sub *Node
	for _, c := range tree.Children {
		if c.Name == "sub" {
			sub = c
		}
	}
	if sub == nil {
		t.Fatal("sub not in root children")
	}
	if sub.Children != nil {
		t.Fatal("sub should be thin (nil children) until expanded")
	}
}

func TestMeasureFileLeaf(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "f.txt")
	writeFile(t, f, "hello world")
	_, tree, err := Measure(context.Background(), f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree.IsDir {
		t.Fatal("file root should not be a dir")
	}
	if tree.Size != fileBlocks(t, f) {
		t.Fatalf("size = %d, want %d", tree.Size, fileBlocks(t, f))
	}
}

func TestMeasureMissingRoot(t *testing.T) {
	_, _, err := Measure(context.Background(), filepath.Join(t.TempDir(), "nope"), nil)
	if err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestMeasureCancel(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(dir, "sub", "f", string(rune(i+48)), "x"), "12345")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := Measure(ctx, dir, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Measure err = %v, want context.Canceled", err)
	}
}

func TestMeasureThrottledProgress(t *testing.T) {
	dir := t.TempDir()
	const n = 3000
	for i := 0; i < n; i++ {
		writeFile(t, filepath.Join(dir, "f"+string(rune(i/64+48)), "g"+string(rune(i%64+48))), "x")
	}
	ch := make(chan Progress, 16)
	done := make(chan error, 1)
	go func() {
		_, _, err := Measure(context.Background(), dir, ch)
		done <- err
	}()
	var events int
	var visited int
	for p := range ch {
		events++
		if p.Visited < visited {
			t.Fatalf("progress went backwards: %d -> %d", visited, p.Visited)
		}
		visited = p.Visited
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if events > n/8 {
		t.Fatalf("progress not throttled: %d events for %d entries", events, n)
	}
	dirs := (n-1)/64 + 1
	if visited != n+dirs {
		t.Fatalf("visited = %d, want %d (files + subdirs)", visited, n+dirs)
	}
}

// TestMeasureStreamsCompletedChildren verifies that Measure reports each direct
// child of the scan root as its total becomes known, in exploration order
// (readdir order, directories when their subtree walk returns, files as they
// are visited), with their real du-style sizes. Nested directories are never
// reported — only the scan root's own children.
func TestMeasureStreamsCompletedChildren(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "adir", "nested", "deep.txt"), "12345")
	writeFile(t, filepath.Join(root, "afile.txt"), "123")
	writeFile(t, filepath.Join(root, "zdir", "b.txt"), "12")

	ch := make(chan Progress, 64)
	done := make(chan error, 1)
	go func() {
		_, _, err := Measure(context.Background(), root, ch)
		done <- err
	}()
	var completed []ChildSize
	for p := range ch {
		completed = append(completed, p.Completed...)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	want := []ChildSize{
		{Name: "adir", Size: fileBlocks(t, filepath.Join(root, "adir", "nested", "deep.txt"))},
		{Name: "afile.txt", Size: fileBlocks(t, filepath.Join(root, "afile.txt"))},
		{Name: "zdir", Size: fileBlocks(t, filepath.Join(root, "zdir", "b.txt"))},
	}
	if len(completed) != len(want) {
		t.Fatalf("completed = %d children, want %d (%+v)", len(completed), len(want), completed)
	}
	for i, w := range want {
		if completed[i].Name != w.Name {
			t.Fatalf("completed[%d].Name = %q, want %q", i, completed[i].Name, w.Name)
		}
		if completed[i].Size != w.Size {
			t.Fatalf("completed[%d] (%s).Size = %d, want %d", i, w.Name, completed[i].Size, w.Size)
		}
	}
}

// TestMeasureCompletedOrderMatchesTree verifies the reported children agree
// with the materialized root tree (same names and sizes), so the animation
// always lands on exactly what the browse view will show.
func TestMeasureCompletedOrderMatchesTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "f1"), "111111111")
	writeFile(t, filepath.Join(root, "b", "g2"), "22")
	writeFile(t, filepath.Join(root, "c.txt"), "3333333333")

	ch := make(chan Progress, 64)
	done := make(chan error, 1)
	go func() {
		_, _, err := Measure(context.Background(), root, ch)
		done <- err
	}()
	var byName = map[string]int64{}
	for p := range ch {
		for _, c := range p.Completed {
			byName[c.Name] = c.Size
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_, tree, err := Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Children) != len(byName) {
		t.Fatalf("streamed %d children, tree has %d", len(byName), len(tree.Children))
	}
	for _, c := range tree.Children {
		if got, ok := byName[c.Name]; !ok || got != c.Size {
			t.Fatalf("streamed %s = %d (present: %v), tree has %d", c.Name, got, ok, c.Size)
		}
	}
}

func TestHardlinkDedup(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.bin")
	writeFile(t, a, "some bigger content for a multi-block file")
	b := filepath.Join(root, "b.bin")
	if err := os.Link(a, b); err != nil {
		t.Skipf("cannot create hardlink: %v", err)
	}
	c := filepath.Join(root, "c.bin")
	writeFile(t, c, "an independent file")

	_, tree, err := Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := fileBlocks(t, a) + fileBlocks(t, c) // b shares a's inode
	if tree.Size != want {
		t.Fatalf("root.Size = %d, want %d (hardlink counted once)", tree.Size, want)
	}
}

func TestLazyExpandAndIdempotent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "top.txt"), "top")
	writeFile(t, filepath.Join(root, "sub", "x.txt"), "x")
	writeFile(t, filepath.Join(root, "sub", "y.txt"), "y")

	sc, tree, err := Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sub *Node
	for _, c := range tree.Children {
		if c.Name == "sub" {
			sub = c
		}
	}
	if err := sc.Expand(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	if sub.Children == nil {
		t.Fatal("sub should now be expanded")
	}
	if len(sub.Children) != 2 {
		t.Fatalf("sub children = %d, want 2", len(sub.Children))
	}
	want := fileBlocks(t, filepath.Join(root, "sub", "x.txt")) +
		fileBlocks(t, filepath.Join(root, "sub", "y.txt"))
	if sub.Size != want {
		t.Fatalf("sub.Size = %d, want %d", sub.Size, want)
	}
	for _, c := range sub.Children {
		if c.Parent != sub {
			t.Fatal("child parent not wired")
		}
	}

	// Expanding again is a no-op.
	before := len(sub.Children)
	if err := sc.Expand(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	if len(sub.Children) != before {
		t.Fatal("Expand is not idempotent")
	}
}

func TestExpandFallbackAfterForget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "dumb", "keep.txt"), "k")
	sc, tree, err := Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var d *Node
	for _, c := range tree.Children {
		if c.Name == "dumb" {
			d = c
		}
	}
	old := d.Size

	// Content appears after the pass (as after a deletion elsewhere): forget
	// the recorded total and expand again — it must re-measure on the spot.
	writeFile(t, filepath.Join(d.Path, "new.txt"), "new content")
	sc.Forget(d.Path)
	if err := sc.Expand(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if d.Size <= old {
		t.Fatalf("d.Size = %d after new file, want > %d", d.Size, old)
	}
	if d.Size == old {
		t.Fatal("new file not reflected")
	}
}

func TestThrottleNilReceiver(t *testing.T) {
	// totalOrMeasure drives on-demand re-measures with a nil throttle; every
	// throttle method must tolerate that.
	var th *throttle
	th.report(1, 0, "/x")
	th.report(0, 1, "/y")
	th.flush()
}

// TestExpandReMeasuresMissingSubdir ensures Expand re-measures a directory
// whose total is missing (e.g. it changed after the pass) without panicking.
// This previously panicked with a nil-pointer dereference because the
// on-demand measure ran with a nil throttle.
func TestExpandReMeasuresMissingSubdir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "play", "sub", "z.txt"), "z")
	sc, tree, err := Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var play *Node
	for _, c := range tree.Children {
		if c.Name == "play" {
			play = c
		}
	}
	if play == nil {
		t.Fatal("play not in children")
	}

	// The directory and its subdir are now stale: forget both totals, add
	// content, and expand. The subdir must be re-measured on the spot.
	sc.Forget(play.Path)
	sc.Forget(filepath.Join(play.Path, "sub"))
	writeFile(t, filepath.Join(play.Path, "sub", "w.txt"), "w")
	if err := sc.Expand(context.Background(), play); err != nil {
		t.Fatal(err)
	}
	if len(play.Children) != 1 || play.Children[0].Name != "sub" {
		t.Fatalf("children = %#v, want [sub]", play.Children)
	}
	sub := play.Children[0]
	want := fileBlocks(t, filepath.Join(sub.Path, "z.txt")) + fileBlocks(t, filepath.Join(sub.Path, "w.txt"))
	if sub.Size != want {
		t.Fatalf("sub.Size = %d, want %d (re-measured on the spot)", sub.Size, want)
	}
	// sub stays thin until expanded; its recorded total was refreshed.
	if err := sc.Expand(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	if len(sub.Children) != 2 {
		t.Fatalf("sub children = %d, want 2", len(sub.Children))
	}
}

func TestImplausibleSize(t *testing.T) {
	ok := []int64{0, 1, 4096, maxSaneSize}
	for _, n := range ok {
		if implausibleSize(n) {
			t.Errorf("implausibleSize(%d) = true, want false", n)
		}
	}
	bad := []int64{-1, maxSaneSize + 1, int64(7000) << 50, 1 << 62}
	for _, n := range bad {
		if !implausibleSize(n) {
			t.Errorf("implausibleSize(%d) = false, want true", n)
		}
	}
}

func TestSystemDirName(t *testing.T) {
	for _, name := range []string{"Windows", "windows", "SYSTEM Volume Information", "Program Files (x86)", "$Recycle.Bin", "ProgramData"} {
		if !systemDirName(name) {
			t.Errorf("systemDirName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"Users", "projects", "Documents", "win", "Sysadmin"} {
		if systemDirName(name) {
			t.Errorf("systemDirName(%q) = true, want false", name)
		}
	}
}

func TestSkipSystemDir(t *testing.T) {
	t.Setenv("SPACEFINDER_NO_SKIP", "")
	cases := []struct {
		wsl      bool
		parent   string
		name     string
		wantSkip bool
	}{
		{wsl: true, parent: "/mnt/c", name: "Windows", wantSkip: true},
		{wsl: true, parent: "/mnt/c", name: "windows", wantSkip: true},
		{wsl: true, parent: "/mnt/c", name: "$Recycle.Bin", wantSkip: true},
		{wsl: true, parent: "/mnt/d/Users/me", name: "Windows", wantSkip: true},
		{wsl: true, parent: "/mnt/c", name: "Users", wantSkip: false},
		{wsl: false, parent: "/mnt/c", name: "Windows", wantSkip: false},
		{wsl: true, parent: "/", name: "Windows", wantSkip: false},
		{wsl: true, parent: "/home/me", name: "Windows", wantSkip: false},
	}
	for _, c := range cases {
		if got := skipSystemDir(c.wsl, c.parent, c.name); got != c.wantSkip {
			t.Errorf("skipSystemDir(wsl=%v, parent=%q, name=%q) = %v, want %v", c.wsl, c.parent, c.name, got, c.wantSkip)
		}
	}

	// The env kill-switch opts out even on a WSL /mnt path.
	t.Setenv("SPACEFINDER_NO_SKIP", "1")
	if skipSystemDir(true, "/mnt/c", "Windows") {
		t.Fatal("skipSystemDir honored SPACEFINDER_NO_SKIP=1")
	}
}

func TestMeasureIgnoresMountLikeDirs(t *testing.T) {
	// A directory whose st_dev differs from its parent is treated as a leaf
	// (du -x). We cannot mount in unprivileged tests, but the same-deviceness
	// guard must at least keep ordinary trees intact.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "b"), "x")
	_, tree, err := Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Size != fileBlocks(t, filepath.Join(root, "a", "b")) {
		t.Fatalf("size = %d, want %d", tree.Size, fileBlocks(t, filepath.Join(root, "a", "b")))
	}
}

// unreadableDir chmods dir to 0o000 so a readdir fails, and restores it on
// cleanup. It reports false when running as root, where permission bits are
// bypassed and the test would be meaningless.
func unreadableDir(t *testing.T, dir string) bool {
	t.Helper()
	if os.Geteuid() == 0 {
		return false
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	return true
}

func TestMeasureCollectsErrors(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ok.txt"), "x")
	secret := filepath.Join(root, "secret")
	if err := os.Mkdir(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(secret, "locked.txt"), "y")
	if !unreadableDir(t, secret) {
		t.Skip("running as root; permission checks are bypassed")
	}

	sc, _, err := Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sc.TotalErrors() != 1 {
		t.Fatalf("TotalErrors = %d, want 1", sc.TotalErrors())
	}
	errs := sc.Errors()
	if len(errs) != 1 {
		t.Fatalf("Errors = %d entries, want 1", len(errs))
	}
	if errs[0].Path != secret {
		t.Fatalf("error path = %q, want %q", errs[0].Path, secret)
	}
	if errs[0].Err == nil {
		t.Fatal("error reason is nil")
	}
}

func TestExpandKeepsErrors(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "sub", "x.txt"), "x")
	secret := filepath.Join(root, "secret")
	if err := os.Mkdir(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(secret, "locked.txt"), "y")
	if !unreadableDir(t, secret) {
		t.Skip("running as root; permission checks are bypassed")
	}

	sc, tree, err := Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sc.TotalErrors() != 1 {
		t.Fatalf("TotalErrors after measure = %d, want 1", sc.TotalErrors())
	}
	// A later expand must not clear the recorded errors.
	var sub *Node
	for _, c := range tree.Children {
		if c.Name == "sub" {
			sub = c
		}
	}
	if err := sc.Expand(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	if sc.TotalErrors() != 1 {
		t.Fatalf("TotalErrors after expand = %d, want 1", sc.TotalErrors())
	}
	found := false
	for _, e := range sc.Errors() {
		if e.Path == secret {
			found = true
		}
	}
	if !found {
		t.Fatalf("error for %q not recorded; got %#v", secret, sc.Errors())
	}
}

func TestErrorsSampleCapped(t *testing.T) {
	root := t.TempDir()
	// Many sibling unreadable dirs each contribute one readdir error, far
	// exceeding the display cap while the true total is preserved.
	const n = maxStoredErrors + 20
	unreadable := make([]string, 0, n)
	for i := 0; i < n; i++ {
		d := filepath.Join(root, fmt.Sprintf("secret%d", i))
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if !unreadableDir(t, d) {
			t.Skip("running as root; permission checks are bypassed")
		}
		unreadable = append(unreadable, d)
	}

	sc, _, err := Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(sc.Errors()); got != maxStoredErrors {
		t.Fatalf("stored %d errors, want the cap %d", got, maxStoredErrors)
	}
	if sc.TotalErrors() != n {
		t.Fatalf("TotalErrors = %d, want %d", sc.TotalErrors(), n)
	}
}
