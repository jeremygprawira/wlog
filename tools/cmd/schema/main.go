// Command schema validates every golden document of the repository against the JSON
// Schema of the shape it claims to be.
//
// A schema that nothing checks is a document nobody reads, and a golden that drifts from
// its schema is a promise broken quietly. This command compiles the schemas of the schema
// package and validates each golden with the library the plan names, so a wrong type, a
// missing required key, or a key that no table declares fails the build.
//
// Run it from the tools directory: go run ./cmd/schema
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// eventGoldens is the directory that holds one event golden per kind.
const eventGoldens = "testdata/shape"

// mapGoldens lists the documents that follow the map report shape.
var mapGoldens = []string{"wlog.map.json", "cmd/wlog/report/testdata/map_v2.json"}

// main validates every golden, prints one line per document that breaks its schema, and
// exits 1 when any of them does.
func main() {
	dir := flag.String("dir", "", "the workspace root to check, which defaults to the one above the tools module")
	flag.Parse()

	root := *dir
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			fail(err)
		}
		root, err = workspace.FindRoot(wd)
		if err != nil {
			fail(err)
		}
	}

	found, err := check(root)
	if err != nil {
		fail(err)
	}
	for _, line := range found {
		fmt.Println(line)
	}
	if len(found) > 0 {
		fmt.Fprintf(os.Stderr, "schema: %d document(s) break their schema\n", len(found))
		os.Exit(1)
	}
}

// fail prints an error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "schema:", err)
	os.Exit(1)
}

// check validates every golden document under root, and returns one line per document that
// breaks its schema.
func check(root string) ([]string, error) {
	events, err := compile(filepath.Join(root, "schema", "event.v1.json"))
	if err != nil {
		return nil, err
	}
	maps, err := compile(filepath.Join(root, "schema", "map.v2.json"))
	if err != nil {
		return nil, err
	}

	kinds, err := filepath.Glob(filepath.Join(root, eventGoldens, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(kinds)

	documents := make([]document, 0, len(kinds)+len(mapGoldens))
	for _, path := range kinds {
		documents = append(documents, document{path: path, schema: events})
	}
	for _, name := range mapGoldens {
		documents = append(documents, document{path: filepath.Join(root, name), schema: maps})
	}

	var found []string
	for _, doc := range documents {
		value, err := decode(doc.path)
		if err != nil {
			return nil, err
		}
		if err := doc.schema.Validate(value); err != nil {
			found = append(found, fmt.Sprintf("%s: %s", relative(root, doc.path), summary(err.Error())))
		}
	}
	return found, nil
}

// document is one golden file and the schema it must satisfy.
type document struct {
	path   string
	schema *jsonschema.Schema
}

// compile builds one schema, with format assertions on, so a bad date or a bad UUID fails.
func compile(path string) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	schema, err := compiler.Compile(path)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", path, err)
	}
	return schema, nil
}

// decode reads one JSON document into a tree of maps, slices, and numbers.
func decode(path string) (any, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeBytes(body)
}

// decodeBytes reads one JSON document from memory. Numbers keep their text, so a large
// integer does not lose its last digits on the way in.
func decodeBytes(body []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// relative names a path from the workspace root, so a finding reads the same on any machine.
func relative(root, path string) string {
	name, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return name
}

// summary compacts one validation error into a single line that names each field which
// broke, so a finding reads in a log and names what to fix.
func summary(text string) string {
	parts := make([]string, 0, 4)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if line == "" || strings.HasPrefix(line, "jsonschema validation failed") {
			continue
		}
		parts = append(parts, line)
	}
	if len(parts) > 4 {
		parts = append(parts[:4], "...")
	}
	return strings.Join(parts, "; ")
}
