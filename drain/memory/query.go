package memory

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	wlogquery "github.com/jeremygprawira/wlog/query"
)

// Filter selects events from a Memory. A zero field means "any".
type Filter struct {
	Level    string                    // exact match on the level field, empty means any
	Since    time.Time                 // inclusive, zero means any
	Until    time.Time                 // inclusive, zero means any
	Contains map[string]any            // every pair must match by exact value
	Match    func(map[string]any) bool // runs last, after the cheap checks
	Limit    int                       // 0 means every match
}

// Query returns the matching events, oldest first.
//
// Limit keeps the NEWEST matches rather than the oldest: a caller that asks for the last ten
// errors wants the ten most recent ones, not the first ten a process ever logged.
func (m *Memory) Query(f Filter) []map[string]any {
	matches := make([]map[string]any, 0)
	for _, event := range m.Snapshot() {
		if matchesFilter(event, f) {
			matches = append(matches, event)
		}
	}
	if f.Limit > 0 && len(matches) > f.Limit {
		matches = matches[len(matches)-f.Limit:]
	}
	return matches
}

// Matches reports whether one event passes the filter. Read and Tail in drain/file use
// it, so one filter type covers every reader.
func Matches(event map[string]any, f Filter) bool { return matchesFilter(event, f) }

// matchesFilter reports whether one event passes every filter field, cheapest first.
func matchesFilter(event map[string]any, f Filter) bool {
	if f.Level != "" {
		if level, _ := event["level"].(string); level != f.Level {
			return false
		}
	}
	if !f.Since.IsZero() || !f.Until.IsZero() {
		timestamp, ok := eventTime(event)
		if !ok {
			return false
		}
		if !f.Since.IsZero() && timestamp.Before(f.Since) {
			return false
		}
		if !f.Until.IsZero() && timestamp.After(f.Until) {
			return false
		}
	}
	for key, want := range f.Contains {
		got, ok := event[key]
		if !ok || !reflect.DeepEqual(got, want) {
			return false
		}
	}
	return f.Match == nil || f.Match(event)
}

// eventTime reads the event's timestamp as a time.
func eventTime(event map[string]any) (time.Time, bool) {
	text, ok := event["timestamp"].(string)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// HandlerOption configures the query and the stream handler.
type HandlerOption func(*handlerConfig)

// handlerConfig holds the resolved settings of one handler.
type handlerConfig struct {
	token      string
	anyAddress bool
}

// WithToken requires the header Authorization: Bearer <token> on every request.
func WithToken(token string) HandlerOption {
	return func(c *handlerConfig) { c.token = token }
}

// WithAnyAddress accepts a request from any address. The default accepts a loopback
// client only, because the endpoint holds every event the app kept.
func WithAnyAddress() HandlerOption {
	return func(c *handlerConfig) { c.anyAddress = true }
}

// resolveHandler reads the handler options.
func resolveHandler(opts []HandlerOption) handlerConfig {
	cfg := handlerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// QueryHandler answers GET /events with a JSON array of the matching events, newest
// first. The query parameters hold the same names as the flags of wlog query, so one
// filter means the same thing in the terminal and over HTTP.
func (m *Memory) QueryHandler(opts ...HandlerOption) http.Handler {
	cfg := resolveHandler(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !handlerAllowed(r, cfg) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		filter, limit, err := parseQuery(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		matches := make([]map[string]any, 0, 16)
		for _, event := range m.Snapshot() {
			if filter.Match(event) {
				matches = append(matches, event)
			}
		}
		// Newest first, then the newest N.
		reverseEvents(matches)
		if len(matches) > limit {
			matches = matches[:limit]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(matches)
	})
}

// parseQuery reads the query parameters of one request into a filter and a limit.
func parseQuery(r *http.Request) (*wlogquery.Filter, int, error) {
	values := r.URL.Query()
	opts := wlogquery.Options{
		Levels:    splitParam(values.Get("level")),
		Kinds:     splitParam(values.Get("kind")),
		Operation: values.Get("op"),
		Status:    values.Get("status"),
		Code:      values.Get("code"),
		TraceID:   values.Get("trace"),
		RequestID: values.Get("request-id"),
		EventID:   values.Get("event-id"),
		Where:     values["where"],
		Text:      values.Get("text"),
	}
	start, err := parseTimeParam(values.Get("since"))
	if err != nil {
		return nil, 0, fmt.Errorf("since: %w", err)
	}
	end, err := parseTimeParam(values.Get("until"))
	if err != nil {
		return nil, 0, fmt.Errorf("until: %w", err)
	}
	opts.Since, opts.Until = start, end
	filter, err := wlogquery.Compile(opts)
	if err != nil {
		return nil, 0, err
	}
	limit := 100
	if text := values.Get("limit"); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil || parsed <= 0 {
			return nil, 0, fmt.Errorf("limit %q is not a positive number", text)
		}
		limit = parsed
	}
	return filter, limit, nil
}

// splitParam reads one comma-separated parameter into a list.
func splitParam(text string) []string {
	if text == "" {
		return nil
	}
	parts := strings.Split(text, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// parseTimeParam reads a duration back from now, or an RFC 3339 time.
func parseTimeParam(text string) (time.Time, error) {
	if text == "" {
		return time.Time{}, nil
	}
	if window, err := time.ParseDuration(text); err == nil {
		return time.Now().Add(-window), nil
	}
	stamp, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a duration or an RFC 3339 time", text)
	}
	return stamp, nil
}

// reverseEvents turns a list around in place, so the newest event comes first.
func reverseEvents(events []map[string]any) {
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
}

// handlerAllowed reports whether one request may read the store.
func handlerAllowed(r *http.Request, cfg handlerConfig) bool {
	if cfg.token != "" {
		return r.Header.Get("Authorization") == "Bearer "+cfg.token
	}
	if cfg.anyAddress {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
