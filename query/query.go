// Package query is the one filter language for wlog search. wlog query, the drain-memory
// query endpoint, and wlog mcp all use it, so one filter means the same thing everywhere.
//
// Read top to bottom: Options holds the parsed flags of one query. Compile turns them
// into a Filter, and Match reports whether one event passes. Every part of the language
// is stdlib only.
package query

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Options holds the parsed flags of one query. An empty field means no filter.
type Options struct {
	Levels    []string // levels to keep, such as error and warn
	Kinds     []string // kinds to keep, such as request and message
	Since     time.Time
	Until     time.Time
	Operation string // a glob on operation, where ** crosses /
	Status    string // a comparison on the status field of the event, such as >=500
	Code      string // error.code equals this value
	TraceID   string
	RequestID string
	EventID   string
	Where     []string // field path, operator, and value, such as llm.cost_micros>1000
	Text      string   // a substring of summary or message, ignoring case
}

// Filter is one compiled query.
type Filter struct {
	levels    map[string]bool
	kinds     map[string]bool
	since     time.Time
	until     time.Time
	operation *regexp.Regexp
	status    *condition
	code      string
	traceID   string
	requestID string
	eventID   string
	where     []condition
	text      string
}

// condition is one comparison on one field of an event.
type condition struct {
	path  string
	op    byte
	value string
	regex *regexp.Regexp
}

// Compile turns one Options into a Filter, and it reports the first flag it cannot read.
func Compile(opts Options) (*Filter, error) {
	filter := &Filter{
		since: opts.Since, until: opts.Until,
		code: opts.Code, traceID: opts.TraceID, requestID: opts.RequestID, eventID: opts.EventID,
		text: strings.ToLower(opts.Text),
	}
	if len(opts.Levels) > 0 {
		filter.levels = make(map[string]bool, len(opts.Levels))
		for _, level := range opts.Levels {
			filter.levels[strings.ToLower(level)] = true
		}
	}
	if len(opts.Kinds) > 0 {
		filter.kinds = make(map[string]bool, len(opts.Kinds))
		for _, kind := range opts.Kinds {
			filter.kinds[strings.ToLower(kind)] = true
		}
	}
	if opts.Operation != "" {
		pattern, err := globPattern(opts.Operation)
		if err != nil {
			return nil, fmt.Errorf("operation glob %q: %w", opts.Operation, err)
		}
		filter.operation = pattern
	}
	if opts.Status != "" {
		status, err := parseCondition("status", opts.Status)
		if err != nil {
			return nil, err
		}
		filter.status = &status
	}
	for _, where := range opts.Where {
		parsed, err := parseWhere(where)
		if err != nil {
			return nil, err
		}
		filter.where = append(filter.where, parsed)
	}
	return filter, nil
}

// Match reports whether one event passes every part of the filter.
func (f *Filter) Match(event map[string]any) bool {
	if f.levels != nil {
		level, _ := event["level"].(string)
		if !f.levels[strings.ToLower(level)] {
			return false
		}
	}
	if f.kinds != nil {
		kind, _ := event["kind"].(string)
		if !f.kinds[strings.ToLower(kind)] {
			return false
		}
	}
	if !f.since.IsZero() || !f.until.IsZero() {
		stamp, ok := eventTime(event)
		if !ok {
			return false
		}
		if !f.since.IsZero() && stamp.Before(f.since) {
			return false
		}
		if !f.until.IsZero() && stamp.After(f.until) {
			return false
		}
	}
	if f.operation != nil {
		operation, _ := event["operation"].(string)
		if !f.operation.MatchString(operation) {
			return false
		}
	}
	if f.status != nil && !matchStatus(*f.status, event) {
		return false
	}
	if f.code != "" && Field(event, "error.code") != f.code {
		return false
	}
	if f.traceID != "" && Field(event, "trace.trace_id") != f.traceID {
		return false
	}
	if f.requestID != "" && Field(event, "trace.request_id") != f.requestID {
		return false
	}
	if f.eventID != "" && Field(event, "event_id") != f.eventID {
		return false
	}
	for _, where := range f.where {
		if !matchCondition(where, Field(event, where.path)) {
			return false
		}
	}
	if f.text != "" {
		summary, _ := event["summary"].(string)
		message, _ := event["message"].(string)
		if !strings.Contains(strings.ToLower(summary), f.text) &&
			!strings.Contains(strings.ToLower(message), f.text) {
			return false
		}
	}
	return true
}

// Field returns the value at one dotted path of an event, and an empty string when the
// path is absent. A path walks nested objects, such as http.status.
func Field(event map[string]any, path string) string {
	current := any(event)
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = object[part]
		if !ok {
			return ""
		}
	}
	return valueText(current)
}

// FieldValue returns the raw value at one dotted path, and false when the path is absent.
func FieldValue(event map[string]any, path string) (any, bool) {
	current := any(event)
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// eventTime returns the timestamp of one event.
func eventTime(event map[string]any) (time.Time, bool) {
	text, _ := event["timestamp"].(string)
	if text == "" {
		return time.Time{}, false
	}
	stamp, err := time.Parse(time.RFC3339Nano, text)
	return stamp, err == nil
}

// statusPaths are the fields the status filter reads, in order, so one flag covers an
// HTTP request, an RPC call, and a command.
var statusPaths = []string{"http.status", "rpc.status_code", "cli.exit_code"}

// matchStatus compares the first status field the event has.
func matchStatus(status condition, event map[string]any) bool {
	for _, path := range statusPaths {
		if value, ok := FieldValue(event, path); ok {
			return matchCondition(status, valueText(value))
		}
	}
	return false
}

// matchCondition compares one value with one condition.
func matchCondition(c condition, actual any) bool {
	if c.op == '?' {
		return actual != nil && valueText(actual) != ""
	}
	text := valueText(actual)
	switch c.op {
	case '~':
		return c.regex != nil && c.regex.MatchString(text)
	case '=':
		return equalValue(text, c.value)
	case '!':
		return !equalValue(text, c.value)
	}
	got, ok := number(text)
	if !ok {
		return false
	}
	want, ok := number(c.value)
	if !ok {
		return false
	}
	switch c.op {
	case '>':
		return got > want
	case '<':
		return got < want
	case 'g': // >=
		return got >= want
	case 'l': // <=
		return got <= want
	}
	return false
}

// equalValue compares two values: as numbers when both are numbers, and as text
// otherwise.
func equalValue(text, want string) bool {
	if got, ok := number(text); ok {
		if target, ok := number(want); ok {
			return got == target
		}
	}
	return text == want
}

// number reads one value as a number. JSON numbers arrive as float64, and a stored int
// arrives as int64.
func number(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case int64:
		return float64(number), true
	case int:
		return float64(number), true
	}
	parsed, err := strconv.ParseFloat(valueText(value), 64)
	if err != nil || math.IsNaN(parsed) {
		return 0, false
	}
	return parsed, true
}

// valueText renders one value for a comparison. A missing value is an empty string.
func valueText(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		return strconv.FormatBool(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(value, 10)
	case int:
		return strconv.Itoa(value)
	default:
		return fmt.Sprint(value)
	}
}

// parseWhere reads one where flag, such as llm.cost_micros>1000.
func parseWhere(text string) (condition, error) {
	index, op := operatorIndex(text)
	if index < 0 {
		return condition{}, fmt.Errorf("where %q: no operator, want one of =, !=, >, >=, <, <=, ~, ?", text)
	}
	path := strings.TrimSpace(text[:index])
	value := strings.TrimSpace(text[index+len(op):])
	if path == "" {
		return condition{}, fmt.Errorf("where %q: the field path is empty", text)
	}
	if op != "?" && value == "" {
		return condition{}, fmt.Errorf("where %q: the value is empty", text)
	}
	return parseCondition(path, op+value)
}

// operatorIndex returns the position and the text of the first operator of one where
// flag. It reads a two-character operator before a one-character one.
func operatorIndex(text string) (int, string) {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '=', '!', '~':
			if i+1 < len(text) && text[i+1] == '=' {
				return i, text[i : i+2]
			}
			if text[i] != '!' {
				return i, text[i : i+1]
			}
		case '>', '<', '?':
			if i+1 < len(text) && text[i+1] == '=' {
				return i, text[i : i+2]
			}
			return i, text[i : i+1]
		}
	}
	return -1, ""
}

// parseCondition reads one path and its operator plus value, such as status>=500.
func parseCondition(path, text string) (condition, error) {
	index, op := operatorIndex(text)
	if index < 0 {
		// A bare value means equality, which is what --status 500 does.
		return condition{path: path, op: '=', value: text}, nil
	}
	value := strings.TrimSpace(text[index+len(op):])
	c := condition{path: path, op: opByte(op), value: value}
	switch op {
	case "!=":
		c.op = '!'
	case ">=":
		c.op = 'g'
	case "<=":
		c.op = 'l'
	case "~":
		pattern, err := regexp.Compile(value)
		if err != nil {
			return condition{}, fmt.Errorf("where %q: %w", text, err)
		}
		c.regex = pattern
	case "?":
		c.value = ""
	}
	return c, nil
}

// opByte maps the text of one operator to its stored byte.
func opByte(op string) byte {
	switch op {
	case ">=":
		return 'g'
	case "<=":
		return 'l'
	case "!=":
		return '!'
	default:
		return op[0]
	}
}

// globPattern turns one operation glob into a regular expression. A star matches any run
// of characters except a slash, and a double star crosses a slash too.
func globPattern(glob string) (*regexp.Regexp, error) {
	var pattern strings.Builder
	pattern.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch glob[i] {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				pattern.WriteString(".*")
				i++
				continue
			}
			pattern.WriteString("[^/]*")
		case '?':
			pattern.WriteString("[^/]")
		default:
			pattern.WriteString(regexp.QuoteMeta(string(glob[i])))
		}
	}
	pattern.WriteString("$")
	return regexp.Compile(pattern.String())
}
