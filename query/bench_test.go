// This file holds the 1 GB read benchmark of the query package. It writes one 1 GB
// fixture on demand and streams it through the filter, which is the work wlog query does
// on one file.
package query_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremygprawira/wlog/query"
)

// oneGB is the fixture size the acceptance names.
const oneGB = 1 << 30

// fixtureLine is one event line, about 200 bytes.
var fixtureLine = []byte(`{"timestamp":"2026-09-16T08:16:25Z","level":"info","kind":"request","operation":"POST /orders/{id}","duration_ms":12.5,"summary":"POST /orders/{id} 200 in 12.5ms","http":{"status":200,"route":"/orders/{id}"},"trace":{"trace_id":"t-1","request_id":"r-1"}}` + "\n")

// declineLine is the one event the benchmark filter keeps.
var declineLine = []byte(`{"timestamp":"2026-09-16T08:16:25Z","level":"error","kind":"request","operation":"POST /orders/{id}","duration_ms":840.2,"summary":"POST /orders/{id} 502 in 840.2ms: PAYMENT_DECLINED card declined","http":{"status":502,"route":"/orders/{id}"},"error":{"code":"PAYMENT_DECLINED","message":"card declined"},"trace":{"trace_id":"t-1","request_id":"r-1"}}` + "\n")

// BenchmarkQuery1GB streams one 1 GB fixture through the filter. The plan asks for under
// ten seconds on an M-series Mac.
func BenchmarkQuery1GB(b *testing.B) {
	path := writeFixture(b)
	filter, err := query.Compile(query.Options{Levels: []string{"error"}, Text: "declined"})
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}

	b.SetBytes(oneGB)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count, err := stream(path, filter)
		if err != nil {
			b.Fatalf("stream: %v", err)
		}
		if count == 0 {
			b.Fatal("the fixture found no match")
		}
	}
}

// stream reads one file and returns the number of matches.
func stream(path string, filter *query.Filter) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()

	matches := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if filter.Match(event) {
			matches++
		}
	}
	return matches, scanner.Err()
}

// writeFixture writes one 1 GB file, with one match every thousand lines, and returns
// its path.
func writeFixture(b *testing.B) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "events-1gb.ndjson")
	file, err := os.Create(path)
	if err != nil {
		b.Fatalf("create the fixture: %v", err)
	}
	writer := bufio.NewWriterSize(file, 1<<20)
	written := 0
	line := 0
	for written < oneGB {
		var err error
		if line%1000 == 0 {
			_, err = writer.Write(declineLine)
			written += len(declineLine)
		} else {
			_, err = writer.Write(fixtureLine)
			written += len(fixtureLine)
		}
		if err != nil {
			b.Fatalf("write the fixture: %v", err)
		}
		line++
	}
	if err := writer.Flush(); err != nil {
		b.Fatalf("flush the fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		b.Fatalf("close the fixture: %v", err)
	}
	return path
}
