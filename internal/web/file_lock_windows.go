//go:build windows

package web

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

const allLockBytes = ^uint32(0)

func withExclusiveFileLock(lockPath string, openErrLabel string, lockErrLabel string, fn func() error) error {
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("%s: %w", openErrLabel, err)
	}
	defer lockFile.Close()

	overlapped := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(lockFile.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, allLockBytes, allLockBytes, overlapped); err != nil {
		return fmt.Errorf("%s: %w", lockErrLabel, err)
	}
	defer windows.UnlockFileEx(windows.Handle(lockFile.Fd()), 0, allLockBytes, allLockBytes, overlapped)

	return fn()
}
