package init_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// TestInit_I5_TakenNameRefuses proves init does not write when the app already
// declares a name the setup would add.
func TestInit_I5_TakenNameRefuses(t *testing.T) {
	dir := chiTree(t)
	main := filepath.Join(dir, "main.go")
	source := readFile(t, main) + "\nfunc NewLogger() {}\n"
	if err := os.WriteFile(main, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir, "--yes"}, io.Discard, &stderr); code == 0 {
		t.Fatal("init exited 0 with NewLogger already declared")
	}
	if _, err := os.Stat(filepath.Join(dir, "wlog_setup.go")); !os.IsNotExist(err) {
		t.Fatal("init wrote wlog_setup.go")
	}
	if !bytes.Contains(stderr.Bytes(), []byte("NewLogger")) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
