package report_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/report"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// v2Points is a small app: one handler that fails two rules.
func v2Points() ([]entry.Point, [][]rules.Check) {
	points := []entry.Point{{
		Package:   "example.com/app/server",
		Function:  "handleOrder",
		File:      "server/orders.go",
		Line:      42,
		Framework: "github.com/labstack/echo/v4",
		Method:    "POST",
		Route:     "/orders/{id}",
	}}
	checks := [][]rules.Check{{
		{ID: rules.RuleMiddleware, Weight: 30, Pass: false, Applicable: true, Detail: "no wlog middleware call found in the program"},
		{ID: rules.RuleContext, Weight: 20, Pass: true, Applicable: true},
		{ID: rules.RuleNoPrint, Weight: 10, Pass: false, Applicable: true, Detail: "handler uses print logging"},
	}}
	return points, checks
}

// TestMap_CLI19_JSONv2Golden proves the document carries version 2, the tool and rule versions,
// short framework ids, module relative paths, object top fixes, a summary, and evidence per rule
// result.
func TestMap_CLI19_JSONv2Golden(t *testing.T) {
	points, checks := v2Points()
	document := report.Build(points, checks, 80, false)

	data, err := report.Encode(document)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}

	if got := decoded["version"]; got != float64(2) {
		t.Errorf("version = %v, want 2", got)
	}
	for _, key := range []string{"tool_version", "rules_version", "summary", "top_fixes", "evidence"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("the document has no %q: %s", key, data)
		}
	}

	handler := decoded["handlers"].([]any)[0].(map[string]any)
	if got := handler["framework"]; got != "echo4" {
		t.Errorf("framework = %v, want the short id echo4", got)
	}
	if got := handler["file"]; got != "server/orders.go" {
		t.Errorf("file = %v, want the module relative path", got)
	}

	fix := decoded["top_fixes"].([]any)[0].(map[string]any)
	for _, key := range []string{"rule", "points", "handlers"} {
		if _, ok := fix[key]; !ok {
			t.Errorf("a top fix has no %q: %v", key, fix)
		}
	}

	// The golden file pins the whole document, so a later field cannot drift in silently.
	compareGolden(t, "map_v2.json", string(data))
}

// TestMap_PAR32_Evidence proves every rule result carries its file:line.
func TestMap_PAR32_Evidence(t *testing.T) {
	points, checks := v2Points()
	document := report.Build(points, checks, 80, false)

	data, err := report.Encode(document)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, want := range []string{`"evidence"`, "server/orders.go:42"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the document holds no %s:\n%s", want, data)
		}
	}
}

// TestMap_PAR29_FixFirst proves the text report lists the failing handlers with file:line, the
// rule id, and a fix line, then FIX FIRST with the fixes worth the most and the projected score,
// each with a docs link.
func TestMap_PAR29_FixFirst(t *testing.T) {
	points, checks := v2Points()
	document := report.Build(points, checks, 80, false)

	text := report.Text(document)
	for _, want := range []string{
		"server/orders.go:42",
		rules.RuleMiddleware,
		"FIX FIRST",
		"projected score",
		"https://github.com/jeremygprawira/wlog/",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the text report holds no %q:\n%s", want, text)
		}
	}
	// A passing rule is not listed as a fix.
	if strings.Contains(text, "context.set") {
		t.Errorf("a passing rule appears in the fix list:\n%s", text)
	}
	compareGolden(t, "report_text.txt", text)
}

// compareGolden compares one rendering with testdata/golden/<name>, writing the file when
// UPDATE_GOLDEN=1 is set.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := "testdata/" + name
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run UPDATE_GOLDEN=1)", path, err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
