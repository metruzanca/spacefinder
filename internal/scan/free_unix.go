//go:build !windows

package scan

import "syscall"

// Free returns the bytes available to the caller on the filesystem containing
// path (statfs). It powers the free-space gutter shown at the scan root.
func Free(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
