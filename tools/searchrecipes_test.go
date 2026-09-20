// Package searchrecipes checks the files under integrations/search: every file of the
// table loads, and every jq program in the cookbook gives its golden output under gojq.
package searchrecipes

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itchyny/gojq"
)

// searchDir is the search recipes folder, from the tools module.
const searchDir = "../integrations/search"

// TestSearchRecipes_Files proves that every file of the table exists and loads, and that
// the phase 14 Elastic template is not here yet.
func TestSearchRecipes_Files(t *testing.T) {
	for _, name := range []string{
		"lnav/wlog.json", "grafana/loki.json", "grafana/clickhouse.json", "datadog/facets.json",
	} {
		body, err := os.ReadFile(filepath.Join(searchDir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		var value any
		if err := json.Unmarshal(body, &value); err != nil {
			t.Errorf("%s is not JSON: %v", name, err)
		}
	}

	for _, name := range []string{
		"jq/cookbook.md", "jq/events.ndjson", "clickhouse/views.sql", "axiom/queries.apl",
		"honeycomb/queries.md", "collectors/vector.toml", "collectors/fluent-bit.conf",
		"collectors/otel-collector.yaml", "collectors/otel-filelog.yaml",
	} {
		body, err := os.ReadFile(filepath.Join(searchDir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if strings.TrimSpace(string(body)) == "" {
			t.Errorf("%s is empty", name)
		}
	}

	views, err := os.ReadFile(filepath.Join(searchDir, "clickhouse/views.sql"))
	if err != nil {
		t.Fatalf("read views.sql: %v", err)
	}
	if !strings.Contains(string(views), "CREATE VIEW") {
		t.Error("views.sql holds no view")
	}

	if _, err := os.Stat(filepath.Join(searchDir, "elastic/index-template.json")); err == nil {
		t.Error("the elastic template exists, and it belongs to phase 14")
	}
}

// TestSearchRecipes_JQ proves that every cookbook program gives its golden output.
func TestSearchRecipes_JQ(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(searchDir, "jq/cookbook.md"))
	if err != nil {
		t.Fatalf("read the cookbook: %v", err)
	}
	programs, goldens := blocks(t, string(body))
	if len(programs) == 0 {
		t.Fatal("the cookbook holds no jq program")
	}
	if len(programs) != len(goldens) {
		t.Fatalf("%d programs and %d goldens, want one golden per program", len(programs), len(goldens))
	}

	input := fixture(t)
	for i, program := range programs {
		if got := runJQ(t, program, input); got != goldens[i] {
			t.Errorf("question %d answered\n%s\nwant\n%s", i+1, got, goldens[i])
		}
	}
}

// blocks reads the fenced jq and text blocks of the cookbook, in order.
func blocks(t *testing.T, body string) (programs, goldens []string) {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(body))
	var current *string
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```jq"):
			programs = append(programs, "")
			current = &programs[len(programs)-1]
		case strings.HasPrefix(trimmed, "```text"):
			goldens = append(goldens, "")
			current = &goldens[len(goldens)-1]
		case trimmed == "```":
			current = nil
		case current != nil:
			if *current != "" {
				*current += "\n"
			}
			*current += line
		}
	}
	return programs, goldens
}

// fixture reads the event fixture as one array.
func fixture(t *testing.T) []any {
	t.Helper()
	file, err := os.Open(filepath.Join(searchDir, "jq/events.ndjson"))
	if err != nil {
		t.Fatalf("open the fixture: %v", err)
	}
	defer func() { _ = file.Close() }()

	events := []any{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("the fixture holds a bad line: %v", err)
		}
		events = append(events, event)
	}
	return events
}

// runJQ runs one program over one input, and it returns one JSON value per line.
func runJQ(t *testing.T, program string, input any) string {
	t.Helper()
	query, err := gojq.Parse(program)
	if err != nil {
		t.Fatalf("parse %q: %v", program, err)
	}
	lines := []string{}
	iter := query.Run(input)
	for {
		value, ok := iter.Next()
		if !ok {
			break
		}
		if failure, isError := value.(error); isError {
			t.Fatalf("run %q: %v", program, failure)
		}
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal the answer of %q: %v", program, err)
		}
		lines = append(lines, string(body))
	}
	return strings.Join(lines, "\n")
}
