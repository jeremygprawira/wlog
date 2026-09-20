// Command explain is wlog explain, wlog rules, wlog schema, and wlog version. Every
// answer comes from the source the runtime uses, so a reader never reads a stale copy.
package explain

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog/cmd/wlog/internal/explain"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
	"github.com/jeremygprawira/wlog/internal/version"
	"github.com/jeremygprawira/wlog/schema"
)

// Run runs one of the four commands. It returns 0 for an answer, 1 for an unknown id,
// and 2 for a usage error.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "rules":
		return runRules(args[1:], stdout, stderr)
	case "schema":
		return runSchema(args[1:], stdout, stderr)
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "env":
		return runEnv(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		return runID(args, stdout, stderr)
	}
}

// usage prints the command list.
func usage(out io.Writer) {
	_, _ = fmt.Fprint(out, `usage:
  wlog explain <id> [--json]   a problem code, a doctor code, a rule id, a field, or an env var
  wlog explain env [--json]    every environment variable setup.FromEnv reads
  wlog rules [--json]          every map rule
  wlog schema [event|map]      the embedded JSON Schema
  wlog version [--json]        the tool, rules, and schema versions, and the Go version
`)
}

// runID answers one id, and it names the near ids of an unknown one.
func runID(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("explain", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonFlag := flags.Bool("json", false, "print one JSON document")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	entry, ok := explain.Find(args[0])
	if !ok {
		_, _ = fmt.Fprintf(stderr, "wlog explain: unknown id %q\n", args[0])
		if closest := explain.Closest(args[0], 3); len(closest) > 0 {
			_, _ = fmt.Fprintf(stderr, "did you mean %s?\n", strings.Join(closest, ", "))
		}
		return 1
	}
	if *jsonFlag {
		return writeJSON(stdout, entry, stderr)
	}
	_, _ = fmt.Fprintf(stdout, "%s (%s)\n", entry.ID, entry.Kind)
	for _, section := range []struct{ name, text string }{
		{"What", entry.What}, {"Why", entry.Why}, {"Fix", entry.Fix},
		{"Example", entry.Example}, {"Link", entry.Link},
	} {
		if section.text != "" {
			_, _ = fmt.Fprintf(stdout, "%s: %s\n", section.name, section.text)
		}
	}
	return 0
}

// runEnv lists every environment variable the runtime reads, and no value.
func runEnv(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("explain env", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonFlag := flags.Bool("json", false, "print one JSON document")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	entries := []explain.Entry{}
	for _, entry := range explain.All() {
		if entry.Kind == "env" {
			entries = append(entries, entry)
		}
	}
	if *jsonFlag {
		return writeJSON(stdout, entries, stderr)
	}
	for _, entry := range entries {
		_, _ = fmt.Fprintf(stdout, "%s\t%s\n", entry.ID, entry.What)
	}
	return 0
}

// runRules lists every map rule with its weight and its fix.
func runRules(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("rules", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonFlag := flags.Bool("json", false, "print one JSON document")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	type ruleRow struct {
		ID     string `json:"id"`
		Weight int    `json:"weight"`
		Fix    string `json:"fix"`
		Link   string `json:"link"`
	}
	rows := []ruleRow{}
	for _, rule := range rules.Order() {
		info := rules.Infos[rule.ID]
		rows = append(rows, ruleRow{
			ID: rule.ID, Weight: rule.Weight, Fix: info.Fix,
			Link: "https://github.com/jeremygprawira/wlog/blob/main/docs/rules.md#" + info.Docs,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Weight > rows[j].Weight })
	if *jsonFlag {
		return writeJSON(stdout, rows, stderr)
	}
	for _, row := range rows {
		_, _ = fmt.Fprintf(stdout, "%s\t%d\t%s\n", row.ID, row.Weight, row.Fix)
	}
	return 0
}

// runSchema prints one embedded JSON Schema.
func runSchema(args []string, stdout, stderr io.Writer) int {
	name := "event"
	if len(args) > 0 {
		name = args[0]
	}
	switch name {
	case "event":
		_, _ = stdout.Write(schema.EventV1())
	case "map":
		_, _ = stdout.Write(schema.MapV2())
	default:
		_, _ = fmt.Fprintf(stderr, "wlog schema: %q is not event or map\n", name)
		return 2
	}
	return 0
}

// runVersion prints the tool, rules, and schema versions, and the Go version.
func runVersion(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonFlag := flags.Bool("json", false, "print one JSON document")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	document := map[string]any{
		"tool":         version.Version,
		"rules":        rules.Version,
		"event_schema": 2,
		"map_schema":   2,
		"go":           runtime.Version(),
	}
	if *jsonFlag {
		return writeJSON(stdout, document, stderr)
	}
	_, _ = fmt.Fprintf(stdout, "tool %s\nrules %d\nevent schema 2\nmap schema 2\ngo %s\n",
		version.Version, rules.Version, runtime.Version())
	return 0
}

// writeJSON writes one JSON document.
func writeJSON(out io.Writer, value any, stderr io.Writer) int {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		_, _ = fmt.Fprintf(stderr, "wlog explain: %v\n", err)
		return 2
	}
	return 0
}
