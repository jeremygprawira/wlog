package wlog_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/redact"
)

func TestCore_WithRedactor_Custom(t *testing.T) {
	log := wlog.New(wlog.WithRedactor(redact.MustNew(redact.AddKeys("nik"))))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "nik", "3171010101010001")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if got["nik"] != "[REDACTED]" {
		t.Errorf("nik = %v, want [REDACTED]", got["nik"])
	}
}

func TestCore_SetRedactor_SwapsAtRuntime(t *testing.T) {
	log := wlog.New()
	log.SetRedactor(redact.Disabled())

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "password", "hunter2")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if got["password"] != "hunter2" {
		t.Errorf("password = %v, want hunter2 (redaction disabled)", got["password"])
	}
}

func TestCore_SetRedactor_NilResetsToDefault(t *testing.T) {
	log := wlog.New()
	log.SetRedactor(redact.Disabled())
	log.SetRedactor(nil)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "password", "hunter2")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if got["password"] != "[REDACTED]" {
		t.Errorf("password = %v, want [REDACTED] (nil resets to Default())", got["password"])
	}
}

func TestCore_RedactFingerprint_PresentByDefault(t *testing.T) {
	log := wlog.New()
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if got["redact.fingerprint"] == nil || got["redact.fingerprint"] == "" {
		t.Errorf("redact.fingerprint missing: %v", got)
	}
}

func TestCore_RedactFingerprint_Disabled(t *testing.T) {
	log := wlog.New(wlog.WithRedactFingerprint(false))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if _, ok := got["redact.fingerprint"]; ok {
		t.Errorf("redact.fingerprint present despite WithRedactFingerprint(false): %v", got)
	}
}

func TestCore_SetRedactor_ConcurrentSwapsAndEmits(t *testing.T) {
	defaultFP := redact.Default().Fingerprint()
	disabledFP := redact.Disabled().Fingerprint()

	var mu sync.Mutex
	var events []map[string]any
	log := wlog.New(wlog.WithDrains(wlog.DrainFunc(func(_ context.Context, e map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		cp := make(map[string]any, len(e))
		for k, v := range e {
			cp[k] = v
		}
		events = append(events, cp)
	})))

	// 1000 concurrent JSON lines can exceed the OS pipe buffer, so drain stdout
	// concurrently instead of only after the workload finishes (captureStdout's
	// usual pattern), or emit() would block on a full pipe and the test would hang.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	drained := make(chan struct{})
	go func() {
		io.Copy(io.Discard, r)
		close(drained)
	}()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				log.SetRedactor(redact.Disabled())
			} else {
				log.SetRedactor(redact.Default())
			}
		}(i)
	}
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := log.WithContext(context.Background())
			ctx, end := wlog.Start(ctx, "op")
			wlog.Set(ctx, "password", "hunter2")
			end()
		}()
	}
	wg.Wait()
	os.Stdout = orig
	w.Close()
	<-drained

	mu.Lock()
	defer mu.Unlock()
	for _, e := range events {
		fp := e["redact.fingerprint"]
		switch fp {
		case defaultFP:
			if e["password"] != "[REDACTED]" {
				t.Fatalf("fingerprint says default redactor but password leaked: %v", e)
			}
		case disabledFP:
			if e["password"] != "hunter2" {
				t.Fatalf("fingerprint says disabled redactor but password changed: %v", e)
			}
		default:
			t.Fatalf("unexpected fingerprint: %v", fp)
		}
	}
}
