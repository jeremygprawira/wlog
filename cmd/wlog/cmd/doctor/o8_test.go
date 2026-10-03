package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDoctor_O8_OTelOrderCases proves the stats handler, a comment, and a second
// file cannot hide an OTel middleware that sits inside wlog.
func TestDoctor_O8_OTelOrderCases(t *testing.T) {
	handler := `stats := otelgrpc.NewServerHandler()
grpc.ChainUnaryInterceptor(wloggrpc.UnaryServerInterceptor(), otelgrpc.UnaryServerInterceptor())`
	if check := otelOrderCheck(handler); check.Status != "warn" {
		t.Errorf("stats handler hid the interceptor order: %s, want warn", check.Status)
	}

	comment := `// otelhttp.NewHandler wraps outside
handler := wlogstd.Setup(otelhttp.NewHandler(mux, "server"))`
	if check := otelOrderCheck(comment); check.Status != "warn" {
		t.Errorf("a comment hid the order: %s, want warn", check.Status)
	}

	dir := t.TempDir()
	ok := []byte("package p\nfunc ok() { otelhttp.NewHandler(wlogstd.Setup(mux), \"server\") }\n")
	bad := []byte("package p\nfunc bad() { wlogstd.Setup(otelhttp.NewHandler(mux, \"server\")) }\n")
	if err := os.WriteFile(filepath.Join(dir, "a_ok.go"), ok, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "z_bad.go"), bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if check := checkOTelOrder(dir); check.Status != "warn" {
		t.Errorf("one correct file hid the wrong file: %s, want warn", check.Status)
	}
}
