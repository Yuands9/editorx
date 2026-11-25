package editor

import (
	"errors"
	"os"
	"syscall"
	"time"
)

func replaceSavedFile(temporary, target string) error {
	// Windows can report either error while a reader briefly holds a handle without delete sharing.
	const sharingViolation = syscall.Errno(32)
	err := os.Rename(temporary, target)
	if !errors.Is(err, sharingViolation) && !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return err
	}
	original, statErr := os.Lstat(target)
	if statErr != nil || !original.Mode().IsRegular() || original.Mode().Perm()&0o200 == 0 {
		return err
	}
	for _, delay := range [...]time.Duration{10, 20, 40, 80} {
		time.Sleep(delay * time.Millisecond)
		current, statErr := os.Lstat(target)
		if statErr != nil || !os.SameFile(original, current) || current.Size() != original.Size() ||
			current.Mode() != original.Mode() || !current.ModTime().Equal(original.ModTime()) {
			return err
		}
		err = os.Rename(temporary, target)
		if !errors.Is(err, sharingViolation) && !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			return err
		}
	}
	return err
}
