// Package scan measures a directory tree du-style: it walks the tree once to
// record the total size of every directory (allocated blocks, single
// filesystem, hardlinks counted once), and materializes the tree lazily one
// level at a time as the UI drills into it.
//
// The initial Measure pass costs roughly what `du -x -B1 --max-depth=1` costs:
// you get real totals for every direct child, but grandchildren are not turned
// into objects until Expand is called for their parent. Expanding a directory
// is just one readdir plus a lookup in the recorded totals, so drilling down
// is effectively instant.
package scan

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/metruzanca/spacefinder/internal/logging"
)

// Progress is a throttle-sampled snapshot emitted while measuring. The counts
// are cumulative.
type Progress struct {
	// Visited is the number of filesystem entries inspected so far.
	Visited int
	// Errors is the number of entries that could not be read (permission
	// denied, vanished files, etc.). These do not fail the measure.
	Errors int
	// Current is the path currently being measured, for the splash screen.
	Current string
	// Completed lists the scan root's direct children whose totals became
	// known since the previous event, in exploration order. The TUI animates
	// the tree building out from these. Only populated for the top-level pass.
	Completed []ChildSize
}

// ChildSize is one direct child of the scan root whose total size is now
// known: its name and its measured du-style byte count.
type ChildSize struct {
	Name string
	Size int64
}

// Node is one directory or file. Directories are thin: Children stays nil
// until Expand materializes the next level.
type Node struct {
	Name     string
	Path     string
	Size     int64
	IsDir    bool
	Approx   bool // Size is an estimate, not a measured total (excluded subtree)
	Parent   *Node
	Children []*Node // nil until the directory has been expanded
}

// Scanner carries the results of a measure pass: the total size of every
// directory visited, plus hardlink dedup state. It is the handle used to
// expand directories lazily.
type Scanner struct {
	totals map[string]int64
	approx map[string]bool // paths whose subtrees were skipped, sized approximately
	seen   map[fileID]struct{}

	errors   []ScanError
	errTotal int
}

// ScanError records one filesystem entry that could not be read during a
// measure pass or a later expand. These are tolerated (they never fail a
// scan) but are surfaced so the user can see what was skipped.
type ScanError struct {
	Path string
	Err  error
}

// maxStoredErrors caps how many error paths are retained for display. The
// true total is always counted even when the sample is truncated.
const maxStoredErrors = 100

// recordError notes a path that could not be read.
func (s *Scanner) recordError(path string, err error) {
	s.errTotal++
	logging.Errorf("scan error path=%q err=%v", path, err)
	if len(s.errors) < maxStoredErrors {
		s.errors = append(s.errors, ScanError{Path: path, Err: err})
	}
}

// Errors returns the sampled unreadable paths (capped at maxStoredErrors) in
// the order they were hit; TotalErrors reports the true count.
func (s *Scanner) Errors() []ScanError {
	return s.errors
}

// TotalErrors is the number of entries that could not be read across the
// measure pass and any expands, regardless of the stored sample size.
func (s *Scanner) TotalErrors() int {
	return s.errTotal
}

// fileID identifies an inode for hardlink dedup.
type fileID struct {
	dev uint64
	ino uint64
}

// maxSaneSize is the largest du-style size a single entry can plausibly
// report (~1 PiB). Larger values come from bogus stat results (e.g. drvfs/9p
// stats on guarded Windows entries) and are dropped so they cannot inflate
// every ancestor's total up to the scan root.
const maxSaneSize = 1 << 50

// implausibleSize reports whether a du-style byte count cannot be real.
func implausibleSize(n int64) bool {
	return n < 0 || n > maxSaneSize
}

// systemDirNames are Windows system folders whose content is host-managed,
// read-only, or not user data. On WSL the Windows drive's version of these is
// expensive to walk (a slow 9p round-trip per entry) and mostly unreadable,
// so their subtrees are skipped by default and shown with an approximate size.
// Matched case-insensitively.
var systemDirNames = map[string]bool{
	"$recycle.bin":              true,
	"system volume information": true,
	"windows":                   true,
	"program files":             true,
	"program files (x86)":       true,
	"programdata":               true,
	"perflogs":                  true,
	"recovery":                  true,
	"documents and settings":    true,
	"application data":          true,
	"local settings":            true,
}

// systemDirName reports whether name is a known Windows system folder
// (case-insensitive).
func systemDirName(name string) bool {
	return systemDirNames[strings.ToLower(name)]
}

// systemSkipDisabled reports whether the user opted out of the default
// Windows system-folder skip entirely.
func systemSkipDisabled() bool {
	return os.Getenv("SPACEFINDER_NO_SKIP") != ""
}

// skipSystemDir reports whether a directory subtree should be skipped: its
// name matches a known Windows system folder and it lives under a /mnt drive
// mount on WSL. The mount-prefix gate keeps the name heuristic from touching
// ordinary Linux trees that happen to contain such a folder.
func skipSystemDir(wsl bool, parentDir, name string) bool {
	if !wsl || systemSkipDisabled() {
		return false
	}
	if !systemDirName(name) {
		return false
	}
	return strings.HasPrefix(parentDir, "/mnt/") ||
		strings.HasPrefix(filepath.Join(parentDir, name), "/mnt/")
}

// Measure walks root once, recording the total size of every directory, and
// materializes root's immediate children into the returned tree. Progress is
// sent (throttled) on ch when ch is non-nil; ch is closed before Measure
// returns.
func Measure(ctx context.Context, root string, ch chan<- Progress) (*Scanner, *Node, error) {
	if ch != nil {
		defer close(ch)
	}
	start := time.Now()
	logging.Debugf("measure starting: root=%q", root)
	info, err := os.Lstat(root)
	if err != nil {
		logging.Errorf("measure failed to stat root=%q: %v", root, err)
		return nil, nil, err
	}
	s := &Scanner{
		totals: map[string]int64{},
		approx: map[string]bool{},
		seen:   map[fileID]struct{}{},
	}
	rootNode := &Node{Name: filepath.Base(root), Path: filepath.Clean(root), IsDir: info.IsDir()}
	th := &throttle{ch: ch, last: time.Now(), root: rootNode.Path}
	if info.IsDir() {
		if err := s.measureDir(ctx, rootNode.Path, info, infoDev(info), th); err != nil {
			logging.Errorf("measure failed walking root=%q: %v", root, err)
			return nil, nil, err
		}
		rootNode.Size = s.totals[rootNode.Path]
	} else {
		rootNode.Size = infoSize(info)
	}
	th.flush()
	if err := s.Expand(ctx, rootNode); err != nil {
		logging.Errorf("measure failed expanding root=%q: %v", root, err)
		return nil, nil, err
	}
	logging.Debugf("measure complete: root=%q size=%d errors=%d took=%s", root, rootNode.Size, s.errTotal, time.Since(start))
	return s, rootNode, nil
}

// Expand materializes node's immediate children: one readdir, one lstat per
// entry, and each subdirectory's total taken from the recorded measure. Any
// directory whose total is missing (created or changed since the pass) is
// measured on the spot so sizes stay correct. Expand is idempotent.
func (s *Scanner) Expand(ctx context.Context, node *Node) error {
	if node == nil || !node.IsDir || node.Children != nil {
		return nil
	}
	start := time.Now()
	logging.Debugf("expand starting: dir=%q", node.Path)
	entries, err := os.ReadDir(node.Path)
	if err != nil {
		s.recordError(node.Path, err)
		node.Children = []*Node{} // non-nil marks the directory as expanded
		logging.Debugf("expand yielded no entries (unreadable): dir=%q", node.Path)
		return nil
	}
	// Device of node itself, for the same mount-point check the walk applies:
	// a mount point is a du -x leaf — its own entry size — never re-walked.
	parentDev := uint64(0)
	if info, err := os.Lstat(node.Path); err == nil {
		parentDev = infoDev(info)
	}
	children := make([]*Node, 0, len(entries))
	var total int64
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		stat, serr := e.Info()
		if serr != nil {
			continue
		}
		full := filepath.Join(node.Path, e.Name())
		approx := s.approx[full]
		child := &Node{Name: e.Name(), Path: full, IsDir: stat.IsDir(), Approx: approx, Parent: node}
		if stat.IsDir() {
			if approx {
				// Excluded in the measure pass (system dir): keep its own-entry
				// size and never re-walk it lazily.
				child.Size = infoSize(stat)
			} else if dev := infoDev(stat); dev != 0 && dev != parentDev {
				child.Size = infoSize(stat)
			} else {
				child.Size = s.totalOrMeasure(ctx, full, stat)
			}
		} else {
			child.Size = infoSize(stat)
		}
		if implausibleSize(child.Size) {
			s.recordError(full, fmt.Errorf("implausible du size %d bytes", child.Size))
			child.Size = 0
		}
		total += child.Size
		children = append(children, child)
	}
	node.Children = children
	// Keep the du-consistent recorded total when available (it accounts for
	// hardlink dedup); otherwise the directory was re-measured on the spot
	// after a change, so derive its size from the fresh children.
	if tot, ok := s.totals[node.Path]; ok {
		node.Size = tot
	} else {
		node.Size = total
	}
	logging.Debugf("expand complete: dir=%q entries=%d size=%d took=%s", node.Path, len(node.Children), node.Size, time.Since(start))
	return nil
}

// Forget drops the recorded total for path, so the next Expand of that
// directory re-measures it on the spot. Called after a deletion.
func (s *Scanner) Forget(path string) {
	delete(s.totals, path)
}

// totalOrMeasure returns the recorded total for a directory, or measures it
// now if it was not part of the original pass.
func (s *Scanner) totalOrMeasure(ctx context.Context, path string, info fs.FileInfo) int64 {
	if tot, ok := s.totals[path]; ok {
		return tot
	}
	if err := s.measureDir(ctx, path, info, infoDev(info), nil); err != nil {
		return 0
	}
	return s.totals[path]
}

// measureDir walks a directory du-style and records its total size. It does
// not descend into mount points (single-filesystem semantics) and counts each
// hardlink only once. Errors on individual entries are tolerated.
func (s *Scanner) measureDir(ctx context.Context, path string, info fs.FileInfo, parentDev uint64, th *throttle) error {
	total, err := s.measureEntries(ctx, path, parentDev, th)
	if err != nil {
		return err
	}
	// A directory that cannot be read sizes as its own entry.
	if total < 0 {
		total = infoSize(info)
	}
	if implausibleSize(total) {
		total = 0
	}
	s.totals[path] = total
	return nil
}

// measureEntries reads path and returns the sum of its entries. A negative
// return signals the directory itself could not be read.
func (s *Scanner) measureEntries(ctx context.Context, path string, parentDev uint64, th *throttle) (int64, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		s.recordError(path, err)
		th.report(0, 1, path)
		return -1, nil
	}
	var total int64
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		full := filepath.Join(path, e.Name())
		stat, serr := e.Info()
		if serr != nil {
			s.recordError(full, serr)
			th.report(0, 1, full)
			continue
		}
		mode := stat.Mode()
		var childSize int64
		var isDir bool
		switch {
		case mode&fs.ModeSymlink != 0:
			// Never follow links; count the link itself.
			childSize = infoSize(stat)
		case stat.IsDir():
			isDir = true
			if skipSystemDir(WSL(), path, e.Name()) {
				// Host-managed Windows tree (System32, ProgramData, ...):
				// descending costs many slow 9p round-trips to reach files the
				// user cannot read or delete anyway. Size it approximately.
				childSize = infoSize(stat)
				s.approx[full] = true
				logging.Debugf("skipping Windows system dir (approx): %q", full)
				break
			}
			dev := infoDev(stat)
			if dev != 0 && dev != parentDev {
				// Different filesystem: treat as a leaf (du -x).
				childSize = infoSize(stat)
			} else if err := s.measureDir(ctx, full, stat, dev, th); err != nil {
				return 0, err
			} else {
				childSize = s.totals[full]
			}
		default:
			childSize = s.dedupSize(stat)
		}
		if implausibleSize(childSize) {
			s.recordError(full, fmt.Errorf("implausible du size %d bytes", childSize))
			th.report(0, 1, full)
			childSize = 0
		}
		total += childSize
		// A direct child of the scan root is "explored" the moment its size is
		// known: files immediately, directories when their subtree walk
		// returns. Report it so the TUI can grow the treemap block by block.
		if th != nil && path == th.root {
			th.reportChild(e.Name(), childSize, isDir)
		}
		th.report(1, 0, full)
	}
	return total, nil
}

// dedupSize counts a regular file's allocated size, skipping any hardlink
// whose inode was already counted in this measure session.
func (s *Scanner) dedupSize(info fs.FileInfo) int64 {
	if n := infoNlink(info); n > 1 {
		id := fileID{dev: infoDev(info), ino: infoIno(info)}
		if _, ok := s.seen[id]; ok {
			return 0
		}
		s.seen[id] = struct{}{}
	}
	return infoSize(info)
}

// throttle batches progress events so a fast walk does not flood the UI with
// one message (and one repaint) per entry.
type throttle struct {
	ch   chan<- Progress
	last time.Time
	root string // scan root; only its direct children are reported as completed

	curVisited, curErrs int
	lastVisited         int
	current             string
	pending             []ChildSize // completed root children since the last flush
}

func (t *throttle) report(visited, errs int, current string) {
	if t == nil {
		return
	}
	t.curVisited += visited
	t.curErrs += errs
	if current != "" {
		t.current = current
	}
	if time.Since(t.last) >= 50*time.Millisecond || t.curVisited-t.lastVisited >= 2048 {
		t.flush()
	}
}

// reportChild notes a direct root child whose size is now known. Directory
// completions flush immediately so the UI animates each "new block appears"
// moment; small entries (files, symlinks) ride the regular throttle window so
// a root full of files does not flood the channel.
func (t *throttle) reportChild(name string, size int64, dir bool) {
	if t == nil {
		return
	}
	t.pending = append(t.pending, ChildSize{Name: name, Size: size})
	if dir {
		t.flush()
	}
}

func (t *throttle) flush() {
	if t == nil || t.ch == nil {
		return
	}
	t.ch <- Progress{Visited: t.curVisited, Errors: t.curErrs, Current: t.current, Completed: t.pending}
	t.pending = nil
	t.last = time.Now()
	t.lastVisited = t.curVisited
}
