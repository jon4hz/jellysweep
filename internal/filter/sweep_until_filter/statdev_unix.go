//go:build unix

package sweepuntilfilter

import "syscall"

// deviceID returns the filesystem device id (st_dev) for path. st_dev uniquely
// identifies the mounted filesystem containing the path, so it is a reliable
// deduplication key even when no partition table is available (e.g. minimal
// containers): bind mounts of the same filesystem share one st_dev, while distinct
// filesystems — including separate ZFS datasets — get distinct ids.
func deviceID(path string) (uint64, bool) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, false
	}
	return uint64(st.Dev), true //nolint:unconvert // st.Dev's type varies by platform (int32 on darwin, uint64 on linux)
}
