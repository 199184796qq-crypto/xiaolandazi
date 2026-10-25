//go:build !windows

package recordingcache

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockActiveFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}

func unlockActiveFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
