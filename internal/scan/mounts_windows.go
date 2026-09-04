//go:build windows

package scan

import "golang.org/x/sys/windows"

// systemMounts lists the available drive letters. Windows drives carry no
// device path or filesystem type through this cheap call, so both stay empty
// and the picker shows the free space instead.
func systemMounts() []Mount {
	bits, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var mounts []Mount
	for i := 0; i < 26; i++ {
		if bits&(uint32(1)<<i) == 0 {
			continue
		}
		letter := string(rune('A' + i))
		mounts = append(mounts, Mount{Path: letter + `:\`, Device: letter, Type: "drive"})
	}
	return mounts
}
