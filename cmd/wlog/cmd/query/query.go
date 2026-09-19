// Command query is wlog query. It reads events from files, folders, stdin, or a live
// app, keeps the ones the filter matches, and prints them in one of four formats.
package query

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jeremygprawira/wlog/cmd/wlog/internal/term"
	"github.com/jeremygprawira/wlog/query"
)

// maxLine bounds one event line. An event is capped at 256 KiB by core, and the reader
// keeps room for the JSON wrapper.
const maxLine = 1 << 20

// extensions are the file names a folder source reads.
var extensions = []string{".ndjson", ".jsonl", ".log", ".gz"}

// Run runs wlog query. It returns grep's exit codes: 0 for at least one match, 1 for no
// match, and 2 for a usage or read error.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("query", flag.ContinueOnError)
	flags.SetOutput(stderr)
	levels := flags.String("level", "", "levels to keep, comma separated")
	kinds := flags.String("kind", "", "kinds to keep, comma separated")
	since := flags.String("since", "", "a duration back from now, such as 15m, or an RFC 3339 time")
	until := flags.String("until", "", "an RFC 3339 time")
	operation := flags.String("op", "", "a glob on operation, where ** crosses /")
	status := flags.String("status", "", "a comparison on the status, such as >=500")
	code := flags.String("code", "", "error.code equals this value")
	trace := flags.String("trace", "", "a trace id")
	requestID := flags.String("request-id", "", "a request id")
	eventID := flags.String("event-id", "", "an event id")
	text := flags.String("text", "", "a substring of summary or message, ignoring case")
	format := flags.String("format", "", "summary, json, pretty, or table")
	fields := flags.String("fields", "", "columns for table, or keys to keep for json")
	limit := flags.Int("limit", 100, "keep the newest N matches")
	oldest := flags.Bool("oldest", false, "keep the oldest N matches")
	url := flags.String("url", "", "read the memory endpoint of a live app")
	groupBy := flags.String("group-by", "", "count or measure one field per group")
	countFlag := flags.Bool("count", false, "print the count, per group with --group-by")
	statsField := flags.String("stats", "", "print count, p50, p95, p99, and max of one numeric field")
	sizeFlag := flags.Bool("size", false, "print the bytes per event and the monthly rate")
	follow := flags.Bool("follow", false, "keep reading appended lines and rotated files")
	where := &whereFlag{}
	flags.Var(where, "where", "a condition, repeatable, such as llm.cost_micros>1000")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	opts, err := options(*since, *until, *levels, *kinds, *operation, *status, *code, *trace, *requestID, *eventID, *text, where.values)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
		return 2
	}
	filter, err := query.Compile(opts)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
		return 2
	}

	chosen := *format
	if chosen == "" {
		if file, ok := stdout.(*os.File); ok && term.IsTerminal(file) {
			chosen = "summary"
		} else {
			chosen = "json"
		}
	}
	print, err := printerFor(chosen, splitFields(*fields), stdout)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
		return 2
	}

	window, err := newWindow(*limit, *oldest)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
		return 2
	}

	if *follow {
		return followSources(flags.Args(), *url, filter, print, stderr)
	}

	sources := flags.Args()
	if *url != "" {
		sources = append(sources, *url)
	}
	if len(sources) == 0 {
		sources = defaultSources()
	}

	var skipped int
	read := func(reader io.Reader, source string) error {
		err := scan(reader, filter, window, func(line string) { skipped++ })
		if err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		return nil
	}
	for _, source := range sources {
		if source == "-" {
			if err := read(os.Stdin, "-"); err != nil {
				_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
				return 2
			}
			continue
		}
		if *url != "" && source == *url {
			if err := readURL(source, filter, window); err != nil {
				_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
				return 2
			}
			continue
		}
		if err := readPath(source, read); err != nil {
			_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
			return 2
		}
		if window.full() {
			break
		}
	}

	matches := window.events()
	switch {
	case *sizeFlag:
		if err := printSizes(stdout, matches); err != nil {
			_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
			return 2
		}
	case *groupBy != "" && *countFlag:
		for _, bucket := range query.Counts(matches, *groupBy) {
			if _, err := fmt.Fprintf(stdout, "%d  %s\n", bucket.Count, bucket.Key); err != nil {
				return 2
			}
		}
	case *groupBy != "" && *statsField != "":
		for _, bucket := range query.GroupStats(matches, *groupBy, *statsField) {
			if _, err := fmt.Fprintf(stdout, "%s  %d  %.1f  %.1f  %.1f  %.1f\n",
				bucket.Key, bucket.Stats.Count, bucket.Stats.P50, bucket.Stats.P95, bucket.Stats.P99, bucket.Stats.Max); err != nil {
				return 2
			}
		}
	case *statsField != "":
		stats := query.Statistics(matches, *statsField)
		if _, err := fmt.Fprintf(stdout, "%d  %.1f  %.1f  %.1f  %.1f\n",
			stats.Count, stats.P50, stats.P95, stats.P99, stats.Max); err != nil {
			return 2
		}
	case *countFlag:
		if _, err := fmt.Fprintf(stdout, "%d matches\n", len(matches)); err != nil {
			return 2
		}
	default:
		for _, event := range matches {
			if err := print(event); err != nil {
				_, _ = fmt.Fprintf(stderr, "wlog query: %v\n", err)
				return 2
			}
		}
	}
	if skipped > 0 {
		_, _ = fmt.Fprintf(stderr, "wlog query: skipped %d lines\n", skipped)
	}
	if len(matches) == 0 {
		return 1
	}
	return 0
}

// options builds the filter options of one query.
func options(since, until, levels, kinds, operation, status, code, trace, requestID, eventID, text string, where []string) (query.Options, error) {
	opts := query.Options{
		Levels: splitList(levels), Kinds: splitList(kinds),
		Operation: operation, Status: status, Code: code,
		TraceID: trace, RequestID: requestID, EventID: eventID,
		Where: where, Text: text,
	}
	start, err := parseTime(since)
	if err != nil {
		return opts, fmt.Errorf("--since: %w", err)
	}
	end, err := parseTime(until)
	if err != nil {
		return opts, fmt.Errorf("--until: %w", err)
	}
	opts.Since, opts.Until = start, end
	return opts, nil
}

// parseTime reads a duration back from now, or an RFC 3339 time.
func parseTime(text string) (time.Time, error) {
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

// splitList reads one comma-separated flag into a list.
func splitList(text string) []string {
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

// splitFields reads the fields flag into a list.
func splitFields(text string) []string { return splitList(text) }

// defaultSources returns the sources of a query with no argument: stdin when it is a
// pipe, and the default folder of drain-file otherwise.
func defaultSources() []string {
	info, err := os.Stdin.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice == 0 {
		return []string{"-"}
	}
	return []string{filepath.Join(".wlog", "logs")}
}

// readPath reads one file, one folder, or one compressed file.
func readPath(source string, read func(io.Reader, string) error) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return readFile(source, read)
	}
	names, err := folderFiles(source)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := readFile(name, read); err != nil {
			return err
		}
	}
	return nil
}

// folderFiles returns the event files of one folder, in name order.
func folderFiles(folder string) ([]string, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		for _, extension := range extensions {
			if strings.HasSuffix(entry.Name(), extension) {
				names = append(names, filepath.Join(folder, entry.Name()))
				break
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

// readFile reads one file, and it decompresses a gzip file.
func readFile(path string, read func(io.Reader, string) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if strings.HasSuffix(path, ".gz") {
		reader, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()
		return read(reader, path)
	}
	return read(file, path)
}

// readURL reads the memory endpoint of one live app. The endpoint answers a JSON array
// of events, and any other body is read line by line, like a file.
func readURL(address string, filter *query.Filter, sink matchSink) error {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", address, response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	var events []map[string]any
	if err := json.Unmarshal(body, &events); err == nil {
		// The endpoint answers newest first, and a reader takes the events in
		// event order, so the newest- and oldest-limits behave as they do on a
		// file.
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
		for _, event := range events {
			if filter.Match(event) {
				sink.add(event)
			}
		}
		return nil
	}
	return scan(bytes.NewReader(body), filter, sink, func(string) {})
}

// matchSink receives the matches of one scan.
type matchSink interface {
	add(map[string]any)
	full() bool
}

// scan reads one line per event, keeps the matches, and counts a line that is not a JSON
// object.
func scan(reader io.Reader, filter *query.Filter, window matchSink, skip func(string)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLine)
	for scanner.Scan() {
		line := scanner.Bytes()
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil || event == nil {
			skip(string(line))
			continue
		}
		if filter.Match(event) {
			window.add(event)
			if window.full() {
				return nil
			}
		}
	}
	return scanner.Err()
}

// window keeps the matches of one query, bounded by the limit.
type window struct {
	limit   int
	oldest  bool
	events_ []map[string]any
}

// newWindow builds the match window of one query.
func newWindow(limit int, oldest bool) (*window, error) {
	if limit <= 0 {
		return nil, errors.New("--limit must be positive")
	}
	return &window{limit: limit, oldest: oldest}, nil
}

// add keeps one match: the newest N, or the oldest N.
func (w *window) add(event map[string]any) {
	if w.oldest {
		if len(w.events_) < w.limit {
			w.events_ = append(w.events_, event)
		}
		return
	}
	w.events_ = append(w.events_, event)
	if len(w.events_) > w.limit {
		w.events_ = w.events_[1:]
	}
}

// full reports whether an oldest query has every match it needs.
func (w *window) full() bool { return w.oldest && len(w.events_) >= w.limit }

// events returns the kept matches in reading order.
func (w *window) events() []map[string]any { return w.events_ }

// printerFor returns the printer of one format.
func printerFor(format string, fields []string, out io.Writer) (func(map[string]any) error, error) {
	switch format {
	case "summary":
		return func(event map[string]any) error {
			_, err := fmt.Fprintln(out, summaryLine(event))
			return err
		}, nil
	case "json":
		return func(event map[string]any) error {
			return writeJSON(out, keepFields(event, fields), false)
		}, nil
	case "pretty":
		return func(event map[string]any) error {
			return writeJSON(out, event, true)
		}, nil
	case "table":
		columns := fields
		if len(columns) == 0 {
			columns = []string{"timestamp", "level", "operation", "summary"}
		}
		writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(writer, strings.Join(columns, "\t")); err != nil {
			return nil, err
		}
		return func(event map[string]any) error {
			values := make([]string, len(columns))
			for i, column := range columns {
				values[i] = query.Field(event, column)
			}
			if _, err := fmt.Fprintln(writer, strings.Join(values, "\t")); err != nil {
				return err
			}
			return writer.Flush()
		}, nil
	default:
		return nil, fmt.Errorf("--format %q is not summary, json, pretty, or table", format)
	}
}

// writeJSON writes one event as one JSON value.
func writeJSON(out io.Writer, event map[string]any, pretty bool) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(event)
}

// keepFields returns the named keys of one event, and the whole event when no key is
// named. A dotted key rebuilds its nesting.
func keepFields(event map[string]any, fields []string) map[string]any {
	if len(fields) == 0 {
		return event
	}
	out := map[string]any{}
	for _, field := range fields {
		value, ok := query.FieldValue(event, field)
		if !ok {
			continue
		}
		setPath(out, field, value)
	}
	return out
}

// setPath writes one value at one dotted path of a map.
func setPath(into map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := into
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

// summaryLine prints one event the way the spec's example does: the time, the level, the
// operation, the status, the duration, and the error with its fix.
func summaryLine(event map[string]any) string {
	var line strings.Builder
	if stamp, _ := event["timestamp"].(string); stamp != "" {
		line.WriteString(stamp)
		line.WriteByte(' ')
	}
	if level, _ := event["level"].(string); level != "" {
		line.WriteString(strings.ToUpper(level))
		line.WriteByte(' ')
	}
	if operation, _ := event["operation"].(string); operation != "" {
		line.WriteString(operation)
		line.WriteByte(' ')
	}
	if status := statusText(event); status != "" {
		line.WriteString(status)
		line.WriteByte(' ')
	}
	if duration, ok := event["duration_ms"].(float64); ok {
		fmt.Fprintf(&line, "in %.1fms", duration)
	}
	if code := query.Field(event, "error.code"); code != "" {
		line.WriteString(": ")
		line.WriteString(code)
	}
	if message := query.Field(event, "error.message"); message != "" {
		line.WriteByte(' ')
		line.WriteString(message)
	}
	if fix := query.Field(event, "error.fix"); fix != "" {
		fmt.Fprintf(&line, " (fix: %s)", fix)
	}
	return strings.TrimSpace(line.String())
}

// statusText returns the status of one event, from whichever field it has.
func statusText(event map[string]any) string {
	for _, path := range []string{"http.status", "rpc.status_code", "cli.exit_code"} {
		if value := query.Field(event, path); value != "" {
			return value
		}
	}
	return ""
}

// whereFlag collects the repeatable where flags.
type whereFlag struct {
	values []string
}

// String returns the collected values.
func (w *whereFlag) String() string { return strings.Join(w.values, ",") }

// Set adds one value.
func (w *whereFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("the where flag is empty")
	}
	w.values = append(w.values, value)
	return nil
}
