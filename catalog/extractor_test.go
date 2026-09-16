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
	next := stubExtractor{info: wlog.ErrorInfo{Code: "NOT_FOUND", Message: "no row"}}
	got := catalog.Extractor(next, reg).Extract(errors.New("no row"))

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
		Code: "NOT_FOUND", Status: 500, Why: "per-request detail", Message: "boom",
	}}
	got := catalog.Extractor(next, reg).Extract(errors.New("boom"))

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
	got := catalog.Extractor(next, reg).Extract(errors.New("x"))

	if got.Code != "OTHER" || got.Message != "kept" || got.Status != 0 || got.Why != "" {
		t.Errorf("unmatched code changed: %+v", got)
	}
}

// TestCatalog_Extractor_Agnostic proves the decorator gives one registry the same
// result through two different wrapped extractors, because it reads the code and never
// the Go type.
func TestCatalog_Extractor_Agnostic(t *testing.T) {
	reg := catalog.New("invoice", invoiceEntry())
	first := catalog.Extractor(stubExtractor{info: wlog.ErrorInfo{Code: "NOT_FOUND"}}, reg)
	second := catalog.Extractor(stubExtractor{info: wlog.ErrorInfo{Code: "INVOICE_NOT_FOUND"}}, reg)

	a := first.Extract(errors.New("x"))
	b := second.Extract(errors.New("x"))
	if a.Kind != b.Kind || a.Status != b.Status || a.Why != b.Why || a.Fix != b.Fix || a.Link != b.Link {
		t.Errorf("two extractors disagreed:\n%+v\n%+v", a, b)
	}
}
