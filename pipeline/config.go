package pipeline

import (
	"time"

	"github.com/jeremygprawira/wlog"
)

// BackoffKind selects how the delay between retries grows.
type BackoffKind int

const (
	Exponential BackoffKind = iota
	Linear
	Fixed
)

type config struct {
	batchSize     int
	minLevel      int
	minLevelSet   bool
	batchInterval time.Duration
	maxAttempts   int
	backoff       BackoffKind
	initialDelay  time.Duration
	maxDelay      time.Duration
	maxBuffer     int
	onDropped     func(events []map[string]any, err error)
}

func defaultConfig() config {
	return config{
		batchSize:     50,
		batchInterval: 5 * time.Second,
		maxAttempts:   3,
		backoff:       Exponential,
		initialDelay:  time.Second,
		maxDelay:      30 * time.Second,
		maxBuffer:     1000,
	}
}

// Option configures Wrap.
type Option func(*config)

// BatchSize sets how many events trigger an immediate flush. Default 50.
func BatchSize(n int) Option { return func(c *config) { c.batchSize = max(n, 1) } }

// BatchInterval sets the longest a partial batch waits before flushing. Default 5s.
func BatchInterval(d time.Duration) Option {
	return func(c *config) { c.batchInterval = max(d, time.Millisecond) }
}

// MaxAttempts sets total tries per batch, including the first. Default 3.
func MaxAttempts(n int) Option { return func(c *config) { c.maxAttempts = max(n, 1) } }

// Backoff selects the retry delay curve. Default Exponential.
func Backoff(kind BackoffKind) Option { return func(c *config) { c.backoff = kind } }

// InitialDelay sets the first retry's base delay. Default 1s.
func InitialDelay(d time.Duration) Option { return func(c *config) { c.initialDelay = d } }

// MaxDelay caps how long any single retry waits. Default 30s.
func MaxDelay(d time.Duration) Option {
	return func(c *config) { c.maxDelay = max(d, time.Millisecond) }
}

// MinLevel drops an event whose level is lower than level before it enters the buffer, so
// a busy service sends only what it asked for. An event carrying audit records always
// passes (SPEC.md: audit is never filtered away), and an event whose level is missing or
// unknown passes too, so a filter never hides what it cannot judge.
//
// A level name that is not debug, info, warn, or error turns the option off rather than
// guessing, so a typo can never hide every event.
func MinLevel(level wlog.Level) Option {
	return func(c *config) {
		rank, ok := levelRanks[level]
		if !ok {
			return
		}
		c.minLevel = rank
		c.minLevelSet = true
	}
}

// levelRanks orders the four levels, lowest first.
var levelRanks = map[wlog.Level]int{
	wlog.LevelDebug: 0,
	wlog.LevelInfo:  1,
	wlog.LevelWarn:  2,
	wlog.LevelError: 3,
}

// MaxBuffer caps how many events are queued at once, across all not-yet-flushed
// batches. Default 1000.
func MaxBuffer(n int) Option { return func(c *config) { c.maxBuffer = max(n, 1) } }

// OnDropped is called when an event or a batch is dropped: an event dropped for being
// the oldest once MaxBuffer is exceeded, or a whole batch dropped after MaxAttempts is
// exhausted (err set in that case).
func OnDropped(fn func(events []map[string]any, err error)) Option {
	return func(c *config) { c.onDropped = fn }
}
