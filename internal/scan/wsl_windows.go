//go:build windows

package scan

// WSL reports whether the running Linux kernel is a WSL kernel. It only ever
// applies to Linux builds; on Windows there is no WSL kernel to detect, so it
// always reports false.
func WSL() bool { return false }
