// This file checks the cockroachdb/errors extractor: the telemetry code, the domain, the
// stack, the issue link, the safe details, the hint and detail switches, and the rule
// that a value marked unsafe never reaches the redacted message.
package wlogcockroach_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"

	wlogcockroach "github.com/jeremygprawira/wlog/errors/cockroach"
)

// TestCockroach_C9_Fields proves the code, the kind, the link, the stack, and the safe
// details of one wrapped error.
func TestCockroach_C9_Fields(t *testing.T) {
	base := errors.New("the order is not payable")
	err := errors.WithStack(
		errors.WithDomain(
			errors.WithTelemetry(
				errors.WithIssueLink(base, errors.IssueLink{IssueURL: "https://issues.example/1"}),
				"ORDER_NOT_PAYABLE",
			),
			errors.Domain("orders"),
		),
	)
	info := wlogcockroach.Extractor().Extract(err)

	if info.Code != "ORDER_NOT_PAYABLE" {
		t.Errorf("code = %q, want ORDER_NOT_PAYABLE", info.Code)
	}
	if info.Kind != "orders" {
		t.Errorf("kind = %q, want orders", info.Kind)
	}
	if info.Link != "https://issues.example/1" {
		t.Errorf("link = %q, want the issue URL", info.Link)
	}
	if !strings.Contains(info.Message, "the order is not payable") {
		t.Errorf("message = %q, want the error text", info.Message)
	}
	if !strings.Contains(info.Stack, "cockroach_test.go") {
		t.Errorf("stack = %q, want the test frame", info.Stack)
	}
	keys, _ := info.Internal["telemetry_keys"].([]string)
	if len(keys) != 1 || keys[0] != "ORDER_NOT_PAYABLE" {
		t.Errorf("telemetry_keys = %v, want the one key", info.Internal["telemetry_keys"])
	}
	for _, payload := range info.Internal["safe_details"].([]any) {
		detail, _ := payload.(map[string]any)
		if name, _ := detail["type"].(string); strings.Contains(name, "stack") {
			t.Errorf("safe_details kept the stack entry: %v", detail)
		}
	}
	if info.Fix != "" {
		t.Errorf("fix = %q, want empty without WithHints", info.Fix)
	}
}

// TestCockroach_C9_Switches proves that WithHints and WithDetails add the hint and the
// detail, and that the defaults leave both out.
func TestCockroach_C9_Switches(t *testing.T) {
	err := errors.WithDetail(
		errors.WithHint(errors.New("the order is not payable"), "Ask the customer for another card."),
		"order_id=A-1",
	)
	plain := wlogcockroach.Extractor().Extract(err)
	if plain.Fix != "" || plain.Internal["details"] != nil {
		t.Errorf("fix/details = %q/%v, want both empty by default", plain.Fix, plain.Internal["details"])
	}
	full := wlogcockroach.Extractor(wlogcockroach.WithHints(), wlogcockroach.WithDetails()).Extract(err)
	if full.Fix != "Ask the customer for another card." {
		t.Errorf("fix = %q, want the hint", full.Fix)
	}
	details, _ := full.Internal["details"].([]string)
	if len(details) != 1 || details[0] != "order_id=A-1" {
		t.Errorf("details = %v, want order_id=A-1", full.Internal["details"])
	}
}

// TestCockroach_C9_Redacted proves that the message hides a value that the library
// marked as unsafe, and that WithUnredactedMessage keeps it.
func TestCockroach_C9_Redacted(t *testing.T) {
	err := errors.Newf("card %s failed", "4111111111111111")
	info := wlogcockroach.Extractor().Extract(err)
	body, marshalErr := json.Marshal(info)
	if marshalErr != nil {
		t.Fatalf("marshal: %v", marshalErr)
	}
	if strings.Contains(string(body), "4111111111111111") {
		t.Errorf("the card number reached the redacted info: %s", body)
	}
	raw := wlogcockroach.Extractor(wlogcockroach.WithUnredactedMessage()).Extract(err)
	if !strings.Contains(raw.Message, "4111111111111111") {
		t.Errorf("message = %q, want the raw text with WithUnredactedMessage", raw.Message)
	}
}

// TestCockroach_C9_PlainError proves that a plain error still fills the message and the
// stack, with no code and no kind.
func TestCockroach_C9_PlainError(t *testing.T) {
	info := wlogcockroach.Extractor().Extract(errors.New("plain"))
	if info.Code != "" || info.Kind != "" {
		t.Errorf("code/kind = %q/%q, want both empty", info.Code, info.Kind)
	}
	if info.Message != "plain" {
		t.Errorf("message = %q, want plain", info.Message)
	}
	if info.Stack == "" {
		t.Errorf("stack is empty, want the caller frame")
	}
}
