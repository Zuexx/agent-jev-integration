//go:build windows

package fsutil

import (
	"errors"
	"os"
	"syscall"
	"time"
)

const errorSharingViolation = syscall.Errno(32)

// rename retries briefly: on Windows, renaming over a file another process has
// open (the statusline reading config.json) fails until that file is closed.
func rename(oldpath, newpath string) error {
	var err error
	for i := 0; i < 20; i++ {
		if err = os.Rename(oldpath, newpath); err == nil || !transient(err) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return err
}

func transient(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == syscall.ERROR_ACCESS_DENIED || errno == errorSharingViolation)
}
