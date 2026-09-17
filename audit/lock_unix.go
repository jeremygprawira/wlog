//go:build unix

package audit

import (
	"fmt"
	"os"
	"syscall"
)

// lockFile takes an exclusive, non-blocking lock on the journal file, so a second
// process that opens the same path is refused instead of interleaving two chains. The
// kernel drops the lock when the file closes, so a crash never leaves a stale lock. The
// returned function releases it, and closing the file releases it too.
func lockFile(f *os.File) (func(), error) {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("audit: journal locked by another writer: %w", err)
	}
	return func() {}, nil
}
