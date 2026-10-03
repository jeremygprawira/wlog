package adapters_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/internal/adapters"
)

// TestAdapters_I14_RowsMatchTheWorkspace reads go.work and proves each adapter module has
// a row, and that the directory and the function of each row exist.
func TestAdapters_I14_RowsMatchTheWorkspace(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	work, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		t.Fatal(err)
	}
	skip := map[string]bool{
		".": true, "./examples": true, "./examples/mux": true, "./cmd/wlog": true,
		"./tools": true, "./store/sql/drivertest": true,
		// drain/cloudwatch has no row yet. I-16 adds it.
		"./drain/cloudwatch": true,
	}
	rows := map[string]adapters.Adapter{}
	for _, adapter := range adapters.Table {
		rel := strings.TrimPrefix(adapter.Wlog, "github.com/jeremygprawira/wlog")
		if rel == "" {
			rel = "."
		} else {
			rel = "." + rel
		}
		rows[rel] = adapter
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("row %s has no directory %s", adapter.Name, rel)
		}
		for _, name := range setupFuncs(adapter.Setup) {
			if !dirHasFunc(filepath.Join(root, rel), name) {
				t.Errorf("row %s names %s, and that function is not in %s", adapter.Name, name, rel)
			}
		}
	}
	for _, line := range strings.Split(string(work), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "./") && line != "." {
			continue
		}
		if strings.Contains(line, "=>") {
			continue
		}
		path := strings.TrimSuffix(line, ")")
		if skip[path] {
			continue
		}
		if _, ok := rows[path]; !ok {
			t.Errorf("go.work module %s has no adapter row", path)
		}
	}
}

var setupFunc = regexp.MustCompile(`wlog[A-Za-z0-9]+\.([A-Z][A-Za-z0-9]*)`)

func setupFuncs(setup string) []string {
	var names []string
	for _, match := range setupFunc.FindAllStringSubmatch(setup, -1) {
		names = append(names, match[1])
	}
	return names
}

func dirHasFunc(dir, name string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	needle := "func " + name
	method := "func ("
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		text := string(data)
		if strings.Contains(text, needle+"(") || strings.Contains(text, needle+"[") || strings.Contains(text, needle+" ") {
			return true
		}
		if strings.Contains(text, method) && strings.Contains(text, ") "+name+"(") {
			return true
		}
	}
	return false
}
