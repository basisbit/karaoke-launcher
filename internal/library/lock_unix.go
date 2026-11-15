//go:build !windows

package library

import (
	"errors"
	"os"
	"syscall"
)

// lock takes an exclusive lock on path. The system releases it when the process ends, also after a crash.
func lock(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrRunning
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}
