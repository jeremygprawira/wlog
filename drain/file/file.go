// Package file appends wlog events to a local NDJSON file. It rotates by size and by
// age, keeps a bounded number of backups, and creates every file with mode 0600. It
// reads WLOG_FILE_PATH when WithPath is not given.
package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
)

// Rotation defaults: rotate after 100 MiB or 24 hours, keep three backups next to a named
// file, and keep the newest seven daily files in the default folder.
const (
	defaultMaxSize    = 100 << 20
	defaultMaxAge     = 24 * time.Hour
	defaultMaxBackups = 3
	defaultMaxFiles   = 7
	// defaultDir is where a drain with no path writes, and defaultGitignore keeps that
	// folder out of the repository.
	defaultDir       = ".wlog"
	defaultGitignore = "*\n"
)

// errClosed is what a write after Close returns, so a late batch is reported instead of
// reopening a file the caller closed.
var errClosed = errors.New("file: drain is closed")

// config holds the resolved configuration for one Sender.
type config struct {
	path         string
	maxSize      int64
	maxAge       time.Duration
	maxBackups   int
	maxFiles     int
	pipelineOpts []pipeline.Option
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

// WithMaxFiles sets how many daily files the default folder keeps, newest first. Default
// 7. It applies when no path is set, because only then does each day get its own file.
func WithMaxFiles(n int) Option { return func(c *config) { c.maxFiles = n } }

// WithPipeline sets the pipeline options New wraps the sender with.
func WithPipeline(opts ...pipeline.Option) Option {
	return func(c *config) { c.pipelineOpts = append(c.pipelineOpts, opts...) }
}

// Sender appends batches to one file. It implements pipeline.Sender, so wrap it with
// pipeline.Wrap to get batching, retry, and a bounded buffer.
type Sender struct {
	mu       sync.Mutex
	cfg      config
	f        *os.File
	openedAt time.Time
	// daily records that the path came from the default folder, so each day gets its own
	// file and old days are pruned.
	daily bool
}

// New returns the file drain with the pipeline defaults, or with the options WithPipeline
// set. With no path and no WLOG_FILE_PATH it writes .wlog/logs/<date>.jsonl.
func New(opts ...Option) (wlog.Drain, error) {
	s, popts, err := newSender(opts...)
	if err != nil {
		return nil, err
	}
	return pipeline.Wrap(s, popts...), nil
}

// NewSender returns the raw sender, for a caller that builds its own pipeline or calls
// SendBatch itself.
func NewSender(opts ...Option) (*Sender, error) {
	s, _, err := newSender(opts...)
	return s, err
}

// newSender resolves one configuration, so New and NewSender can never disagree.
func newSender(opts ...Option) (*Sender, []pipeline.Option, error) {
	c := config{
		path:       os.Getenv("WLOG_FILE_PATH"),
		maxSize:    defaultMaxSize,
		maxAge:     defaultMaxAge,
		maxBackups: defaultMaxBackups,
		maxFiles:   defaultMaxFiles,
	}
	for _, opt := range opts {
		opt(&c)
	}
	daily := c.path == ""
	if daily {
		c.path = defaultPath(time.Now())
	}
	s := &Sender{cfg: c, daily: daily}
	if err := s.open(); err != nil {
		return nil, nil, err
	}
	if daily {
		s.prepareDefaultFolder()
	}
	return s, c.pipelineOpts, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) wlog.Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// defaultPath returns the daily file a drain with no path writes, so one day of events
// never mixes with the next.
func defaultPath(now time.Time) string {
	return filepath.Join(defaultDir, "logs", now.UTC().Format("2006-01-02")+".jsonl")
}

// prepareDefaultFolder creates the default folder's .gitignore, so the logs never reach a
// repository, and prunes old daily files to MaxFiles.
func (s *Sender) prepareDefaultFolder() {
	dir := filepath.Dir(s.cfg.path)
	gitignore := filepath.Join(defaultDir, ".gitignore")
	if _, err := os.Stat(gitignore); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(gitignore, []byte(defaultGitignore), 0o600)
	}
	s.pruneDaily(dir)
}

// pruneDaily removes every daily file in dir except the newest maxFiles, oldest first. A
// file name sorts by its date, so the sort is the oldest-first order.
func (s *Sender) pruneDaily(dir string) {
	if s.cfg.maxFiles <= 0 {
		return
	}
	names, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var files []string
	for _, entry := range names {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	for len(files) > s.cfg.maxFiles {
		_ = os.Remove(filepath.Join(dir, files[0]))
		files = files[1:]
	}
}

func (s *Sender) open() error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.path), 0o700); err != nil {
		return fmt.Errorf("file: create %s: %w", filepath.Dir(s.cfg.path), err)
	}
	f, err := os.OpenFile(s.cfg.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("file: open %s: %w", s.cfg.path, err)
	}
	s.f = f
	// Age rotation counts from the moment the file opened, because every write moves the
	// modification time and a busy file would otherwise never rotate.
	s.openedAt = time.Now()
	return nil
}

// SendBatch appends one NDJSON blob. The mutex covers the rotation check and the
// write, so concurrent batches never interleave a line or race a rename.
func (s *Sender) SendBatch(_ context.Context, events []map[string]any) error {
	if len(events) == 0 {
		return nil
	}
	var buf []byte
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("file: marshal event: %w", err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		// The caller closed the drain, so the batch is reported rather than written to a
		// file that was reopened behind the caller's back.
		return errClosed
	}
	if err := s.rotateIfNeeded(int64(len(buf))); err != nil {
		return err
	}
	if _, err := s.f.Write(buf); err != nil {
		return fmt.Errorf("file: write %s: %w", s.cfg.path, err)
	}
	return nil
}

// Close syncs and closes the file, so an event is on disk before the process ends. It is
// safe to call more than once, and a later SendBatch returns an error.
func (s *Sender) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	syncErr := s.f.Sync()
	err := s.f.Close()
	s.f = nil
	if syncErr != nil {
		return fmt.Errorf("file: sync %s: %w", s.cfg.path, syncErr)
	}
	return err
}
