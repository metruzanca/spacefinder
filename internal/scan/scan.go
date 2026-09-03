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
	"io/fs"
	"os"
	"path/filepath"
	"time"
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
}

// Node is one directory or file. Directories are thin: Children stays nil
// until Expand materializes the next level.
type Node struct {
	Name     string
	Path     string
	Size     int64
	IsDir    bool
	Parent   *Node
	Children []*Node // nil until the directory has been expanded
}

// Scanner carries the results of a measure pass: the total size of every
// directory visited, plus hardlink dedup state. It is the handle used to
// expand directories lazily.
type Scanner struct {
	totals map[string]int64
	seen   map[fileID]struct{}
}

// fileID identifies an inode for hardlink dedup.
type fileID struct {
	dev uint64
	ino uint64
}

// Measure walks root once, recording the total size of every directory, and
// materializes root's immediate children into the returned tree. Progress is
// sent (throttled) on ch when ch is non-nil; ch is closed before Measure
// returns.
func Measure(ctx context.Context, root string, ch chan<- Progress) (*Scanner, *Node, error) {
	if ch != nil {
		defer close(ch)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, nil, err
	}
	s := &Scanner{
		totals: map[string]int64{},
		seen:   map[fileID]struct{}{},
	}
	rootNode := &Node{Name: filepath.Base(root), Path: filepath.Clean(root), IsDir: info.IsDir()}
	th := &throttle{ch: ch, last: time.Now()}
	if info.IsDir() {
		if err := s.measureDir(ctx, rootNode.Path, info, infoDev(info), th); err != nil {
			return nil, nil, err
		}
		rootNode.Size = s.totals[rootNode.Path]
	} else {
		rootNode.Size = infoSize(info)
	}
	th.flush()
	if err := s.Expand(ctx, rootNode); err != nil {
		return nil, nil, err
	}
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
	entries, err := os.ReadDir(node.Path)
	if err != nil {
		node.Children = []*Node{} // non-nil marks the directory as expanded
		return nil
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
		child := &Node{Name: e.Name(), Path: full, IsDir: stat.IsDir(), Parent: node}
		if stat.IsDir() {
			child.Size = s.totalOrMeasure(ctx, full, stat)
		} else {
			child.Size = infoSize(stat)
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
	s.totals[path] = total
	return nil
}

// measureEntries reads path and returns the sum of its entries. A negative
// return signals the directory itself could not be read.
func (s *Scanner) measureEntries(ctx context.Context, path string, parentDev uint64, th *throttle) (int64, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
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
			th.report(0, 1, full)
			continue
		}
		mode := stat.Mode()
		switch {
		case mode&fs.ModeSymlink != 0:
			// Never follow links; count the link itself.
			total += infoSize(stat)
		case stat.IsDir():
			dev := infoDev(stat)
			if dev != 0 && dev != parentDev {
				// Different filesystem: treat as a leaf (du -x).
				total += infoSize(stat)
			} else if err := s.measureDir(ctx, full, stat, dev, th); err != nil {
				return 0, err
			} else {
				total += s.totals[full]
			}
		default:
			total += s.dedupSize(stat)
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

	curVisited, curErrs int
	lastVisited         int
	current             string
}

func (t *throttle) report(visited, errs int, current string) {
	t.curVisited += visited
	t.curErrs += errs
	if current != "" {
		t.current = current
	}
	if time.Since(t.last) >= 50*time.Millisecond || t.curVisited-t.lastVisited >= 2048 {
		t.flush()
	}
}

func (t *throttle) flush() {
	if t == nil || t.ch == nil {
		return
	}
	t.ch <- Progress{Visited: t.curVisited, Errors: t.curErrs, Current: t.current}
	t.last = time.Now()
	t.lastVisited = t.curVisited
}
