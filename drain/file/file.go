// Package file appends wlog events to a local NDJSON file. It rotates by size and by
// age, keeps a bounded number of backups, and creates every file with mode 0600. It
// reads WLOG_FILE_PATH when WithPath is not given.
package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Rotation defaults: rotate after 100 MiB or 24 hours, and keep three backups.
const (
	defaultMaxSize    = 100 << 20
	defaultMaxAge     = 24 * time.Hour
	defaultMaxBackups = 3
)

// config holds the resolved configuration for one Drain.
type config struct {
	path       string
	maxSize    int64
	maxAge     time.Duration
	maxBackups int
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithPath sets the file path. Overrides WLOG_FILE_PATH.
func WithPath(path string) Option { return func(c *config) { c.path = path } }

// WithMaxSize sets the size in bytes that triggers a rotation. Default 100 MiB.
func WithMaxSize(size int64) Option { return func(c *config) { c.maxSize = size } }

// WithMaxAge sets how old the file may become before the next write rotates it.
// Default 24 hours. Zero disables the age check.
func WithMaxAge(age time.Duration) Option { return func(c *config) { c.maxAge = age } }

// WithMaxBackups sets how many rotated files may stay on disk. Default 3. Zero keeps
// none, so each rotation deletes the previous file instead.
func WithMaxBackups(n int) Option { return func(c *config) { c.maxBackups = n } }

// Drain appends batches to one file. It implements pipeline.Sender, so wrap it with
// pipeline.Wrap to get batching, retry, and a bounded buffer.
type Drain struct {
	mu  sync.Mutex
	cfg config
	f   *os.File
}

// New builds a Drain from opts and WLOG_FILE_PATH. It returns an error when the path
// is missing or the file cannot be opened.
func New(opts ...Option) (*Drain, error) {
	c := config{
		path:       os.Getenv("WLOG_FILE_PATH"),
		maxSize:    defaultMaxSize,
		maxAge:     defaultMaxAge,
		maxBackups: defaultMaxBackups,
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.path == "" {
		return nil, fmt.Errorf("file: WLOG_FILE_PATH is required")
	}
	d := &Drain{cfg: c}
	if err := d.open(); err != nil {
		return nil, err
	}
	return d, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) *Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// open creates or appends to the file with mode 0600. The caller holds d.mu, or New
// calls it before the Drain is shared.
func (d *Drain) open() error {
	f, err := os.OpenFile(d.cfg.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("file: open %s: %w", d.cfg.path, err)
	}
	d.f = f
	return nil
}

// SendBatch appends one NDJSON blob. The mutex covers the rotation check and the
// write, so concurrent batches never interleave a line or race a rename.
func (d *Drain) SendBatch(_ context.Context, events []map[string]any) error {
	var buf []byte
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("file: marshal event: %w", err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.rotateIfNeeded(int64(len(buf))); err != nil {
		return err
	}
	if _, err := d.f.Write(buf); err != nil {
		return fmt.Errorf("file: write %s: %w", d.cfg.path, err)
	}
	return nil
}

// Close closes the file. It is safe to call more than once.
func (d *Drain) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.f == nil {
		return nil
	}
	err := d.f.Close()
	d.f = nil
	return err
}
