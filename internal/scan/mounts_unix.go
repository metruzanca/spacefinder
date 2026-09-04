//go:build linux

package scan

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

// systemMounts reads the kernel's mount table.
func systemMounts() []Mount {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	ms, _ := parseProcMounts(f)
	return ms
}

// parseProcMounts parses a /proc/mounts-style table with whitespace-separated
// fields of device, mount point, filesystem type, options, dump, pass. Mount
// points escape spaces, tabs, newlines, and backslashes as \040, \011, \012,
// and \134 respectively, so fields are unescaped after splitting.
func parseProcMounts(r io.Reader) ([]Mount, error) {
	var mounts []Mount
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		f := strings.Split(line, " ")
		if len(f) < 3 {
			continue
		}
		mounts = append(mounts, Mount{
			Path:   unescapeMount(f[1]),
			Device: unescapeMount(f[0]),
			Type:   f[2],
		})
	}
	return mounts, sc.Err()
}

// unescapeMount decodes the \ooo octal escapes /proc/mounts uses for special
// characters in mount points and device names.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+3 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		oct := s[i+1 : i+4]
		n, err := strconv.ParseUint(oct, 8, 8)
		if err != nil {
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte(byte(n))
		i += 3
	}
	return b.String()
}
