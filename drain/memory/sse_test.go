package memory_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/drain/memory"
)

func TestSSE_ReplayThenLive(t *testing.T) {
	m := memory.New(10)
	for i := 1; i <= 5; i++ {
		send(m, i)
	}

	srv := httptest.NewServer(m.SSEHandler())
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"?replay=2", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	var lines []string
	dataLines := func() []string {
		var out []string
		for _, l := range lines {
			if strings.HasPrefix(l, "data: ") {
				out = append(out, l)
			}
		}
		return out
	}

	// Read the two replayed events.
	for len(dataLines()) < 2 {
		if !scanner.Scan() {
			t.Fatalf("stream ended early reading replay: %v", scanner.Err())
		}
		lines = append(lines, scanner.Text())
	}
	replayed := dataLines()
	if !strings.Contains(replayed[0], `"i":4`) || !strings.Contains(replayed[1], `"i":5`) {
		t.Errorf("replayed lines = %v, want i=4 then i=5", replayed)
	}

	// A live event sent after connecting must also arrive.
	send(m, 6)
	for len(dataLines()) < 3 {
		if !scanner.Scan() {
			t.Fatalf("stream ended early reading live event: %v", scanner.Err())
		}
		lines = append(lines, scanner.Text())
	}
	if !strings.Contains(dataLines()[2], `"i":6`) {
		t.Errorf("live line = %v, want i=6", dataLines()[2])
	}
}

func TestSSE_NoGoroutineLeakAfterDisconnect(t *testing.T) {
	m := memory.New(10)
	srv := httptest.NewServer(m.SSEHandler())
	defer srv.Close()

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	buf := make([]byte, 1)
	go func() { _, _ = resp.Body.Read(buf) }() // start reading so the server side actually begins streaming

	time.Sleep(50 * time.Millisecond)
	cancel()
	_ = resp.Body.Close()
	time.Sleep(100 * time.Millisecond)
	runtime.GC()

	after := runtime.NumGoroutine()
	if after > before+1 { // small slack for the test runtime itself
		t.Errorf("goroutines before=%d after=%d, suspect a leak", before, after)
	}
}

// TestMemory_SSE_V2Frames proves the hello frame and the since replay of the v2 stream.
func TestMemory_SSE_V2Frames(t *testing.T) {
	m := memory.New(10)
	at := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	sendEvent(t, m, "info", at, map[string]any{"event_id": "e-1"})
	sendEvent(t, m, "error", at.Add(time.Second), map[string]any{"event_id": "e-2"})

	server := httptest.NewServer(m.StreamHandler(memory.WithAnyAddress()))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?since=e-1", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	reader := bufio.NewReader(response.Body)

	if line := readFrameLine(t, reader); line != "event: hello" {
		t.Fatalf("first frame = %q, want the hello frame", line)
	}
	if data := readFrameLine(t, reader); !strings.Contains(data, `"size":2`) {
		t.Errorf("hello data = %q, want the store size", data)
	}
	if blank := readFrameLine(t, reader); blank != "" {
		t.Fatalf("frame separator = %q, want a blank line", blank)
	}
	if line := readFrameLine(t, reader); line != "event: event" {
		t.Fatalf("second frame = %q, want the replayed event", line)
	}
	if data := readFrameLine(t, reader); !strings.Contains(data, "e-2") || strings.Contains(data, "e-1") {
		t.Errorf("event data = %q, want only the event after e-1", data)
	}
}

// readFrameLine reads one line of the SSE stream, and it fails the test after a timeout.
func readFrameLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read the stream: %v", err)
	}
	return strings.TrimRight(line, "\n")
}
