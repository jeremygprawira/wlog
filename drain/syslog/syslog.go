// Package syslog writes wlog events as RFC 5424 frames over TLS, TCP, or UDP. It reads
// WLOG_SYSLOG_ADDR, WLOG_SYSLOG_NETWORK, WLOG_SYSLOG_APP_NAME, WLOG_SYSLOG_FACILITY,
// and WLOG_SYSLOG_SD_ID when an option does not set them.
//
// Go's log/syslog is frozen, has no TLS, and writes the older BSD format, so this package
// writes the frames itself.
//
// TCP and TLS use octet-counting frames, and UDP sends one datagram per message. The
// drain dials on the first batch. After a write error it closes the connection and
// returns a retryable error, so the next attempt dials again. A retry can repeat frames,
// so delivery is at least once.
package syslog

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
)

// Facility is the RFC 5424 facility of a frame.
type Facility int

// The facilities the standard names. Local use covers an app's own log.
const (
	Kern   Facility = 0
	User   Facility = 1
	Daemon Facility = 3
	Local0 Facility = 16
	Local7 Facility = 23
)

// Defaults the spec names.
const (
	defaultNetwork = "tls"
	defaultAppName = "-"
	defaultMaxUDP  = 2048
	dialTimeout    = 5 * time.Second
	bom            = "\ufeff"
	maxHostname    = 255
	maxAppName     = 48
)

// config holds the resolved configuration.
type config struct {
	addr         string
	network      string
	tlsConfig    *tls.Config
	facility     Facility
	appName      string
	sdID         string
	bom          bool
	maxUDP       int
	pipelineOpts []pipeline.Option
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithAddr sets the host:port of the collector. Overrides WLOG_SYSLOG_ADDR.
func WithAddr(addr string) Option { return func(c *config) { c.addr = addr } }

// WithNetwork sets tls, tcp, or udp. Overrides WLOG_SYSLOG_NETWORK. Default tls.
func WithNetwork(network string) Option { return func(c *config) { c.network = network } }

// WithTLSConfig sets the TLS configuration. MinVersion is raised to TLS 1.2.
func WithTLSConfig(cfg *tls.Config) Option {
	return func(c *config) {
		if cfg != nil {
			c.tlsConfig = cfg
		}
	}
}

// WithFacility sets the facility. Overrides WLOG_SYSLOG_FACILITY. Default User.
func WithFacility(f Facility) Option { return func(c *config) { c.facility = f } }

// WithAppName sets the app name. Overrides WLOG_SYSLOG_APP_NAME. Default service.name.
func WithAppName(name string) Option { return func(c *config) { c.appName = name } }

// WithStructuredData turns on one structured-data element with sdID. The id must be of
// the form name@digits.
func WithStructuredData(sdID string) Option { return func(c *config) { c.sdID = sdID } }

// WithBOM writes the UTF-8 byte order mark before the message. The default is true.
func WithBOM(on bool) Option { return func(c *config) { c.bom = on } }

// WithMaxUDPBytes sets the largest UDP datagram. Default 2048.
func WithMaxUDPBytes(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxUDP = n
		}
	}
}

// WithPipeline sets the pipeline options New wraps the sender with.
func WithPipeline(opts ...pipeline.Option) Option {
	return func(c *config) { c.pipelineOpts = append(c.pipelineOpts, opts...) }
}

// Sender writes frames to one collector. It implements pipeline.Sender.
type Sender struct {
	addr     string
	network  string
	tls      *tls.Config
	facility Facility
	appName  string
	sdID     string
	bom      bool
	maxUDP   int
	logger   *wlog.Logger

	mu          sync.Mutex
	conn        net.Conn
	truncations atomic.Int64
}

// New returns the drain with the pipeline defaults, or with the options WithPipeline set.
func New(opts ...Option) (wlog.Drain, error) {
	s, popts, err := newSender(opts...)
	if err != nil {
		return nil, err
	}
	return pipeline.Wrap(s, popts...), nil
}

// NewSender returns the raw sender, for a caller that builds its own pipeline.
func NewSender(opts ...Option) (*Sender, error) {
	s, _, err := newSender(opts...)
	return s, err
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) wlog.Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// newSender resolves one configuration from opts and the environment.
func newSender(opts ...Option) (*Sender, []pipeline.Option, error) {
	c := config{
		addr:     os.Getenv("WLOG_SYSLOG_ADDR"),
		network:  os.Getenv("WLOG_SYSLOG_NETWORK"),
		appName:  os.Getenv("WLOG_SYSLOG_APP_NAME"),
		sdID:     os.Getenv("WLOG_SYSLOG_SD_ID"),
		facility: User,
		bom:      true,
		maxUDP:   defaultMaxUDP,
	}
	if raw := os.Getenv("WLOG_SYSLOG_FACILITY"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("syslog: WLOG_SYSLOG_FACILITY: %w", err)
		}
		c.facility = Facility(value)
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.addr == "" {
		return nil, nil, fmt.Errorf("syslog: WLOG_SYSLOG_ADDR is required")
	}
	if c.network == "" {
		c.network = defaultNetwork
	}
	switch c.network {
	case "tls", "tcp", "udp":
	default:
		return nil, nil, fmt.Errorf("syslog: network %q is not tls, tcp, or udp", c.network)
	}
	if c.sdID != "" && !validSDID(c.sdID) {
		return nil, nil, fmt.Errorf("syslog: structured data id %q is not name@digits", c.sdID)
	}
	if c.network == "tls" {
		if c.tlsConfig == nil {
			c.tlsConfig = &tls.Config{}
		}
		// A floor of TLS 1.2 keeps an old protocol out.
		if c.tlsConfig.MinVersion < tls.VersionTLS12 {
			c.tlsConfig.MinVersion = tls.VersionTLS12
		}
	}
	return &Sender{
		addr:     c.addr,
		network:  c.network,
		tls:      c.tlsConfig,
		facility: c.facility,
		appName:  c.appName,
		sdID:     c.sdID,
		bom:      c.bom,
		maxUDP:   c.maxUDP,
	}, c.pipelineOpts, nil
}

// Setup keeps the Logger, so the drain can report its own faults.
func (s *Sender) Setup(l *wlog.Logger) error {
	s.logger = l
	return nil
}

// SendBatch dials when needed and writes one frame per event.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range events {
		frame := s.frame(event)
		if err := s.write(ctx, frame); err != nil {
			// The connection is in an unknown state, so close it. The next attempt
			// dials again. A retry can repeat a frame, so delivery is at least once.
			s.closeLocked()
			return err
		}
	}
	return nil
}

// Close closes the connection, so TLS sends close_notify.
func (s *Sender) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked()
	return nil
}

// closeLocked closes the connection and clears it.
func (s *Sender) closeLocked() {
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

// write sends one frame, dialing first when there is no connection.
func (s *Sender) write(ctx context.Context, frame []byte) error {
	conn, err := s.dial(ctx)
	if err != nil {
		return err
	}
	if s.network == "udp" {
		_, err = conn.Write(frame)
		return err
	}
	// Octet counting: the length, one space, then the message.
	header := strconv.Itoa(len(frame)) + " "
	if _, err := conn.Write([]byte(header)); err != nil {
		return err
	}
	_, err = conn.Write(frame)
	return err
}

// dial returns the open connection, or builds one.
func (s *Sender) dial(ctx context.Context) (net.Conn, error) {
	if s.conn != nil {
		return s.conn, nil
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	var (
		conn net.Conn
		err  error
	)
	if s.network == "tls" {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: s.tls}
		conn, err = tlsDialer.DialContext(ctx, "tcp", s.addr)
	} else {
		conn, err = dialer.DialContext(ctx, s.network, s.addr)
	}
	if err != nil {
		return nil, fmt.Errorf("syslog: dial %s: %w", s.addr, err)
	}
	s.conn = conn
	return conn, nil
}

// frame builds one RFC 5424 frame.
func (s *Sender) frame(event map[string]any) []byte {
	severity := severityOf(levelOf(event))
	pri := int(s.facility)*8 + severity
	hostname := s.hostname(event)
	appName := s.appNameOf(event)
	msgID := headerField(kindOf(event))
	message := s.message(event)

	var buf strings.Builder
	fmt.Fprintf(&buf, "<%d>1 %s %s %s %d %s %s %s",
		pri, utcTimestamp(event), hostname, appName, os.Getpid(), msgID, s.structuredData(event), message)

	if s.network != "udp" {
		return []byte(buf.String())
	}
	frame := buf.String()
	if len(frame) <= s.maxUDP {
		return []byte(frame)
	}
	// A UDP frame over the cap becomes the summary plus the event id, so a reader still
	// learns what happened.
	s.truncations.Add(1)
	short := fmt.Sprintf("<%d>1 %s %s %s %d %s %s %s event_id=%s",
		pri, utcTimestamp(event), hostname, appName, os.Getpid(), msgID, s.structuredData(event),
		s.bomText()+summaryOf(event), stringOf(event["event_id"]))
	return []byte(short)
}

// message returns the MSG part: the BOM, then the canonical event as JSON.
func (s *Sender) message(event map[string]any) string {
	body, err := json.Marshal(event)
	if err != nil {
		body = []byte("{}")
	}
	return s.bomText() + string(body)
}

// bomText returns the byte order mark when it is on.
func (s *Sender) bomText() string {
	if !s.bom {
		return ""
	}
	return bom
}

// structuredData returns "-", or one element with the low-cardinality fields.
func (s *Sender) structuredData(event map[string]any) string {
	if s.sdID == "" {
		return "-"
	}
	var buf strings.Builder
	buf.WriteByte('[')
	buf.WriteString(s.sdID)
	for _, pair := range []struct{ name, path string }{
		{"level", "level"},
		{"kind", "kind"},
		{"outcome", "outcome"},
		{"trace_id", "trace.trace_id"},
		{"request_id", "trace.request_id"},
	} {
		if value, ok := pathValue(event, pair.path); ok {
			if text, ok := value.(string); ok && text != "" {
				fmt.Fprintf(&buf, " %s=\"%s\"", pair.name, paramValue(text))
			}
		}
	}
	buf.WriteByte(']')
	return buf.String()
}

// hostname returns service.instance, else the machine host, else "-".
func (s *Sender) hostname(event map[string]any) string {
	if value, ok := pathValue(event, "service.instance"); ok {
		if text, ok := value.(string); ok && text != "" {
			return cut(headerField(text), maxHostname)
		}
	}
	if name, err := os.Hostname(); err == nil && name != "" {
		return cut(headerField(name), maxHostname)
	}
	return "-"
}

// appNameOf returns the configured app name, else service.name, else "-".
func (s *Sender) appNameOf(event map[string]any) string {
	if s.appName != "" {
		return cut(headerField(s.appName), maxAppName)
	}
	if value, ok := pathValue(event, "service.name"); ok {
		if text, ok := value.(string); ok && text != "" {
			return cut(headerField(text), maxAppName)
		}
	}
	return defaultAppName
}

// utcTimestamp returns the event timestamp in UTC with six fractional digits.
func utcTimestamp(event map[string]any) string {
	text, _ := event["timestamp"].(string)
	stamp, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		stamp = time.Now()
	}
	return stamp.UTC().Format("2006-01-02T15:04:05.000000Z")
}

// severityOf maps a level to the RFC 5424 severity.
func severityOf(level string) int {
	switch wlog.Level(level) {
	case wlog.LevelDebug:
		return 7
	case wlog.LevelWarn:
		return 4
	case wlog.LevelError:
		return 3
	default:
		return 6
	}
}

// headerField replaces a character outside ASCII 33 to 126 with "_", and an empty field
// with "-".
func headerField(text string) string {
	if text == "" {
		return "-"
	}
	out := make([]rune, 0, len(text))
	for _, r := range text {
		if r < 33 || r > 126 {
			out = append(out, '_')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// paramValue escapes the three characters a PARAM-VALUE reserves.
func paramValue(text string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `]`, `\]`)
	return replacer.Replace(text)
}

// cut shortens a field to max bytes.
func cut(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max]
}

// validSDID reports whether id is of the form name@digits, with no reserved character.
func validSDID(id string) bool {
	name, digits, ok := strings.Cut(id, "@")
	if !ok || name == "" || digits == "" {
		return false
	}
	if strings.ContainsAny(id, `= ]"`) {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// levelOf reads the event level.
func levelOf(event map[string]any) string {
	text, _ := event["level"].(string)
	return text
}

// kindOf reads the event kind.
func kindOf(event map[string]any) string {
	text, _ := event["kind"].(string)
	return text
}

// summaryOf reads the event summary.
func summaryOf(event map[string]any) string {
	text, _ := event["summary"].(string)
	return text
}

// stringOf reads a string value.
func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

// pathValue reads a dotted path from an event.
func pathValue(event map[string]any, path string) (any, bool) {
	var current any = event
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := object[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}
