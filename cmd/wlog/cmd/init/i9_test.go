package init_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// TestInit_I9_FailedBuildRestoresTheTree proves a failed verify puts the old bytes back.
func TestInit_I9_FailedBuildRestoresTheTree(t *testing.T) {
	dir := chiTree(t)
	mainPath := filepath.Join(dir, "main.go")
	before := readFile(t, mainPath)
	broken := "package main\n\nimport \"missing.example/nope\"\n"
	if err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := wloginit.Run([]string{"--dir", dir, "--yes"}, io.Discard, io.Discard); code == 0 {
		t.Fatal("init exited 0 on a tree that does not build")
	}
	if _, err := os.Stat(filepath.Join(dir, "wlog_setup.go")); !os.IsNotExist(err) {
		t.Fatal("a failed build left wlog_setup.go")
	}
	if got := readFile(t, mainPath); got != before {
		t.Fatalf("main.go was not restored:\n%s", got)
	}
}
