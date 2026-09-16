package pipeline

import "time"

// BackoffKind selects how the delay between retries grows.
type BackoffKind int

const (
	Exponential BackoffKind = iota
	Linear
	Fixed
)

type config struct {
	batchSize     int
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

// MaxBuffer caps how many events are queued at once, across all not-yet-flushed
// batches. Default 1000.
func MaxBuffer(n int) Option { return func(c *config) { c.maxBuffer = max(n, 1) } }

// OnDropped is called when an event or a batch is dropped: an event dropped for being
// the oldest once MaxBuffer is exceeded, or a whole batch dropped after MaxAttempts is
// exhausted (err set in that case).
func OnDropped(fn func(events []map[string]any, err error)) Option {
	return func(c *config) { c.onDropped = fn }
}
