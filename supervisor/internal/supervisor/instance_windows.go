//go:build windows

package supervisor

import (
	"errors"
	"os"
	"syscall"
)

// The kernel owns the exclusion. A crash releases the handle automatically;
// the presence of the file alone is never treated as a live owner.
func acquireInstanceLock(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, syscall.Errno(32)) { // ERROR_SHARING_VIOLATION
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
