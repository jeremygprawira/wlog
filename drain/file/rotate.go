package file

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// rotateIfNeeded rotates the file when the next write would pass maxSize, or when the
// file is older than maxAge. An empty file never rotates for size, so a first blob
// larger than maxSize still lands in the main file instead of an empty backup. The
// caller holds d.mu.
func (d *Drain) rotateIfNeeded(incoming int64) error {
	if d.f == nil {
		if err := d.open(); err != nil {
			return err
		}
	}
	info, err := d.f.Stat()
	if err != nil {
		return fmt.Errorf("file: stat %s: %w", d.cfg.path, err)
	}
	sizeRotation := info.Size() > 0 && info.Size()+incoming > d.cfg.maxSize
	ageRotation := d.cfg.maxAge > 0 && time.Since(info.ModTime()) > d.cfg.maxAge
	if !sizeRotation && !ageRotation {
		return nil
	}
	return d.rotate()
}

// rotate closes the current file and shifts the backups: path.N moves to path.N+1 for
// N from maxBackups-1 down to 1, the oldest backup is deleted, and path moves to
// path.1. A fresh file is then opened at path. The caller holds d.mu.
func (d *Drain) rotate() error {
	if d.f != nil {
		if err := d.f.Close(); err != nil {
			return fmt.Errorf("file: close %s: %w", d.cfg.path, err)
		}
		d.f = nil
	}

	if d.cfg.maxBackups <= 0 {
		if err := os.Remove(d.cfg.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("file: remove %s: %w", d.cfg.path, err)
		}
		return d.open()
	}

	oldest := fmt.Sprintf("%s.%d", d.cfg.path, d.cfg.maxBackups)
	if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("file: remove %s: %w", oldest, err)
	}
	for i := d.cfg.maxBackups - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", d.cfg.path, i)
		to := fmt.Sprintf("%s.%d", d.cfg.path, i+1)
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("file: rotate %s: %w", from, err)
		}
	}
	if err := os.Rename(d.cfg.path, d.cfg.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("file: rotate %s: %w", d.cfg.path, err)
	}
	return d.open()
}
