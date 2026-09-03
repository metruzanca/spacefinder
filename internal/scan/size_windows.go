//go:build windows

package scan

import "io/fs"

// infoSize reports logical size on windows, where allocated-block stats are
// not exposed through syscall.Stat_t.
func infoSize(info fs.FileInfo) int64 {
	return info.Size()
}

func infoDev(info fs.FileInfo) uint64 { return 0 }

func infoIno(info fs.FileInfo) uint64 { return 0 }

func infoNlink(info fs.FileInfo) uint64 { return 0 }
