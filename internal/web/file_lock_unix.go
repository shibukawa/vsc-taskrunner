//go:build !windows

package web

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func withExclusiveFileLock(lockPath string, openErrLabel string, lockErrLabel string, fn func() error) error {
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("%s: %w", openErrLabel, err)
	}
	defer lockFile.Close()

	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("%s: %w", lockErrLabel, err)
	}
	defer unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)

	return fn()
}
