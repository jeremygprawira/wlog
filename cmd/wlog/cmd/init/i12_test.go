package init_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// TestInit_I12_JSONStaysOnStdout proves --json --yes leaves stdout as JSON only.
func TestInit_I12_JSONStaysOnStdout(t *testing.T) {
	dir := chiTree(t)
	var stdout, stderr bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir, "--json", "--yes"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init exit %d\n%s", code, stderr.String())
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var plan v2Plan
	if err := dec.Decode(&plan); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	rest, err := io.ReadAll(dec.Buffered())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(rest)) != "" || strings.Contains(stdout.String(), "wrote") {
		t.Fatalf("text after the JSON:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "wrote") {
		t.Fatalf("progress did not move to stderr:\n%s", stderr.String())
	}
}
