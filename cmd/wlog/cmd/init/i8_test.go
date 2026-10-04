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

// TestInit_I8_WalksUpToGoMod proves --dir cmd/api finds the go.mod above it.
func TestInit_I8_WalksUpToGoMod(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "cmd", "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	main := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	goMod := "module example.com/api\n\ngo 1.25.0\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir}, io.Discard, &stderr); code != 0 {
		t.Fatalf("init exit %d\n%s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "go.mod") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
