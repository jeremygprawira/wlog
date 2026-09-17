//go:build windows

package audit

import (
	"fmt"
	"os"
)

// lockFile takes an exclusive lock on the journal path through a sibling lock file,
// because the standard library exposes no file lock on Windows.
//
// A crash can leave the lock file behind, so an open that fails names the file to
// remove. The returned function removes it.
func lockFile(f *os.File) (func(), error) {
	lock := f.Name() + ".lock"
	h, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: journal locked by another writer: remove %s if no other writer runs: %w", lock, err)
	}
	if err := h.Close(); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(lock) }, nil
}
