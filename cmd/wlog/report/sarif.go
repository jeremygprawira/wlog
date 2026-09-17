package report

import (
	"encoding/json"
	"fmt"

	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// sarifSchema is the schema uri every SARIF 2.1.0 document names, so a tool that reads one knows
// how to read it.
const sarifSchema = "https://json.schemastore.org/sarif-2.1.0.json"

// sarifLevel maps a rule's severity to the level SARIF understands. A rule that costs points is a
// warning: the code runs, and the tool warns about what it cannot see.
const sarifLevel = "warning"

// SARIF renders the map as SARIF 2.1.0, the format GitHub code scanning reads. One result per
// failed rule, each with its file and line, so a review shows the finding on the code.
func SARIF(m Map) ([]byte, error) {
	document := sarifDocument{
		Schema:  sarifSchema,
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool:    sarifTool{Driver: sarifDriver{Name: "wlog map", Version: ToolVersion, Rules: sarifRules()}},
			Results: sarifResults(m),
		}},
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("report: sarif: %w", err)
	}
	return append(data, '\n'), nil
}

// sarifRules declares the rules a reader can look up, one entry per rule in the fixed order.
func sarifRules() []sarifRule {
	list := rules.Order()
	out := make([]sarifRule, 0, len(list))
	for _, rule := range list {
		fix, docs := rules.FixFor(rule.ID)
		out = append(out, sarifRule{
			ID:               rule.ID,
			ShortDescription: sarifText{Text: fix},
			HelpURI:          docs,
		})
	}
	return out
}

// sarifResults turns every failed, unsuppressed rule into one result.
func sarifResults(m Map) []sarifResult {
	results := make([]sarifResult, 0)
	for _, handler := range m.Handlers {
		for _, check := range handler.Checks {
			if !check.Applicable || check.Pass || check.Suppressed {
				continue
			}
			results = append(results, sarifResult{
				RuleID:  check.ID,
				Level:   sarifLevel,
				Message: sarifText{Text: check.Detail},
				Locations: []sarifLocation{{
					PhysicalLocation: sarifPhysicalLocation{
						ArtifactLocation: sarifArtifactLocation{URI: handler.File},
						Region:           sarifRegion{StartLine: handler.Line},
					},
				}},
			})
		}
	}
	return results
}

// The SARIF document shape, named field by field so the JSON keys are exactly the schema's.
type sarifDocument struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name    string      `json:"name"`
	Version string      `json:"version"`
	Rules   []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string    `json:"id"`
	ShortDescription sarifText `json:"shortDescription"`
	HelpURI          string    `json:"helpUri"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}
