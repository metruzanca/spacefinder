//go:build windows

package tui

// duSize has no portable equivalent on windows; the picker keeps showing the
// current directory's free space until it is actually scanned.
func duSize(path string) int64 { return 0 }
