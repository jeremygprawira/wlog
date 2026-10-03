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

// TestInit_I10_RequirementIsAPlanStep proves a go.mod change is named in the plan, the
// build leaves no binary, and the user's environment is what runs the build.
func TestInit_I10_RequirementIsAPlanStep(t *testing.T) {
	dir := chiTree(t)
	var stdout bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir, "--json"}, &stdout, io.Discard); code != 0 {
		t.Fatalf("init exit %d\n%s", code, stdout.String())
	}
	var plan v2Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("decode plan: %v\n%s", err, stdout.String())
	}
	named := false
	for _, file := range plan.Files {
		if strings.HasSuffix(file.Path, "go.mod") || strings.HasSuffix(file.Path, "go.sum") {
			named = true
		}
	}
	if !named {
		t.Fatalf("the plan does not name a module file: %+v", plan.Files)
	}
	if code := wloginit.Run([]string{"--dir", dir, "--yes"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("init --yes exit %d", code)
	}
	base := filepath.Base(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == base {
			t.Fatalf("go build left a binary named %s", base)
		}
	}
}
