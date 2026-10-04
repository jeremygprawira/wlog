package redact_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

// TestRedact_A13_McpSessionIsKept proves the path rpc.mcp.session is not
// masked. The session denylist entry still masks every other session key.
func TestRedact_A13_McpSessionIsKept(t *testing.T) {
	r := redact.Default()
	if r.Denies("rpc.mcp.session") {
		t.Fatal("Denies(rpc.mcp.session) = true, want false")
	}
	if r.DeniesPath("rpc", "mcp", "session") {
		t.Fatal("DeniesPath(rpc.mcp.session) = true, want false")
	}
	if !r.Denies("session") || !r.Denies("session_id") {
		t.Fatal("the session denylist entry no longer matches")
	}
	event := map[string]any{
		"rpc": map[string]any{
			"mcp": map[string]any{
				"session":    "e975c5360c088b08",
				"session_id": "secret-session",
			},
		},
	}
	r.Apply(event)
	mcp := event["rpc"].(map[string]any)["mcp"].(map[string]any)
	if mcp["session"] != "e975c5360c088b08" {
		t.Fatalf("rpc.mcp.session = %v, want the hash kept", mcp["session"])
	}
	if mcp["session_id"] != "[REDACTED]" {
		t.Fatalf("rpc.mcp.session_id = %v, want [REDACTED]", mcp["session_id"])
	}
}
