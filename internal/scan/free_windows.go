//go:build windows

package scan

// Free is unavailable on windows without extra syscall plumbing; report no
// free-space info so the TUI simply omits the free gutter.
func Free(path string) (int64, error) { return 0, nil }
