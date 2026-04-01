//go:build windows

package web

import "golang.org/x/sys/windows"

func freeBytesForPath(root string) uint64 {
	if root == "" {
		return 0
	}

	path, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0
	}

	var freeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(path, &freeBytes, nil, nil); err != nil {
		return 0
	}
	return freeBytes
}
