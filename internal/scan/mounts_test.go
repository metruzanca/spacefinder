package scan

import (
	"strings"
	"testing"
)

func TestParseProcMounts(t *testing.T) {
	fixture := `rootfs / rootfs rw 0 0
/dev/nvme0n1p2 / ext4 rw,relatime 0 0
/dev/nvme0n1p2 /nix/store ext4 ro,nosuid,nodev,relatime 0 0
/dev/nvme0n1p1 /boot vfat rw,relatime,fmask=0077 0 0
proc /proc proc rw,nosuid,nodev 0 0
tmpfs /run tmpfs rw,nosuid 0 0
gvfsd-fuse /run/user/1000/gvfs fuse.gvfsd-fuse rw,nosuid,nodev,user_id=1000 0 0
portal /run/user/1000/doc fuse.portal rw,nosuid,nodev,user_id=1000 0 0
/dev/mmcblk0p1 /mnt/snap\040drive\134x vfat rw 0 0
something /with/a/typo-only-two-fields
`
	mounts, err := parseProcMounts(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	want := []Mount{
		{Device: "rootfs", Path: "/", Type: "rootfs"},
		{Device: "/dev/nvme0n1p2", Path: "/", Type: "ext4"},
		{Device: "/dev/nvme0n1p2", Path: "/nix/store", Type: "ext4"},
		{Device: "/dev/nvme0n1p1", Path: "/boot", Type: "vfat"},
		{Device: "proc", Path: "/proc", Type: "proc"},
		{Device: "tmpfs", Path: "/run", Type: "tmpfs"},
		{Device: "gvfsd-fuse", Path: "/run/user/1000/gvfs", Type: "fuse.gvfsd-fuse"},
		{Device: "portal", Path: "/run/user/1000/doc", Type: "fuse.portal"},
		{Device: "/dev/mmcblk0p1", Path: "/mnt/snap drive\\x", Type: "vfat"},
	}
	if len(mounts) != len(want) {
		t.Fatalf("parsed %d mounts, want %d: %#v", len(mounts), len(want), mounts)
	}
	for i, w := range want {
		if m := mounts[i]; m != w {
			t.Fatalf("mount %d = %#v, want %#v", i, m, w)
		}
	}
}

func TestParseProcMountsEscapes(t *testing.T) {
	got := unescapeMount(`/media/user/USB\040STICK`)
	if got != "/media/user/USB STICK" {
		t.Fatalf("unescape = %q, want %q", got, "/media/user/USB STICK")
	}
	if u := unescapeMount(`/plain/path`); u != "/plain/path" {
		t.Fatalf("unescape plain = %q", u)
	}
}

func TestFilterMounts(t *testing.T) {
	raw := []Mount{
		{Device: "/dev/nvme0n1p2", Path: "/", Type: "ext4"},
		{Device: "/dev/nvme0n1p2", Path: "/nix/store", Type: "ext4", Free: 5}, // bind mount: dropped
		{Device: "/dev/nvme0n1p1", Path: "/boot", Type: "vfat"},
		{Device: "proc", Path: "/proc", Type: "proc"},
		{Device: "tmpfs", Path: "/run", Type: "tmpfs"},
		{Device: "gvfsd-fuse", Path: "/run/user/1000/gvfs", Type: "fuse.gvfsd-fuse"},
		{Device: "", Path: "", Type: "weird"},
		{Device: "/dev/sdb1", Path: "/media/user/USB", Type: "exfat"},
	}
	got := filterMounts(raw)
	want := []Mount{
		{Device: "/dev/nvme0n1p2", Path: "/", Type: "ext4"},
		{Device: "/dev/nvme0n1p1", Path: "/boot", Type: "vfat"},
		{Device: "/dev/sdb1", Path: "/media/user/USB", Type: "exfat"},
	}
	if len(got) != len(want) {
		t.Fatalf("filtered %d mounts, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Path != want[i].Path || got[i].Device != want[i].Device || got[i].Type != want[i].Type {
			t.Fatalf("mount %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

// TestMountsSmoke checks the real detection path runs and returns nothing
// bogus: at minimum it must list the root filesystem.
func TestMountsSmoke(t *testing.T) {
	mounts := Mounts()
	if len(mounts) == 0 {
		t.Fatal("Mounts() returned no filesystems")
	}
	for _, m := range mounts {
		if m.Path == "" {
			t.Fatalf("mount with empty path: %#v", m)
		}
		if pseudoFS[m.Type] {
			t.Fatalf("pseudo filesystem leaked through: %#v", m)
		}
	}
}
