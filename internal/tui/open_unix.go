//go:build !windows

package tui

import (
	"os/exec"
	"runtime"
)

// openDefault returns a command that opens path with the OS default
// application: `open` on macOS, `xdg-open` elsewhere.
func openDefault(path string) *exec.Cmd {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path)
	}
	return exec.Command("xdg-open", path)
}