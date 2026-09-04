//go:build darwin

package scan

import "golang.org/x/sys/unix"

// systemMounts reads the mounted filesystems via the BSD getfsstat syscall.
func systemMounts() []Mount {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil || n <= 0 {
		return nil
	}
	buf := make([]unix.Statfs_t, n)
	n, err = unix.Getfsstat(buf, unix.MNT_NOWAIT)
	if err != nil {
		return nil
	}
	mounts := make([]Mount, 0, n)
	for i := 0; i < n; i++ {
		s := &buf[i]
		mounts = append(mounts, Mount{
			Path:   unix.BytePtrToString(&s.Mntonname[0]),
			Device: unix.BytePtrToString(&s.Mntfromname[0]),
			Type:   unix.BytePtrToString(&s.Fstypename[0]),
		})
	}
	return mounts
}
