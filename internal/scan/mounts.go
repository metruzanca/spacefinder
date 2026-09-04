package scan

// Mount is one mounted filesystem worth offering as a scan root: a physical
// disk, partition, removable medium, or network share. Pseudo/virtual
// filesystems are filtered out by Mounts.
type Mount struct {
	// Path is the mount point and the value to scan.
	Path string
	// Device is the source device or remote path.
	Device string
	// Type is the filesystem type (ext4, vfat, ...).
	Type string
	// Free is the number of free bytes on the filesystem, or 0 when unknown.
	Free int64
}

// Mounts returns the mounted filesystems spacefinder should offer in its
// picker, in mount-table order: pseudo/virtual filesystems and bind mounts of
// an already-listed device are skipped, and every entry carries its free byte
// count (0 when statfs fails, e.g. directories that vanished).
func Mounts() []Mount {
	out := filterMounts(systemMounts())
	for i := range out {
		if WSL() {
			// On WSL free space reflects the host Windows disk, not the virtual
			// disk; suppress it so the picker does not show a misleading figure.
			continue
		}
		out[i].Free, _ = Free(out[i].Path)
	}
	return out
}

// pseudoFS are filesystem types that never back user storage: they mirror
// kernel state, live in memory, or are transient plumbing. Deploying files
// (overlay, squashfs images) and FUSE desktops (gvfs, portal) are excluded
// too; real network mounts (sshfs, cifs, nfs) are kept.
var pseudoFS = map[string]bool{
	"anon_inodefs": true, "autofs": true, "bdev": true, "binfmt_misc": true,
	"bpf": true, "cgroup": true, "cgroup2": true, "configfs": true,
	"cpuset": true, "debugfs": true, "devpts": true, "devtmpfs": true,
	"efivarfs": true, "fdesc": true, "fuse.ctl": true,
	"fuse.gvfsd-fuse": true, "fuse.portal": true, "fusectl": true,
	"hugetlbfs": true, "mqueue": true, "nsfs": true, "overlay": true,
	"proc": true, "pstore": true, "ramfs": true, "rootfs": true,
	"rpc_pipefs": true, "securityfs": true, "selinuxfs": true, "smackfs": true,
	"squashfs": true, "sysfs": true, "tmpfs": true, "tracefs": true,
	"udev": true,
}

// filterMounts drops pseudo filesystems whose type is in pseudoFS and removes
// bind mounts that duplicate the device of an earlier entry (keeping the first
// mount point for a device, e.g. "/" over "/nix/store" on a bind-mounted
// store).
func filterMounts(raw []Mount) []Mount {
	out := make([]Mount, 0, len(raw))
	seenDev := make(map[string]bool, len(raw))
	for _, m := range raw {
		if m.Path == "" || pseudoFS[m.Type] {
			continue
		}
		if m.Device != "" {
			if seenDev[m.Device] {
				continue
			}
			seenDev[m.Device] = true
		}
		out = append(out, m)
	}
	return out
}
