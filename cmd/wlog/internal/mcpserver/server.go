// Package mcpserver is the wlog MCP server: seven read-only tools that answer questions
// about wlog events. Every tool is built on the same code the command line uses, and a
// file tool reads only under the roots it was given.
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jeremygprawira/wlog/cmd/wlog/internal/explain"
	"github.com/jeremygprawira/wlog/query"
	"github.com/jeremygprawira/wlog/redact"
	"github.com/jeremygprawira/wlog/schema"
)

// queryInput is the input of events_query: the wlog query filters, a source, and a limit.
type queryInput struct {
	Source    string   `json:"source,omitempty" jsonschema:"a file or folder under a root, or an HTTP URL"`
	Level     []string `json:"level,omitempty" jsonschema:"levels to keep"`
	Kind      []string `json:"kind,omitempty" jsonschema:"kinds to keep"`
	Since     string   `json:"since,omitempty" jsonschema:"a duration back from now, or an RFC 3339 time"`
	Until     string   `json:"until,omitempty" jsonschema:"an RFC 3339 time"`
	Op        string   `json:"op,omitempty" jsonschema:"a glob on operation"`
	Status    string   `json:"status,omitempty" jsonschema:"a comparison on the status, such as >=500"`
	Code      string   `json:"code,omitempty" jsonschema:"error.code equals this value"`
	Trace     string   `json:"trace_id,omitempty" jsonschema:"a trace id"`
	RequestID string   `json:"request_id,omitempty" jsonschema:"a request id"`
	EventID   string   `json:"event_id,omitempty" jsonschema:"an event id"`
	Where     []string `json:"where,omitempty" jsonschema:"a condition, such as llm.cost_micros>1000"`
	Text      string   `json:"text,omitempty" jsonschema:"a substring of summary or message"`
	Limit     int      `json:"limit,omitempty" jsonschema:"the newest N matches, 100 by default"`
}

// eventsOutput is the output of the event tools.
type eventsOutput struct {
	Events []map[string]any `json:"events"`
	Count  int              `json:"count"`
}

// idInput is the input of events_by_request_id and events_by_trace_id.
type idInput struct {
	Source    string `json:"source,omitempty" jsonschema:"a file or folder under a root, or an HTTP URL"`
	RequestID string `json:"request_id,omitempty" jsonschema:"the request id"`
	TraceID   string `json:"trace_id,omitempty" jsonschema:"the trace id"`
	Limit     int    `json:"limit,omitempty" jsonschema:"the newest N matches"`
}

// entryInput is the input of map_entry.
type entryInput struct {
	Path string `json:"path,omitempty" jsonschema:"the map file, wlog.map.json by default"`
	Name string `json:"name,omitempty" jsonschema:"the function or route of one handler"`
}

// entryOutput is the output of map_entry.
type entryOutput struct {
	Entry map[string]any `json:"entry"`
}

// explainInput is the input of explain.
type explainInput struct {
	ID string `json:"id" jsonschema:"a problem code, a doctor code, a rule id, a field, or an env var"`
}

// explainOutput is the output of explain.
type explainOutput struct {
	Entry explain.Entry `json:"entry"`
}

// redactInput is the input of redact_check. It never takes a value, because the tool
// only reports whether a key is denied.
type redactInput struct {
	Key  string `json:"key,omitempty" jsonschema:"a field name, such as password"`
	Path string `json:"path,omitempty" jsonschema:"a dotted path, such as http.request_headers.authorization"`
}

// redactOutput is the output of redact_check.
type redactOutput struct {
	Denied bool   `json:"denied"`
	Reason string `json:"reason"`
}

// schemaOutput is the output of schema_event.
type schemaOutput struct {
	Schema string `json:"schema"`
}

// Server builds the MCP server. A file tool reads only under roots.
func Server(roots []string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "wlog",
		Version: "0.7.0",
		Title:   "wlog events",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "events_query",
		Description: "Search wlog events with the wlog query filters.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in queryInput) (*mcp.CallToolResult, eventsOutput, error) {
		events, err := search(in, roots)
		return nil, events, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "events_by_request_id",
		Description: "Find the events of one request id.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, eventsOutput, error) {
		events, err := search(queryInput{Source: in.Source, RequestID: in.RequestID, Limit: in.Limit}, roots)
		return nil, events, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "events_by_trace_id",
		Description: "Find the events of one trace id.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, eventsOutput, error) {
		events, err := search(queryInput{Source: in.Source, Trace: in.TraceID, Limit: in.Limit}, roots)
		return nil, events, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "map_entry",
		Description: "Read one entry of a wlog.map.json report.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in entryInput) (*mcp.CallToolResult, entryOutput, error) {
		entry, err := mapEntry(in, roots)
		return nil, entry, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "explain",
		Description: "Explain one wlog id: a problem code, a doctor code, a rule id, a field, or an env var.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in explainInput) (*mcp.CallToolResult, explainOutput, error) {
		entry, ok := explain.Find(in.ID)
		if !ok {
			return nil, explainOutput{}, fmt.Errorf("unknown id %q, closest: %s", in.ID, strings.Join(explain.Closest(in.ID, 3), ", "))
		}
		return nil, explainOutput{Entry: entry}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "redact_check",
		Description: "Report whether the redactor denies one key or path. It takes no value.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in redactInput) (*mcp.CallToolResult, redactOutput, error) {
		return nil, redactCheck(in), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "schema_event",
		Description: "Return the JSON Schema of the wlog event.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, schemaOutput, error) {
		return nil, schemaOutput{Schema: string(schema.EventV1())}, nil
	})

	return server
}

// search filters the events of one source, newest first.
func search(in queryInput, roots []string) (eventsOutput, error) {
	opts := query.Options{
		Levels: in.Level, Kinds: in.Kind, Operation: in.Op, Status: in.Status,
		Code: in.Code, TraceID: in.Trace, RequestID: in.RequestID, EventID: in.EventID,
		Where: in.Where, Text: in.Text,
	}
	start, err := parseTime(in.Since)
	if err != nil {
		return eventsOutput{}, fmt.Errorf("since: %w", err)
	}
	end, err := parseTime(in.Until)
	if err != nil {
		return eventsOutput{}, fmt.Errorf("until: %w", err)
	}
	opts.Since, opts.Until = start, end
	filter, err := query.Compile(opts)
	if err != nil {
		return eventsOutput{}, err
	}

	all, err := readEvents(in.Source, roots)
	if err != nil {
		return eventsOutput{}, err
	}
	matches := make([]map[string]any, 0, len(all))
	for _, event := range all {
		if filter.Match(event) {
			matches = append(matches, event)
		}
	}
	for i, j := 0, len(matches)-1; i < j; i, j = i+1, j-1 {
		matches[i], matches[j] = matches[j], matches[i]
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return eventsOutput{Events: matches, Count: len(matches)}, nil
}

// parseTime reads a duration back from now, or an RFC 3339 time.
func parseTime(text string) (time.Time, error) {
	if text == "" {
		return time.Time{}, nil
	}
	if window, err := time.ParseDuration(text); err == nil {
		return time.Now().Add(-window), nil
	}
	return time.Parse(time.RFC3339, text)
}

// readEvents reads the events of one source: an HTTP URL, a file, or a folder of event
// files.
func readEvents(source string, roots []string) ([]map[string]any, error) {
	if source == "" {
		source = "."
	}
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return readURL(source)
	}
	path, err := allowedPath(source, roots)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return readFile(path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	events := []map[string]any{}
	for _, entry := range entries {
		if entry.IsDir() || !hasEventExtension(entry.Name()) {
			continue
		}
		more, err := readFile(filepath.Join(path, entry.Name()))
		if err != nil {
			return nil, err
		}
		events = append(events, more...)
	}
	return events, nil
}

// hasEventExtension reports whether one file name is an event file.
func hasEventExtension(name string) bool {
	for _, extension := range []string{".ndjson", ".jsonl", ".log"} {
		if strings.HasSuffix(name, extension) {
			return true
		}
	}
	return false
}

// readFile reads one file of events, one JSON object per line.
func readFile(path string) ([]map[string]any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	events := []map[string]any{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

// readURL reads the memory endpoint of a live app.
func readURL(address string) ([]map[string]any, error) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", address, response.Status)
	}
	events := []map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&events); err != nil {
		return nil, err
	}
	return events, nil
}

// allowedPath resolves one source path and proves that it lives under one root. A
// relative path is resolved against each root in turn, so a tool caller names a file the
// way the root sees it.
func allowedPath(source string, roots []string) (string, error) {
	candidates := []string{source}
	for _, root := range roots {
		candidates = append(candidates, filepath.Join(root, source))
	}
	for _, candidate := range candidates {
		path, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		for _, root := range roots {
			absolute, err := filepath.Abs(root)
			if err != nil {
				continue
			}
			relative, err := filepath.Rel(absolute, path)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("%s is outside every root", source)
}

// mapEntry reads one entry of a wlog.map.json report.
func mapEntry(in entryInput, roots []string) (entryOutput, error) {
	name := in.Path
	if name == "" {
		name = "wlog.map.json"
	}
	path, err := allowedPath(name, roots)
	if err != nil {
		return entryOutput{}, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return entryOutput{}, err
	}
	document := map[string]any{}
	if err := json.Unmarshal(body, &document); err != nil {
		return entryOutput{}, err
	}
	if in.Name == "" {
		return entryOutput{Entry: document}, nil
	}
	handlers, _ := document["handlers"].([]any)
	for _, item := range handlers {
		handler, _ := item.(map[string]any)
		if matchesEntry(handler, in.Name) {
			return entryOutput{Entry: handler}, nil
		}
	}
	return entryOutput{}, fmt.Errorf("no entry named %q in %s", in.Name, name)
}

// matchesEntry reports whether one handler entry names the wanted function or route.
func matchesEntry(handler map[string]any, name string) bool {
	for _, key := range []string{"function", "route", "path"} {
		if value, _ := handler[key].(string); value == name {
			return true
		}
	}
	return false
}

// redactCheck reports whether the default redactor denies one key or path.
func redactCheck(in redactInput) redactOutput {
	redactor := redact.Default()
	if in.Key == "" && in.Path == "" {
		return redactOutput{Denied: false, Reason: "no key and no path"}
	}
	if in.Path != "" {
		if redactor.DeniesPath(strings.Split(in.Path, ".")...) {
			return redactOutput{Denied: true, Reason: "the path matches the denylist"}
		}
	}
	if in.Key != "" && redactor.Denies(in.Key) {
		return redactOutput{Denied: true, Reason: "the key matches the denylist"}
	}
	return redactOutput{Denied: false, Reason: "no rule matches"}
}
