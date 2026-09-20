// This file checks the validator extractor: the field, tag, and parameter of each
// failure, the fix line, the internal detail, and the rule that no rejected value ever
// reaches the event.
package wlogvalidator_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"

	wlogvalidator "github.com/jeremygprawira/wlog/errors/validator"
)

// order is one test struct with two failing fields.
type order struct {
	Email string `validate:"required,email"`
	Total int    `validate:"min=100"`
}

// TestValidator_C7_Fields proves the code, the kind, the status, and the fields of one
// validation failure.
func TestValidator_C7_Fields(t *testing.T) {
	err := validator.New().Struct(order{Email: "nope", Total: 5})
	info := wlogvalidator.Extractor(
		wlogvalidator.WithFixes(map[string]string{"email": "Use a real address"}),
	).Extract(err)

	if info.Code != "VALIDATION_FAILED" || info.Kind != "validation" || info.Status != 400 {
		t.Errorf("code/kind/status = %s/%s/%d, want VALIDATION_FAILED/validation/400", info.Code, info.Kind, info.Status)
	}
	if info.Message != "validation failed on 2 fields" {
		t.Errorf("message = %q, want the field count", info.Message)
	}
	if info.Fix != "Use a real address" {
		t.Errorf("fix = %q, want the tag's fix", info.Fix)
	}
	fields, _ := info.Data["fields"].([]any)
	if len(fields) != 2 {
		t.Fatalf("fields = %v, want two", info.Data["fields"])
	}
	first, _ := fields[0].(map[string]any)
	if first["field"] != "Email" || first["tag"] != "email" {
		t.Errorf("field 0 = %v, want Email with the email tag", first)
	}
	second, _ := fields[1].(map[string]any)
	if second["field"] != "Total" || second["tag"] != "min" || second["param"] != "100" {
		t.Errorf("field 1 = %v, want Total with min 100", second)
	}
	internal, _ := info.Internal["fields"].([]any)
	if len(internal) != 2 {
		t.Errorf("internal fields = %v, want two", info.Internal["fields"])
	}
}

// TestValidator_C7_NoValueLeaks proves that a rejected value never reaches the info.
func TestValidator_C7_NoValueLeaks(t *testing.T) {
	type login struct {
		Password string `validate:"required,min=12"`
	}
	err := validator.New().Struct(login{Password: "hunter2"})
	info := wlogvalidator.Extractor().Extract(err)

	body, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "hunter2") {
		t.Errorf("the rejected value reached the info: %s", body)
	}
}

// TestValidator_C7_InvalidValidationError proves that a bad input type is an internal
// fault, not a validation failure.
func TestValidator_C7_InvalidValidationError(t *testing.T) {
	err := validator.New().Struct("not a struct")
	info := wlogvalidator.Extractor().Extract(err)
	if info.Code != "INTERNAL" || info.Kind != "internal" || info.Status != 500 {
		t.Errorf("code/kind/status = %s/%s/%d, want INTERNAL/internal/500", info.Code, info.Kind, info.Status)
	}
}

// TestValidator_C7_PlainError proves that another error type is not claimed.
func TestValidator_C7_PlainError(t *testing.T) {
	if info := wlogvalidator.Extractor().Extract(errPlain{}); info.Code != "" {
		t.Errorf("code = %q, want an empty info for a plain error", info.Code)
	}
}

// errPlain is a plain error.
type errPlain struct{}

// Error returns the message.
func (errPlain) Error() string { return "plain" }
