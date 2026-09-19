// This file tests the schema check: every golden document of the repository satisfies the
// schema of its shape, and a document that breaks one is reported.
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// rootDir returns the workspace root, which holds the schema files and the goldens.
func rootDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestSchema_BET7_GoldensValid proves that every golden event and the map report satisfy
// the schema of their shape, so a drift between the code and the spec fails the build.
func TestSchema_BET7_GoldensValid(t *testing.T) {
	root := rootDir(t)

	kinds, err := filepath.Glob(filepath.Join(root, eventGoldens, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 8 {
		t.Errorf("event goldens = %d, want one for each of the eight kinds", len(kinds))
	}

	suite, err := filepath.Glob(filepath.Join(root, suiteGoldens, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(suite) != 18 {
		t.Errorf("http suite goldens = %d, want one for each scenario that starts an event", len(suite))
	}

	found, err := check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range found {
		t.Errorf("golden breaks its schema: %s", line)
	}
}

// TestSchema_SPECG18_BadEventFails proves that the event schema refuses a document that
// breaks it, so the check cannot pass on anything.
func TestSchema_SPECG18_BadEventFails(t *testing.T) {
	root := rootDir(t)
	events, err := compile(filepath.Join(root, "schema", "event.v1.json"))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		doc  string
	}{
		{
			"a level that is a number",
			`{"timestamp":"2026-01-01T00:00:00Z","level":5,"kind":"work","outcome":"success"}`,
		},
		{
			"a kind that no table names",
			`{"timestamp":"2026-01-01T00:00:00Z","level":"info","kind":"task","outcome":"success"}`,
		},
		{
			"a status that is a string",
			`{"timestamp":"2026-01-01T00:00:00Z","level":"info","kind":"request","outcome":"success","http":{"status":"502"}}`,
		},
		{
			"a missing outcome",
			`{"timestamp":"2026-01-01T00:00:00Z","level":"info","kind":"work"}`,
		},
		{
			"a bad timestamp",
			`{"timestamp":"yesterday","level":"info","kind":"work","outcome":"success"}`,
		},
		{
			"a schema version of another shape",
			`{"timestamp":"2026-01-01T00:00:00Z","level":"info","kind":"work","outcome":"success","wlog":{"schema_version":1}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := decodeBytes([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			if err := events.Validate(value); err == nil {
				t.Error("the schema accepted a document that breaks it")
			}
		})
	}

	good, err := decodeBytes([]byte(`{"timestamp":"2026-01-01T00:00:00Z","level":"info","kind":"work","outcome":"success","order_id":"A-1","wlog":{"schema_version":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := events.Validate(good); err != nil {
		t.Errorf("the schema refused a good event: %v", err)
	}
}
