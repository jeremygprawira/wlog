package report_test

import (
	"encoding/json"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/report"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// TestMap_BET9_SARIFSchema proves the SARIF output carries the fields the 2.1.0 schema requires:
// a version, one run with a driver that declares its rules, and one result per failed rule with
// a rule id, a message, and a physical location with a line.
func TestMap_BET9_SARIFSchema(t *testing.T) {
	points := []entry.Point{{
		Package: "example.com/app", Function: "handleOrder",
		File: "orders.go", Line: 42, Method: "POST", Route: "/orders/{id}",
	}}
	checks := [][]rules.Check{{
		{ID: rules.RuleMiddleware, Weight: 30, Pass: false, Applicable: true, Detail: "no middleware"},
		{ID: rules.RuleContext, Weight: 20, Pass: true, Applicable: true},
	}}
	document := report.Build(points, checks, 0, false)

	data, err := report.SARIF(document)
	if err != nil {
		t.Fatalf("SARIF: %v", err)
	}
	var sarif struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &sarif); err != nil {
		t.Fatalf("the SARIF document is not JSON: %v\n%s", err, data)
	}
	if sarif.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", sarif.Version)
	}
	if sarif.Schema == "" {
		t.Error("the document names no $schema")
	}
	if len(sarif.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(sarif.Runs))
	}
	run := sarif.Runs[0]
	if run.Tool.Driver.Name == "" || len(run.Tool.Driver.Rules) == 0 {
		t.Errorf("the driver declares no rules: %+v", run.Tool.Driver)
	}
	if len(run.Results) != 1 {
		t.Fatalf("results = %d, want one per failed rule", len(run.Results))
	}
	result := run.Results[0]
	if result.RuleID != rules.RuleMiddleware || result.Message.Text == "" || result.Level == "" {
		t.Errorf("result = %+v, want a rule id, a message, and a level", result)
	}
	if len(result.Locations) != 1 {
		t.Fatalf("result carries no location: %+v", result)
	}
	location := result.Locations[0].PhysicalLocation
	if location.ArtifactLocation.URI != "orders.go" || location.Region.StartLine != 42 {
		t.Errorf("location = %+v, want orders.go:42", location)
	}
}
