package init_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// TestInit_I11_SkipsIndirectAndReplaceBlocks proves an indirect require and a replace
// block do not count as adapters, and the plan has no installed field.
func TestInit_I11_SkipsIndirectAndReplaceBlocks(t *testing.T) {
	dir := t.TempDir()
	main := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	goMod := `module example.com/app

go 1.25.0

require github.com/gin-gonic/gin v1.12.0 // indirect

require github.com/anthropics/anthropic-sdk-go v1.73.0

replace (
	github.com/go-chi/chi/v5 v5.3.2 => github.com/go-chi/chi/v5 v5.3.2
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir, "--json"}, &stdout, io.Discard); code != 0 {
		t.Fatalf("init exit %d\n%s", code, stdout.String())
	}
	if strings.Contains(stdout.String(), `"installed"`) {
		t.Fatalf("the plan still has installed:\n%s", stdout.String())
	}
	var plan v2Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, adapter := range plan.Adapters {
		names[adapter.Name] = true
	}
	if names["http-gin"] || names["http-chi"] {
		t.Fatalf("indirect or replace modules were reported: %+v", plan.Adapters)
	}
	if !names["ai-anthropic"] {
		t.Fatalf("the direct require was dropped: %+v", plan.Adapters)
	}
}
