//go:build !windows

package tui

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// duSize runs a one-shot top-level `du -s` (depth-0) summary of path and
// returns its used size in bytes, or 0 when du is unavailable, the directory
// is unreadable, or the estimate takes too long. It powers the rough size
// shown on the picker's current-directory tile without scanning anything.
func duSize(path string) int64 {
	if path == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "du", "-s", "-k", path).Output()
	if err != nil {
		return 0
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return 0
	}
	kb, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil || kb < 0 {
		return 0
	}
	return kb * 1024
}
