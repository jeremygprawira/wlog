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
func (s *Sender) rotateIfNeeded(incoming int64) error {
	if s.f == nil {
		if err := s.open(); err != nil {
			return err
		}
	}
	info, err := s.f.Stat()
	if err != nil {
		return fmt.Errorf("file: stat %s: %w", s.cfg.path, err)
	}
	sizeRotation := info.Size() > 0 && info.Size()+incoming > s.cfg.maxSize
	ageRotation := s.cfg.maxAge > 0 && time.Since(s.openedAt) > s.cfg.maxAge
	if !sizeRotation && !ageRotation {
		return nil
	}
	return s.rotate()
}

// rotate closes the current file and shifts the backups: path.N moves to path.N+1 for
// N from maxBackups-1 down to 1, the oldest backup is deleted, and path moves to
// path.1. A fresh file is then opened at path. The caller holds s.mu.
func (s *Sender) rotate() error {
	if s.f != nil {
		if err := s.f.Close(); err != nil {
			return fmt.Errorf("file: close %s: %w", s.cfg.path, err)
		}
		s.f = nil
	}

	if s.cfg.maxBackups <= 0 {
		if err := os.Remove(s.cfg.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("file: remove %s: %w", s.cfg.path, err)
		}
		return s.open()
	}

	oldest := fmt.Sprintf("%s.%d", s.cfg.path, s.cfg.maxBackups)
	if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("file: remove %s: %w", oldest, err)
	}
	for i := s.cfg.maxBackups - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", s.cfg.path, i)
		to := fmt.Sprintf("%s.%d", s.cfg.path, i+1)
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("file: rotate %s: %w", from, err)
		}
	}
	if err := os.Rename(s.cfg.path, s.cfg.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("file: rotate %s: %w", s.cfg.path, err)
	}
	return s.open()
}
