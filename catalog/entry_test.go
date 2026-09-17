package catalog_test

import (
	"errors"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/catalog"
)

// TestCatalog_CAT8_DeepCopy proves Get and CodedError.Entry hand out copies, so a caller
// cannot change a registry entry after New returned. The old code shared the *Audit
// pointer and the default maps, which raced the audit enricher that reads them.
func TestCatalog_CAT8_DeepCopy(t *testing.T) {
	reg := catalog.New("invoice", catalog.Entry{
		Code:     "not_found",
		Status:   404,
		Audit:    &catalog.Audit{Action: "invoice.read", TargetType: "invoice"},
		Data:     map[string]any{"field": "id", "nested": map[string]any{"safe": true}},
		Internal: map[string]any{"row": 7},
	})

	first, ok := reg.Get("not_found")
	if !ok {
		t.Fatal("Get returned no entry")
	}
	// A caller may change what it received; the registry must never see it.
	first.Audit.Action = "tampered"
	first.Data["field"] = "tampered"
	first.Data["nested"].(map[string]any)["safe"] = false
	first.Internal["row"] = 0

	second, _ := reg.Get("not_found")
	if second.Audit.Action != "invoice.read" {
		t.Errorf("the registry's Audit changed through Get: %v", second.Audit)
	}
	if second.Data["field"] != "id" {
		t.Errorf("the registry's Data changed through Get: %v", second.Data)
	}
	if nested, _ := second.Data["nested"].(map[string]any); nested["safe"] != true {
		t.Errorf("a nested default changed through Get: %v", second.Data)
	}
	if second.Internal["row"] != 7 {
		t.Errorf("the registry's Internal changed through Get: %v", second.Internal)
	}

	// The same holds for the entry a coded error carries.
	err := reg.Err("not_found")
	var coded catalog.CodedError
	if !errors.As(err, &coded) {
		t.Fatal("errors.As to catalog.CodedError failed")
	}
	fromError := coded.Entry()
	fromError.Audit.Action = "tampered-again"
	fromError.Data["field"] = "tampered-again"

	third, _ := reg.Get("not_found")
	if third.Audit.Action != "invoice.read" || third.Data["field"] != "id" {
		t.Errorf("Entry() handed out the registry's own values: %+v", third)
	}
}

// TestCatalog_CAT9_SinglePassTemplate proves a template renders in one pass, so a value
// that itself holds a placeholder is never filled by a later parameter.
func TestCatalog_CAT9_SinglePassTemplate(t *testing.T) {
	reg := catalog.New("invoice", catalog.Entry{
		Code:    "not_found",
		Message: "invoice {id} was not found for {who}",
	})

	err := reg.Err("not_found", "id", "{who}", "who", "u-1")
	if got, want := err.Error(), "invoice {who} was not found for u-1"; got != want {
		t.Errorf("message = %q, want %q: a parameter was filled twice", got, want)
	}

	// A placeholder with no parameter stays as written.
	plain := reg.Err("not_found", "id", 7)
	if got, want := plain.Error(), "invoice 7 was not found for {who}"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

// TestCatalog_PAR10_DomainAttr proves the domain reaches the event as error.attrs.domain,
// and that a value the wrapped extractor filled wins.
func TestCatalog_PAR10_DomainAttr(t *testing.T) {
	reg := catalog.New("billing", catalog.Entry{Code: "declined", Status: 402})

	info := catalog.MustExtractor(wlog.DefaultExtractor(), reg).Extract(reg.Err("declined"))
	if info.Code != "BILLING_DECLINED" {
		t.Errorf("code = %q, want the full, domain-qualified code", info.Code)
	}
	if domain, _ := info.Attrs["domain"].(string); domain != "billing" {
		t.Errorf("attrs.domain = %v, want the registry prefix", info.Attrs)
	}

	// The wrapped extractor's own attrs win, and its other attrs stay.
	next := stubExtractor{info: wlog.ErrorInfo{
		Code:  "BILLING_DECLINED",
		Attrs: map[string]any{"domain": "override", "trace": "t-1"},
	}}
	info = catalog.MustExtractor(next, reg).Extract(errors.New("x"))
	if info.Attrs["domain"] != "override" {
		t.Errorf("attrs.domain = %v, want the value the wrapped extractor filled", info.Attrs["domain"])
	}
	if info.Attrs["trace"] != "t-1" {
		t.Errorf("the wrapped extractor's attrs were dropped: %v", info.Attrs)
	}
}

// TestCatalog_PAR10_EntryDefaults proves an entry's Data and Internal defaults reach the
// ErrorInfo, under the values a per-request extractor already filled.
func TestCatalog_PAR10_EntryDefaults(t *testing.T) {
	reg := catalog.New("invoice", catalog.Entry{
		Code:     "not_found",
		Status:   404,
		Data:     map[string]any{"field": "id", "safe": true},
		Internal: map[string]any{"row": 7},
	})
	next := stubExtractor{info: wlog.ErrorInfo{
		Code:     "INVOICE_NOT_FOUND",
		Data:     map[string]any{"field": "order_id"},
		Internal: map[string]any{"query": "select 1"},
	}}

	info := catalog.MustExtractor(next, reg).Extract(errors.New("x"))
	if info.Data["field"] != "order_id" {
		t.Errorf("Data[field] = %v, want the call-site value", info.Data["field"])
	}
	if info.Data["safe"] != true {
		t.Errorf("the static default did not merge: %v", info.Data)
	}
	if info.Internal["row"] != 7 || info.Internal["query"] != "select 1" {
		t.Errorf("Internal defaults did not merge under the call-site values: %v", info.Internal)
	}

	// A change to what the extractor produced must not reach the registry.
	info.Data["field"] = "tampered"
	entry, _ := reg.Get("not_found")
	if entry.Data["field"] != "id" {
		t.Errorf("the extractor's result aliases the registry: %v", entry.Data)
	}

	// The catalog must not copy its raw template into Message: the real error message is
	// better, and the template is not the message.
	templated := catalog.New("invoice", catalog.Entry{Code: "not_found", Message: "invoice {id} was not found"})
	info = catalog.MustExtractor(stubExtractor{info: wlog.ErrorInfo{Code: "INVOICE_NOT_FOUND"}}, templated).Extract(errors.New("x"))
	if info.Message != "" {
		t.Errorf("Message = %q, want it left alone so the real message shows", info.Message)
	}
}
