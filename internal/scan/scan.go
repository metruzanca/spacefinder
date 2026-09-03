// Package scan walks a directory tree and computes the recursive size of every
// node. A single Scan builds the whole tree so drill-downs in the TUI are
// instant; sizes are accumulated du-like from lstat, and symlinked directories
// are not followed (their lstat size counts as a single leaf) to avoid cycles.
package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
)

// Progress is emitted on the channel passed to Scan as the walk advances.
type Progress struct {
	// Visited is the number of filesystem entries inspected so far.
	Visited int
	// Errors is the number of entries that could not be read (permission
	// denied, vanished files, etc.). These do not fail the scan.
	Errors int
	// Current is the path currently being walked, for the splash screen.
	Current string
}

// Node is one directory or file in the scanned tree.
type Node struct {
	Name     string
	Path     string
	Size     int64
	IsDir    bool
	Children []*Node
}

// Scan walks root recursively, sending progress on ch (nil to skip). It returns
// the root node with recursive sizes populated. The context can be used to
// cancel a long scan; on cancellation Scan returns ctx.Err().
func Scan(ctx context.Context, root string, ch chan<- Progress) (*Node, error) {
	if ch != nil {
		defer close(ch)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	rootNode := &Node{Name: filepath.Base(root), Path: root, IsDir: info.IsDir()}
	report := func(p Progress) {
		if ch != nil {
			ch <- p
		}
	}
	errs := 0
	if err := scanRec(ctx, root, rootNode, &errs, report); err != nil {
		return nil, err
	}
	return rootNode, nil
}

func scanRec(ctx context.Context, path string, node *Node, errs *int, report func(Progress)) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		*errs++
		report(Progress{Errors: 1, Current: path})
		node.Size = sizeOrZero(path, errs, report)
		return nil
	}
	var total int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		full := filepath.Join(path, entry.Name())
		report(Progress{Current: full})
		child := &Node{Name: entry.Name(), Path: full, IsDir: entry.IsDir()}
		switch entry.Type() {
		case fs.ModeSymlink:
			// Size only; never follow links.
			child.Size = sizeOrZero(full, errs, report)
		case fs.ModeDir:
			if err := scanRec(ctx, full, child, errs, report); err != nil && err != context.Canceled {
				continue
			}
		default:
			child.Size = sizeOrZero(full, errs, report)
		}
		node.Children = append(node.Children, child)
		total += child.Size
		report(Progress{Visited: 1, Current: full})
	}
	node.Size = total
	return nil
}

// sizeOrZero returns the lstat size of path, or 0 when it cannot be stat'd
// (e.g. permission denied). Permission problems on individual entries never
// fail the scan; they are surfaced to the caller via the Progress channel.
func sizeOrZero(path string, errs *int, report func(Progress)) int64 {
	info, err := os.Lstat(path)
	if err != nil {
		*errs++
		report(Progress{Errors: 1, Current: path})
		return 0
	}
	return info.Size()
}
