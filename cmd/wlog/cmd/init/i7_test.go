package init_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// TestInit_I7_PrintsOtherAdapterLines proves a non-HTTP adapter is named, not wired.
func TestInit_I7_PrintsOtherAdapterLines(t *testing.T) {
	dir := t.TempDir()
	main := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	goMod := "module example.com/app\n\ngo 1.25.0\n\nrequire github.com/anthropics/anthropic-sdk-go v1.73.0\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir}, &stdout, io.Discard); code != 0 {
		t.Fatalf("init exit %d\n%s", code, stdout.String())
	}
	line := "option.WithMiddleware(wloganthropic.Middleware())"
	if !strings.Contains(stdout.String(), line) {
		t.Fatalf("the plan printed no setup line:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "wloganthropic") && strings.Count(stdout.String(), line) != 1 {
		t.Fatalf("the setup line was written into the file:\n%s", stdout.String())
	}
}
