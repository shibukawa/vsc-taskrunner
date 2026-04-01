//go:build !windows

package web

import "golang.org/x/sys/unix"

func freeBytesForPath(root string) uint64 {
	if root == "" {
		return 0
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(root, &stat); err != nil {
		return 0
	}
	return stat.Bavail * uint64(stat.Bsize)
}
