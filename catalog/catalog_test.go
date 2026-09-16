package catalog_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jeremygprawira/wlog/catalog"
)

// invoiceEntry is the entry the tests register.
func invoiceEntry() catalog.Entry {
	return catalog.Entry{
		Code:    "not_found",
		Kind:    "not_found",
		Status:  404,
		Message: "invoice {id} was not found",
		Why:     "no invoice has that id",
		Fix:     "check the id and retry",
		Link:    "https://example.com/docs/invoice",
		Audit:   &catalog.Audit{Action: "invoice.read", Severity: "low"},
	}
}

// TestCatalog_CodesAndGet proves codes are prefixed and sorted, and that Get accepts
// both the short and the full spelling.
func TestCatalog_CodesAndGet(t *testing.T) {
	invoice := catalog.New("invoice", invoiceEntry())
	auth := catalog.New("auth", catalog.Entry{Code: "invalid_token", Status: 401})

	codes := invoice.Codes()
	if len(codes) != 1 || codes[0] != "INVOICE_NOT_FOUND" {
		t.Errorf("Codes() = %v, want [INVOICE_NOT_FOUND]", codes)
	}
	if invoice.Prefix() != "INVOICE" {
		t.Errorf("Prefix() = %q, want INVOICE", invoice.Prefix())
	}

	short, ok := invoice.Get("not_found")
	if !ok || short.Status != 404 {
		t.Errorf("Get(short) = %+v, %v", short, ok)
	}
	full, ok := invoice.Get("INVOICE_NOT_FOUND")
	if !ok || full.Message != short.Message {
		t.Errorf("Get(full) = %+v, %v, want the same entry", full, ok)
	}
	if _, ok := invoice.Get("other.NOT_FOUND"); ok {
		t.Error("Get matched a code from another prefix")
	}

	// The two registries must not collide.
	if auth.Codes()[0] == invoice.Codes()[0] {
		t.Errorf("two prefixes produced the same code: %v", auth.Codes()[0])
	}
}

// TestCatalog_PanicsOnBadEntries proves a duplicate or empty code fails at startup.
func TestCatalog_PanicsOnBadEntries(t *testing.T) {
	assertPanics(t, "duplicate", func() {
		catalog.New("app",
			catalog.Entry{Code: "dup"},
			catalog.Entry{Code: "dup"},
		)
	})
	assertPanics(t, "empty", func() {
		catalog.New("app", catalog.Entry{Code: ""})
	})
}

func assertPanics(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", name)
		}
	}()
	fn()
}

// TestCatalog_EntryIsCopied proves a caller's later change cannot reach the registry.
func TestCatalog_EntryIsCopied(t *testing.T) {
	entry := invoiceEntry()
	reg := catalog.New("invoice", entry)
	entry.Audit.Severity = "critical"
	entry.Status = 500

	got, _ := reg.Get("not_found")
	if got.Status != 404 || got.Audit == nil || got.Audit.Severity != "low" {
		t.Errorf("registry entry changed with the caller's copy: %+v", got)
	}
}

// TestCatalog_ErrTemplate proves the template renders params and leaves a missing one
// in place.
func TestCatalog_ErrTemplate(t *testing.T) {
	reg := catalog.New("invoice", invoiceEntry())

	err := reg.Err("not_found", "id", 42)
	if err.Error() != "invoice 42 was not found" {
		t.Errorf("Err message = %q, want the rendered template", err.Error())
	}
	missing := reg.Err("not_found")
	if missing.Error() != "invoice {id} was not found" {
		t.Errorf("Err with no param = %q, want the placeholder kept", missing.Error())
	}
}

// TestCatalog_ErrIsAsUnwrap proves errors.Is finds the entry, errors.As reaches the
// coded error, and a cause unwraps.
func TestCatalog_ErrIsAsUnwrap(t *testing.T) {
	entry := invoiceEntry()
	reg := catalog.New("invoice", entry)
	cause := errors.New("connection reset")

	err := reg.Err("not_found", "id", 7, "cause", cause)

	if !errors.Is(err, entry) {
		t.Error("errors.Is(err, entry) = false, want true")
	}
	var coded catalog.CodedError
	if !errors.As(err, &coded) {
		t.Fatal("errors.As to catalog.CodedError failed")
	}
	if coded.Code() != "INVOICE_NOT_FOUND" {
		t.Errorf("Code() = %q, want INVOICE_NOT_FOUND", coded.Code())
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) = false, want true")
	}
	if got := coded.Entry().Fix; got != entry.Fix {
		t.Errorf("Entry().Fix = %q, want %q", got, entry.Fix)
	}
}

// TestCatalog_ErrUnknownCode proves an unknown code is an error, not a panic.
func TestCatalog_ErrUnknownCode(t *testing.T) {
	reg := catalog.New("invoice", invoiceEntry())
	err := reg.Err("nope")
	if err == nil {
		t.Fatal("Err with an unknown code returned nil")
	}
	if !errors.Is(err, catalog.ErrUnknownCode) {
		t.Errorf("Err = %v, want it to wrap ErrUnknownCode", err)
	}
}

// TestCatalog_AuditIsReadable proves the audit policy travels with the entry.
func TestCatalog_AuditIsReadable(t *testing.T) {
	reg := catalog.New("invoice", invoiceEntry())
	got, _ := reg.Get("not_found")
	if got.Audit == nil || got.Audit.Action != "invoice.read" {
		t.Errorf("entry audit = %+v, want action invoice.read", got.Audit)
	}
	if fmt.Sprint(got.Audit.Severity) != "low" {
		t.Errorf("severity = %v, want low", got.Audit.Severity)
	}
}
