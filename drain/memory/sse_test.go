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
	defer resp.Body.Close()

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
	go resp.Body.Read(buf) // start reading so the server side actually begins streaming

	time.Sleep(50 * time.Millisecond)
	cancel()
	resp.Body.Close()
	time.Sleep(100 * time.Millisecond)
	runtime.GC()

	after := runtime.NumGoroutine()
	if after > before+1 { // small slack for the test runtime itself
		t.Errorf("goroutines before=%d after=%d, suspect a leak", before, after)
	}
}
