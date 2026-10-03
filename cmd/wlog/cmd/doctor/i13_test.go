package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/internal/adapters"
)

// TestDoctor_I13_AdapterLinesFollowTheTable proves adapter lines follow the table, not a map.
func TestDoctor_I13_AdapterLinesFollowTheTable(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n")
	var want []string
	for _, adapter := range adapters.Table {
		if adapter.Setup == "" {
			continue
		}
		b.WriteString("\t_ \"" + adapter.Wlog + "\"\n")
		want = append(want, adapter.Wlog)
		if len(want) == 8 {
			break
		}
	}
	b.WriteString(")\n\nfunc main() {}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, check := range adapterChecks(dir) {
		for _, path := range want {
			if strings.HasPrefix(check.Message, path+" ") {
				got = append(got, path)
			}
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
