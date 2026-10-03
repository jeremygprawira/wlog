package init_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// TestInit_I4_V1FileRefuses proves a tree that already has wlog.go is left alone.
func TestInit_I4_V1FileRefuses(t *testing.T) {
	dir := chiTree(t)
	body := "package main\n\nfunc NewLogger() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "wlog.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir, "--yes"}, io.Discard, &stderr); code == 0 {
		t.Fatal("init wrote a second setup over a v1 tree")
	}
	if _, err := os.Stat(filepath.Join(dir, "wlog_setup.go")); !os.IsNotExist(err) {
		t.Fatal("init wrote wlog_setup.go beside wlog.go")
	}
	if !bytes.Contains(stderr.Bytes(), []byte("wlog.go")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
