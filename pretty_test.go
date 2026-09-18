package wlog_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
)

func TestCore_Format_DefaultEnv_IsJSON(t *testing.T) {
	log := wlog.New()
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
	})
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected JSON output, got %q", out)
	}
}

func TestCore_Format_DevEnv_IsPretty(t *testing.T) {
	log := wlog.New(wlog.WithService("svc", "1.0.0", "dev"))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "order.create")
		end()

		flushWriter(t, log)
	})
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected pretty output in dev, got JSON: %q", out)
	}
	if !strings.Contains(out, "order.create") {
		t.Errorf("pretty output missing operation name: %q", out)
	}
	if !strings.Contains(strings.ToUpper(out), "INFO") {
		t.Errorf("pretty output missing level: %q", out)
	}
}

func TestCore_Format_ExplicitOverridesEnv(t *testing.T) {
	log := wlog.New(wlog.WithService("svc", "1.0.0", "dev"), wlog.WithFormat(wlog.FormatJSON))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
	})
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("WithFormat(FormatJSON) did not override the dev-env default: %q", out)
	}

	log2 := wlog.New(wlog.WithFormat(wlog.FormatPretty))
	out2 := captureStdout(t, func() {
		ctx := log2.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
		flushWriter(t, log2)
	})
	if strings.HasPrefix(strings.TrimSpace(out2), "{") {
		t.Errorf("WithFormat(FormatPretty) did not override the default JSON format: %q", out2)
	}
}

func TestCore_Pretty_IsRedacted(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatPretty))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "login")
		wlog.Set(ctx, "password", "hunter2")
		end()

		flushWriter(t, log)
	})
	if strings.Contains(out, "hunter2") {
		t.Errorf("pretty output leaked an unredacted secret: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("pretty output missing the redaction marker: %q", out)
	}
}

func TestCore_Pretty_ErrorBlock(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatPretty), wlog.WithErrorExtractor(customExtractor{}))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "order.get")
		wlog.Error(ctx, errAny{})
		end()

		flushWriter(t, log)
	})
	if !strings.Contains(out, "no such order id") || !strings.Contains(out, "check the order id") {
		t.Errorf("pretty output missing why/fix: %q", out)
	}
}

type errAny struct{}

func (errAny) Error() string { return "no row" }

func TestCore_Pretty_NoColorRespected(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatPretty))

	t.Setenv("NO_COLOR", "1")
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
	})
	if strings.Contains(out, "\033[") {
		t.Errorf("NO_COLOR=1 but output still has ANSI escapes: %q", out)
	}

	t.Setenv("NO_COLOR", "")
	out2 := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
	})
	// Colors need a terminal, and a captured stdout is a file, so the second run has no
	// escapes either. pretty_internal_test.go proves the colored path instead.
	if strings.Contains(out2, "\033[") {
		t.Errorf("a captured stdout is not a terminal, so the output must stay plain: %q", out2)
	}
}

// blockWriter records each Write call as one block, so a test can prove that one event is
// one write.
type blockWriter struct {
	mu     sync.Mutex
	blocks []string
}

// Write records one block.
func (w *blockWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.blocks = append(w.blocks, string(p))
	return len(p), nil
}

// Blocks returns a copy of the recorded blocks.
func (w *blockWriter) Blocks() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.blocks...)
}

// TestPretty_CORE22_NoInterleave proves that 100 events that end at the same time write
// 100 whole blocks, so the lines of two events never mix.
func TestPretty_CORE22_NoInterleave(t *testing.T) {
	out := &blockWriter{}
	log := wlog.New(wlog.WithFormat(wlog.FormatPretty), wlog.WithWriter(out))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := log.WithContext(context.Background())
			ctx, end := wlog.Start(ctx, fmt.Sprintf("op-%d", i))
			wlog.Set(ctx, "marker", fmt.Sprintf("m-%d", i))
			end()
		}(i)
	}
	wg.Wait()
	if err := log.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	blocks := out.Blocks()
	if len(blocks) != 100 {
		t.Fatalf("the writer received %d blocks, want one per event", len(blocks))
	}
	seen := map[string]bool{}
	for _, block := range blocks {
		lines := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
		if len(lines) != 3 {
			t.Fatalf("block has %d lines, want the summary and the two group lines: %q", len(lines), block)
		}
		marker := ""
		for i := 0; i < 100; i++ {
			if strings.Contains(lines[0], fmt.Sprintf("op-%d ", i)) {
				marker = fmt.Sprintf("m-%d", i)
				break
			}
		}
		if marker == "" {
			t.Fatalf("the first line names no event: %q", block)
		}
		// Every line of the block belongs to this event, and to no other.
		if !strings.HasSuffix(lines[len(lines)-1], "marker="+marker) || seen[marker] {
			t.Errorf("two events interleaved in one block: %q", block)
		}
		seen[marker] = true
	}
	if len(seen) != 100 {
		t.Errorf("blocks name %d events, want 100", len(seen))
	}
}

// TestPretty_PAR9_ErrorFirst proves that the error block follows the summary line, so a
// reader sees why the event failed before any field of it.
func TestPretty_PAR9_ErrorFirst(t *testing.T) {
	out := &lockedBuffer{}
	log := wlog.New(
		wlog.WithFormat(wlog.FormatPretty),
		wlog.WithWriter(out),
		wlog.WithErrorExtractor(customExtractor{}),
	)

	captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "order.get")
		wlog.Error(ctx, errAny{})
		end()

		flushWriter(t, log)
	})

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("pretty output has %d lines, want the summary and the error block: %q", len(lines), out.String())
	}
	want := []string{"  Why:  ", "  Fix:  "}
	for i, prefix := range want {
		if !strings.HasPrefix(lines[i+1], prefix) {
			t.Errorf("line %d = %q, want the error block line %q", i+2, lines[i+1], prefix)
		}
	}
}
