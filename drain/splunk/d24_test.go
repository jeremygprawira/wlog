package splunk_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/drain/splunk"
)

// countJSON counts how many times one event value is encoded.
type countJSON struct {
	n *int
}

// MarshalJSON records one encoding of the value.
func (c countJSON) MarshalJSON() ([]byte, error) {
	*c.n++
	return []byte(`"x"`), nil
}

// TestSplunk_D24_EncodesEachEventOnce shows the open D-24 copies.
// Splunk encodes each event once to measure it and again to send it.
// pathValue and the timestamp parse are still copied across the drains.
func TestSplunk_D24_EncodesEachEventOnce(t *testing.T) {
	var n int
	ev := event()
	ev["marker"] = countJSON{n: &n}

	srv := newFake(t, 200, `{"text":"Success","code":0}`)
	sender, err := splunk.NewSender(splunk.WithURL(srv.URL), splunk.WithToken("token"))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{ev}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if n != 1 {
		t.Errorf("encodings = %d, want 1: the event is encoded to measure the chunk and again to send it", n)
	}

	paths := []struct {
		file string
		snip string
	}{
		{"splunk.go", "\nfunc pathValue("},
		{"../syslog/syslog.go", "\nfunc pathValue("},
		{"../honeycomb/honeycomb.go", "\nfunc pathValue("},
		{"../loki/loki.go", "\nfunc labelPath("},
		{"splunk.go", "time.Parse(time.RFC3339Nano"},
		{"../syslog/syslog.go", "time.Parse(time.RFC3339Nano"},
		{"../newrelic/newrelic.go", "time.Parse(time.RFC3339Nano"},
		{"../cloudwatch/cloudwatch.go", "time.Parse(time.RFC3339Nano"},
		{"../loki/loki.go", "time.Parse(time.RFC3339Nano"},
		{"../clickhouse/clickhouse.go", "time.Parse(time.RFC3339Nano"},
		{"../otlp/mapping.go", "time.Parse(time.RFC3339Nano"},
		{"../sentry/envelope.go", "time.Parse(time.RFC3339Nano"},
		{"../memory/query.go", "time.Parse(time.RFC3339Nano"},
	}
	for _, item := range paths {
		body, err := os.ReadFile(item.file)
		if err != nil {
			t.Fatalf("read %s: %v", item.file, err)
		}
		if strings.Contains(string(body), item.snip) {
			t.Errorf("%s still has %q", item.file, strings.TrimSpace(item.snip))
		}
	}
}
