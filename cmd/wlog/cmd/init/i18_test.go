package init_test

import (
	"io"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// TestInit_I18_DryRunIsNotAFlag proves --dry-run is not a second way to skip the write.
func TestInit_I18_DryRunIsNotAFlag(t *testing.T) {
	dir := chiTree(t)
	if code := wloginit.Run([]string{"--dir", dir, "--dry-run"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("init --dry-run exit %d, want 2", code)
	}
}
