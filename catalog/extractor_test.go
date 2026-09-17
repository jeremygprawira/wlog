package catalog_test

import (
	"errors"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/catalog"
)

// stubExtractor returns the ErrorInfo a test hands it.
type stubExtractor struct{ info wlog.ErrorInfo }

func (s stubExtractor) Extract(error) wlog.ErrorInfo { return s.info }

// TestCatalog_Extractor_FillsFromEntry proves the decorator fills the empty fields of
// the wrapped extractor's result.
func TestCatalog_Extractor_FillsFromEntry(t *testing.T) {
	reg := catalog.New("invoice", invoiceEntry())
	next := stubExtractor{info: wlog.ErrorInfo{Code: "INVOICE_NOT_FOUND", Message: "no row"}}
	got := catalog.MustExtractor(next, reg).Extract(errors.New("no row"))

	if got.Kind != "not_found" || got.Status != 404 {
		t.Errorf("Kind/Status = %q/%d, want not_found/404", got.Kind, got.Status)
	}
	if got.Why == "" || got.Fix == "" || got.Link == "" {
		t.Errorf("guidance fields empty: %+v", got)
	}
	if got.Message != "no row" {
		t.Errorf("Message = %q, want the wrapped extractor's value", got.Message)
	}
}

// TestCatalog_Extractor_NextWins proves a field the wrapped extractor filled is never
// replaced by the static default.
func TestCatalog_Extractor_NextWins(t *testing.T) {
	reg := catalog.New("invoice", invoiceEntry())
	next := stubExtractor{info: wlog.ErrorInfo{
		Code: "INVOICE_NOT_FOUND", Status: 500, Why: "per-request detail", Message: "boom",
	}}
	got := catalog.MustExtractor(next, reg).Extract(errors.New("boom"))

	if got.Status != 500 || got.Why != "per-request detail" {
		t.Errorf("next's fields were replaced: %+v", got)
	}
	if got.Kind != "not_found" || got.Fix == "" {
		t.Errorf("empty fields were not filled: %+v", got)
	}
}

// TestCatalog_Extractor_UnmatchedPassesThrough proves an unknown code is untouched.
func TestCatalog_Extractor_UnmatchedPassesThrough(t *testing.T) {
	reg := catalog.New("invoice", invoiceEntry())
	next := stubExtractor{info: wlog.ErrorInfo{Code: "OTHER", Message: "kept"}}
	got := catalog.MustExtractor(next, reg).Extract(errors.New("x"))

	if got.Code != "OTHER" || got.Message != "kept" || got.Status != 0 || got.Why != "" {
		t.Errorf("unmatched code changed: %+v", got)
	}
}

// TestCatalog_Extractor_Agnostic proves the decorator gives one registry the same
// result through two different wrapped extractors, because it reads the code and never
// the Go type.
func TestCatalog_Extractor_Agnostic(t *testing.T) {
	reg := catalog.New("invoice", invoiceEntry())
	first := catalog.MustExtractor(stubExtractor{info: wlog.ErrorInfo{Code: "INVOICE_NOT_FOUND"}}, reg)
	second := catalog.MustExtractor(stubExtractor{info: wlog.ErrorInfo{Code: "invoice_not_found"}}, reg)

	a := first.Extract(errors.New("x"))
	b := second.Extract(errors.New("x"))
	if a.Kind != b.Kind || a.Status != b.Status || a.Why != b.Why || a.Fix != b.Fix || a.Link != b.Link {
		t.Errorf("two extractors disagreed:\n%+v\n%+v", a, b)
	}
}

// TestCatalog_CAT6_FullCodeOnly proves the decorator matches a full code only, so the
// default extractor's plain "INTERNAL" never picks up an entry named "internal", and that
// a registry can opt in to the short spelling.
func TestCatalog_CAT6_FullCodeOnly(t *testing.T) {
	reg := catalog.New("billing",
		catalog.Entry{Code: "internal", Status: 502, Why: "the billing service refused"},
	)
	next := stubExtractor{info: wlog.ErrorInfo{Code: "INTERNAL"}}

	got, err := catalog.Extractor(next, reg)
	if err != nil {
		t.Fatalf("Extractor: %v", err)
	}
	info := got.Extract(errors.New("boom"))
	if info.Status != 0 || info.Why != "" {
		t.Errorf("a short code matched a full-code entry: %+v", info)
	}

	// A registry that opts in accepts both spellings.
	short, err := catalog.Extractor(next, reg.AllowShortCodes())
	if err != nil {
		t.Fatalf("Extractor: %v", err)
	}
	if info := short.Extract(errors.New("boom")); info.Status != 502 || info.Why == "" {
		t.Errorf("the opt-in registry did not match the short code: %+v", info)
	}

	// The full code matches either way.
	full, err := catalog.Extractor(stubExtractor{info: wlog.ErrorInfo{Code: "BILLING_INTERNAL"}}, reg)
	if err != nil {
		t.Fatalf("Extractor: %v", err)
	}
	if info := full.Extract(errors.New("boom")); info.Status != 502 {
		t.Errorf("the full code did not match: %+v", info)
	}
}

// TestCatalog_CAT6_DuplicateReturnsError proves two registries that define the same full
// code are refused, that MustExtractor panics on it, and that a nil registry is skipped.
func TestCatalog_CAT6_DuplicateReturnsError(t *testing.T) {
	first := catalog.New("billing", catalog.Entry{Code: "not_found"})
	second := catalog.New("billing", catalog.Entry{Code: "not_found"})

	if _, err := catalog.Extractor(wlog.DefaultExtractor(), first, second); err == nil {
		t.Fatal("Extractor accepted two registries that define the same code")
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("MustExtractor did not panic on a duplicate code")
			}
		}()
		_ = catalog.MustExtractor(wlog.DefaultExtractor(), first, second)
	}()

	// A nil registry is skipped, not treated as a duplicate.
	other := catalog.New("orders", catalog.Entry{Code: "not_found"})
	if _, err := catalog.Extractor(wlog.DefaultExtractor(), nil, first, nil, other); err != nil {
		t.Errorf("Extractor refused a nil registry beside valid ones: %v", err)
	}

	// Two registries with different prefixes never collide.
	if _, err := catalog.Extractor(wlog.DefaultExtractor(), first, other); err != nil {
		t.Errorf("Extractor refused two different prefixes: %v", err)
	}
}

// TestCatalog_CAT7_IsSameRegistry proves errors.Is matches an entry from the registry the
// error came from, and nothing else: not an entry with the same short code in another
// registry, and not a hand-built entry that belongs to no registry.
func TestCatalog_CAT7_IsSameRegistry(t *testing.T) {
	billing := catalog.New("billing", catalog.Entry{Code: "not_found", Message: "invoice {id} was not found"})
	orders := catalog.New("orders", catalog.Entry{Code: "not_found", Message: "order {id} was not found"})

	err := billing.Err("not_found", "id", 7)
	billingEntry, ok := billing.Get("not_found")
	if !ok {
		t.Fatal("billing.Get returned no entry")
	}
	ordersEntry, ok := orders.Get("not_found")
	if !ok {
		t.Fatal("orders.Get returned no entry")
	}

	if !errors.Is(err, billingEntry) {
		t.Error("errors.Is did not match the entry from the error's own registry")
	}
	if errors.Is(err, ordersEntry) {
		t.Error("errors.Is matched an entry from another registry with the same short code")
	}
	if errors.Is(err, catalog.Entry{Code: "NOT_FOUND"}) {
		t.Error("errors.Is matched a hand-built entry that belongs to no registry")
	}
	if errors.Is(err, orders.Err("not_found")) {
		t.Error("errors.Is matched a coded error from another registry")
	}
	if !errors.Is(err, billing.Err("not_found")) {
		t.Error("errors.Is did not match a coded error with the same full code")
	}
}
