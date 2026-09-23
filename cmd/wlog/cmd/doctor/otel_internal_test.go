// This file tests the OTel ordering check of wlog doctor with source text, so the check
// needs no fixture and no OTel dependency.
package doctor

import "testing"

// TestDoctor_OTelOrder proves the check warns when the OTel middleware sits inside the
// wlog middleware, and passes when it wraps outside.
func TestDoctor_OTelOrder(t *testing.T) {
	outside := `handler := otelhttp.NewHandler(wlogstd.Setup(mux), "server")`
	if check := otelOrderCheck(outside); check.Status != "pass" {
		t.Errorf("OTel outside wlog = %s, want pass", check.Status)
	}
	inside := `handler := wlogstd.Setup(otelhttp.NewHandler(mux, "server"))`
	if check := otelOrderCheck(inside); check.Status != "warn" {
		t.Errorf("OTel inside wlog = %s, want warn", check.Status)
	}
	none := `handler := wlogstd.Setup(mux)`
	if check := otelOrderCheck(none); check.Status != "pass" {
		t.Errorf("no OTel middleware = %s, want pass", check.Status)
	}
}
