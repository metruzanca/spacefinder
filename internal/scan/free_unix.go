//go:build !windows

package scan

import "syscall"

// Free returns the bytes available to the caller on the filesystem containing
// path (statfs). It powers the free-space gutter shown at the scan root.
func Free(path string) (int64, error) {
	if WSL() {
		// On WSL the root filesystem is a thin-provisioned VHDX whose statfs
		// free space reflects the host Windows disk, not the virtual disk, so
		// the figure would be misleading; report no free space instead.
		return 0, nil
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
