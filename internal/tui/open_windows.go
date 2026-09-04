//go:build windows

package tui

import "os/exec"

// openDefault returns a command that opens path with the OS default
// application.
func openDefault(path string) *exec.Cmd {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
}