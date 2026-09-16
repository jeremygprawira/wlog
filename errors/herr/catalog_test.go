package wlogherr_test

import (
	"testing"

	"github.com/jeremygprawira/herr"
	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/catalog"
	wlogherr "github.com/jeremygprawira/wlog/errors/herr"
)

// TestCatalog_MapsClass proves a herr class becomes an entry with the same code, kind
// name, and HTTP status.
func TestCatalog_MapsClass(t *testing.T) {
	class := herr.Define(herr.Class{
		Code:   "NOT_FOUND",
		Kind:   herr.KindNotFound,
		Public: herr.Message("That order does not exist."),
	})

	entries := wlogherr.Catalog(class)
	if len(entries) != 1 {
		t.Fatalf("Catalog returned %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Code != "NOT_FOUND" {
		t.Errorf("Code = %q, want NOT_FOUND", entry.Code)
	}
	if entry.Kind != "not_found" {
		t.Errorf("Kind = %q, want not_found", entry.Kind)
	}
	if entry.Status != 404 {
		t.Errorf("Status = %d, want 404", entry.Status)
	}
	if entry.Message != "That order does not exist." {
		t.Errorf("Message = %q, want the class public message", entry.Message)
	}
}

// TestCatalog_AgnosticProof proves one registry decorates two different error
// libraries the same way. The catalog-filled fields (kind, status, guidance) are
// identical; the code field stays whatever the wrapped extractor produced.
func TestCatalog_AgnosticProof(t *testing.T) {
	class := herr.Define(herr.Class{
		Code:   "NOT_FOUND",
		Kind:   herr.KindNotFound,
		Public: herr.Message("not found"),
	})
	entries := wlogherr.Catalog(class)
	reg := catalog.New("app", entries...)

	viaHerr := catalog.Extractor(wlogherr.Extractor(), reg).Extract(class.New())
	viaDefault := catalog.Extractor(wlog.DefaultExtractor(), reg).Extract(reg.Err("NOT_FOUND"))

	for _, got := range []wlog.ErrorInfo{viaHerr, viaDefault} {
		if got.Kind != "not_found" || got.Status != 404 {
			t.Errorf("kind/status = %q/%d, want not_found/404", got.Kind, got.Status)
		}
	}
	if viaHerr.Kind != viaDefault.Kind || viaHerr.Status != viaDefault.Status ||
		viaHerr.Why != viaDefault.Why || viaHerr.Fix != viaDefault.Fix || viaHerr.Link != viaDefault.Link {
		t.Errorf("the two extractors disagreed:\n%+v\n%+v", viaHerr, viaDefault)
	}
}
