//go:build !windows

package scan

import (
	"os"
	"strings"
	"sync"
)

// wslVersionFile is the path read to detect a WSL kernel. It is a package
// variable so tests can point it at a fixture.
var wslVersionFile = "/proc/version"

var (
	wslOnce sync.Once
	wslBool bool
)

// WSL reports whether the running Linux kernel is a WSL (Windows Subsystem for
// Linux) kernel, detected by the "microsoft" marker in /proc/version. On WSL
// the root filesystem is a thin-provisioned VHDX whose statfs free space
// reflects the host Windows disk, not the virtual disk, so free-space figures
// are suppressed by Free and Mounts.
func WSL() bool {
	wslOnce.Do(func() {
		b, err := os.ReadFile(wslVersionFile)
		if err != nil {
			return
		}
		v := strings.ToLower(string(b))
		wslBool = strings.Contains(v, "microsoft") || strings.Contains(v, "wsl")
	})
	return wslBool
}
