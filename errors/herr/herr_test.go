package wlogherr

import (
	stderrors "errors"
	"testing"

	"github.com/jeremygprawira/herr"
	"github.com/jeremygprawira/wlog"
)

func TestExtractor_MapsHerrFields(t *testing.T) {
	cause := stderrors.New("db timeout")
	err := herr.New("ORDER_NOT_FOUND").
		Kind(herr.KindNotFound).
		Internal("order 42 missing from store").
		WithInternal("order_id", 42).
		Wrap(cause)

	info := Extractor().Extract(err)

	if info.Code != "ORDER_NOT_FOUND" {
		t.Errorf("Code = %q, want ORDER_NOT_FOUND", info.Code)
	}
	if info.Kind != "not_found" {
		t.Errorf("Kind = %q, want not_found", info.Kind)
	}
	if info.Status != 404 {
		t.Errorf("Status = %d, want 404", info.Status)
	}
	if info.Message != "order 42 missing from store" {
		t.Errorf("Message = %q, want internal detail", info.Message)
	}
	if info.Cause != "db timeout" {
		t.Errorf("Cause = %q, want db timeout", info.Cause)
	}
	if info.Attrs["order_id"] != 42 {
		t.Errorf("Attrs[order_id] = %v, want 42", info.Attrs["order_id"])
	}
}

func TestExtractor_PublicMessageHiddenByDefault(t *testing.T) {
	err := herr.New("BAD_INPUT").
		Kind(herr.KindInvalid).
		Public(herr.Message("Something went wrong, try again"))
	// no Internal(...) set

	info := Extractor().Extract(err)

	if info.Message != "" {
		t.Errorf("Message = %q, want empty (public text must not leak by default)", info.Message)
	}
}

func TestExtractor_WithPublicMessage_UsesPublicWhenInternalEmpty(t *testing.T) {
	err := herr.New("BAD_INPUT").
		Kind(herr.KindInvalid).
		Public(herr.Message("Something went wrong, try again"))
	// no Internal(...) set

	info := Extractor(WithPublicMessage()).Extract(err)

	if info.Message != "Something went wrong, try again" {
		t.Errorf("Message = %q, want public text", info.Message)
	}
}

func TestExtractor_WithPublicMessage_InternalStillWins(t *testing.T) {
	err := herr.New("BAD_INPUT").
		Kind(herr.KindInvalid).
		Internal("field 'email' failed validation").
		Public(herr.Message("Something went wrong, try again"))

	info := Extractor(WithPublicMessage()).Extract(err)

	if info.Message != "field 'email' failed validation" {
		t.Errorf("Message = %q, want internal detail even with WithPublicMessage", info.Message)
	}
}

func TestExtractor_PlainError_Fallback(t *testing.T) {
	err := stderrors.New("boom")

	info := Extractor().Extract(err)

	if info.Code != "INTERNAL" {
		t.Errorf("Code = %q, want INTERNAL", info.Code)
	}
	if info.Cause != "boom" {
		t.Errorf("Cause = %q, want boom", info.Cause)
	}
}

// useExtractor takes the wlog interface, so a call proves that the argument
// satisfies it at compile time.
func useExtractor(wlog.ErrorExtractor) {}

func TestExtractor_ImplementsWlogErrorExtractor(t *testing.T) {
	useExtractor(Extractor())
}
