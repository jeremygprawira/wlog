package init_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// TestInit_I14_NoYesWritesNothing proves a run without --yes leaves the tree alone.
func TestInit_I14_NoYesWritesNothing(t *testing.T) {
	dir := chiTree(t)
	if code := wloginit.Run([]string{"--dir", dir}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("init exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "wlog_setup.go")); !os.IsNotExist(err) {
		t.Fatal("a run without --yes wrote wlog_setup.go")
	}
}
