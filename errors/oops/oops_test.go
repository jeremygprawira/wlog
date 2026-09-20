// This file checks the oops extractor: the code, the domain, the hint, the public
// message, the internal detail, and the rule that a request dump never reaches output.
package wlogoops_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samber/oops"

	wlogoops "github.com/jeremygprawira/wlog/errors/oops"
)

// TestOops_C7_Fields proves the code, the domain, the hint, and the context of one oops
// error.
func TestOops_C7_Fields(t *testing.T) {
	err := oops.In("orders").
		Code("ORDER_NOT_PAYABLE").
		Hint("Ask the customer for another card.").
		With("order_id", "A-1").
		New("the order is not payable")
	info := wlogoops.Extractor().Extract(err)

	if info.Code != "ORDER_NOT_PAYABLE" {
		t.Errorf("code = %q, want ORDER_NOT_PAYABLE", info.Code)
	}
	if info.Kind != "orders" {
		t.Errorf("kind = %q, want orders", info.Kind)
	}
	if info.Fix != "Ask the customer for another card." {
		t.Errorf("fix = %q, want the hint", info.Fix)
	}
	if !strings.Contains(info.Message, "the order is not payable") {
		t.Errorf("message = %q, want the error text", info.Message)
	}
	context, _ := info.Internal["context"].(map[string]any)
	if context["order_id"] != "A-1" {
		t.Errorf("internal context = %v, want order_id A-1", info.Internal["context"])
	}
}

// TestOops_C7_PublicMessage proves that WithPublicMessage puts the client-safe text in
// the message.
func TestOops_C7_PublicMessage(t *testing.T) {
	err := oops.In("orders").Public("Try again later.").New("the database is down")
	info := wlogoops.Extractor(wlogoops.WithPublicMessage()).Extract(err)

	if info.Message != "Try again later." {
		t.Errorf("message = %q, want the public text", info.Message)
	}
	if info.Data["public"] != "Try again later." {
		t.Errorf("data.public = %v, want the public text", info.Data["public"])
	}
}

// TestOops_C7_NoRequestLeak proves that a request attached to an oops error never reaches
// the info.
func TestOops_C7_NoRequestLeak(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/orders", nil)
	request.Header.Set("Authorization", "Bearer s3cret")
	err := oops.In("orders").Request(request, true).New("the call failed")

	body, err := json.Marshal(wlogoops.Extractor().Extract(err))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{"s3cret", "Authorization", "Bearer"} {
		if strings.Contains(string(body), secret) {
			t.Errorf("the request dump reached the info: %s", body)
		}
	}
}

// TestOops_C7_PlainError proves that another error type is not claimed.
func TestOops_C7_PlainError(t *testing.T) {
	if info := wlogoops.Extractor().Extract(errPlain{}); info.Code != "" {
		t.Errorf("code = %q, want an empty info for a plain error", info.Code)
	}
}

// errPlain is a plain error.
type errPlain struct{}

// Error returns the message.
func (errPlain) Error() string { return "plain" }
