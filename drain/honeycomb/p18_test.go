package honeycomb

import (
	"os"
	"strings"
	"testing"
)

// TestHoneycomb_P18_DoesNotCacheTheClient shows the P-18 duplicates. The client cache
// keeps a client that costs nothing to build, and the local helpers repeat code that
// already has one home.
func TestHoneycomb_P18_DoesNotCacheTheClient(t *testing.T) {
	sender, _, err := newSender(WithAPIKey("k"))
	if err != nil {
		t.Fatalf("newSender: %v", err)
	}
	first := sender.clientFor("payments")
	second := sender.clientFor("payments")
	if first == second {
		t.Error("clientFor returned one cached client, but building a client costs nothing")
	}

	files := []string{
		"honeycomb.go",
		"../newrelic/newrelic.go",
		"../elastic/elastic.go",
		"../betterstack/betterstack.go",
		"../splunk/splunk.go",
	}
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(body)
		if strings.Contains(text, "\nfunc flatten(") {
			t.Errorf("%s still has its own flatten, which repeats preset.Flat().Apply", path)
		}
		if strings.Contains(text, "\nfunc firstEnv(") {
			t.Errorf("%s still has its own firstEnv", path)
		}
		if strings.Contains(text, "func (s *Sender) chunkEnd(") {
			t.Errorf("%s still has its own chunkEnd", path)
		}
	}
}
