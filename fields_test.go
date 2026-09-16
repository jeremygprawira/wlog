package wlog_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/redact"
)

func TestCore_Fields_DefaultIsNamespaced(t *testing.T) {
	log := wlog.New(wlog.WithService("go-customer", "1.0.0", "prod"))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	svc, ok := got["service"].(map[string]any)
	if !ok || svc["name"] != "go-customer" {
		t.Errorf("default shape: service = %v, want nested {name version env}", got["service"])
	}
	if _, ok := got["service.name"]; ok {
		t.Error("default shape should not have a flat service.name key")
	}
}

func TestCore_Fields_Flat_UnnestsService(t *testing.T) {
	log := wlog.New(wlog.WithService("go-customer", "1.0.0", "prod"), wlog.WithFieldNames(wlog.FieldsFlat()))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if got["service"] != "go-customer" || got["version"] != "1.0.0" || got["environment"] != "prod" {
		t.Errorf("flat shape = %v", got)
	}
}

func TestCore_Fields_OTel_UsesResourceNames(t *testing.T) {
	log := wlog.New(wlog.WithService("go-customer", "1.0.0", "prod"), wlog.WithFieldNames(wlog.FieldsOTel()))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if got["service.name"] != "go-customer" || got["deployment.environment"] != "prod" {
		t.Errorf("otel shape = %v", got)
	}
}

func TestCore_Fields_WithFieldNames_RenamesOneKey(t *testing.T) {
	log := wlog.New(wlog.WithFieldNames(wlog.FieldNames{"operation": "op"}))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "checkout")
		end()
	})

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if got["op"] != "checkout" {
		t.Errorf("op = %v, want checkout", got["op"])
	}
	if _, ok := got["operation"]; ok {
		t.Error("operation should have been renamed away")
	}
	if _, ok := got["level"]; !ok {
		t.Error("level should be untouched by a rename that doesn't mention it")
	}
}

func TestCore_Fields_DenylistMatchesCanonicalNameBeforeRename(t *testing.T) {
	log := wlog.New(
		wlog.WithService("go-customer", "1.0.0", "prod"),
		wlog.WithRedactor(redact.MustNew(redact.AddKeys("service.env"))),
		wlog.WithFieldNames(wlog.FieldsFlat()),
	)
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if got["environment"] != "[REDACTED]" {
		t.Errorf("environment = %v, want [REDACTED] (redaction runs on the canonical name before rename)", got["environment"])
	}
}
