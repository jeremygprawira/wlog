package init_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// TestInit_I6_LoggerUsesEnvironment proves the generated logger does not pin env to local.
func TestInit_I6_LoggerUsesEnvironment(t *testing.T) {
	dir := chiTree(t)
	var stdout bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir}, &stdout, io.Discard); code != 0 {
		t.Fatalf("init exit %d\n%s", code, stdout.String())
	}
	if strings.Contains(stdout.String(), "WithService") {
		t.Fatalf("the plan still pins the service:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "setup.FromEnv()") {
		t.Fatalf("the plan dropped FromEnv:\n%s", stdout.String())
	}
}
