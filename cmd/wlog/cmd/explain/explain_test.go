// This file checks wlog explain, rules, schema, and version: every runtime source has an
// entry, an unknown id lists the near ids, and each command answers JSON.
package explain_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogdoctor "github.com/jeremygprawira/wlog/cmd/wlog/cmd/doctor"
	wlogexplain "github.com/jeremygprawira/wlog/cmd/wlog/cmd/explain"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
	"github.com/jeremygprawira/wlog/setup"
)

// run runs one command and returns its exit code, stdout, and stderr.
func run(args ...string) (int, string, string) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := wlogexplain.Run(args, stdout, stderr)
	return code, stdout.String(), stderr.String()
}

// TestExplain_EverySourceHasEntry proves that every problem code, doctor code, rule id,
// reserved field, and setup variable has an entry.
func TestExplain_EverySourceHasEntry(t *testing.T) {
	for _, problem := range wlog.Problems() {
		requireEntry(t, problem.Code)
	}
	for id := range rules.Infos {
		requireEntry(t, id)
	}
	for _, code := range wlogdoctor.Codes() {
		requireEntry(t, code)
	}
	for _, field := range wlog.ReservedFields() {
		requireEntry(t, field)
	}
	for _, factory := range setup.Builtins() {
		for _, variable := range factory.Vars {
			requireEntry(t, variable.Name)
		}
	}
}

// requireEntry proves that one id has an answer.
func requireEntry(t *testing.T, id string) {
	t.Helper()
	code, stdout, _ := run(id)
	if code != 0 || !strings.Contains(stdout, id) {
		t.Errorf("explain %s: exit %d, stdout %q", id, code, stdout)
	}
}

// TestExplain_UnknownListsClosest proves that an unknown id exits 1 and names the near
// ids.
func TestExplain_UnknownListsClosest(t *testing.T) {
	code, _, stderr := run("WLOG_DRAIN_FAILEDD")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "WLOG_DRAIN_FAILED") {
		t.Errorf("stderr = %q, want the near id", stderr)
	}
}

// TestExplain_JSON proves that --json prints the same keys as the text sections.
func TestExplain_JSON(t *testing.T) {
	code, stdout, _ := run("WLOG_DRAIN_FAILED", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	entry := map[string]any{}
	if err := json.Unmarshal([]byte(stdout), &entry); err != nil {
		t.Fatalf("json: %v", err)
	}
	for _, key := range []string{"id", "kind", "what", "fix", "link"} {
		if _, present := entry[key]; !present {
			t.Errorf("entry = %v, want the key %s", entry, key)
		}
	}
}

// TestRules_JSON proves that every rule of the table appears with its weight.
func TestRules_JSON(t *testing.T) {
	code, stdout, _ := run("rules", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(rows) != len(rules.Infos) {
		t.Errorf("rules = %d, want %d", len(rows), len(rules.Infos))
	}
	for _, row := range rows {
		if _, present := rules.Infos[row["id"].(string)]; !present {
			t.Errorf("rule %v is not in the table", row["id"])
		}
	}
}

// TestSchemaCmd proves that both embedded schemas print.
func TestSchemaCmd(t *testing.T) {
	code, stdout, _ := run("schema", "event")
	if code != 0 || !strings.Contains(stdout, "$schema") {
		t.Errorf("schema event: exit %d, output %q", code, stdout[:min(len(stdout), 80)])
	}
	code, stdout, _ = run("schema", "map")
	if code != 0 || !strings.Contains(stdout, "$schema") {
		t.Errorf("schema map: exit %d", code)
	}
	if code, _, _ := run("schema", "nope"); code != 2 {
		t.Errorf("schema nope exit = %d, want 2", code)
	}
}

// TestVersion_JSON proves the version document holds the tool, rules, schema, and Go
// versions.
func TestVersion_JSON(t *testing.T) {
	code, stdout, _ := run("version", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	document := map[string]any{}
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("json: %v", err)
	}
	for _, key := range []string{"tool", "rules", "event_schema", "map_schema", "go"} {
		if _, present := document[key]; !present {
			t.Errorf("version = %v, want the key %s", document, key)
		}
	}
}

// min returns the smaller of two numbers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
