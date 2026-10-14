package redact_test

import (
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

const echoFrame = "github.com/labstack/echo/v4/middleware.ContextTimeoutConfig.ToMiddleware.func2.1\n\t/go/pkg/mod/github.com/labstack/echo/v4@v4.13.3/middleware/context_timeout.go:71 +0x1c4"

const realJWT = "eyJhbGciOiJIUzI1NiIs.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"

func TestRedact_StackFramesKeepTheirNames(t *testing.T) {
	event := map[string]any{
		"error": map[string]any{"message": "timeout", "stack": echoFrame, "password": "hunter2"},
		"errors": []any{
			map[string]any{"message": "first", "stack": echoFrame},
		},
	}
	redact.Default().Apply(event)

	errInfo := event["error"].(map[string]any)
	if errInfo["stack"] != echoFrame {
		t.Errorf("error.stack = %q, want the frame unchanged", errInfo["stack"])
	}
	if errInfo["password"] != "[REDACTED]" {
		t.Errorf("error.password = %v, want the denylist to still mask it", errInfo["password"])
	}
	first := event["errors"].([]any)[0].(map[string]any)
	if first["stack"] != echoFrame {
		t.Errorf("errors[0].stack = %q, want the frame unchanged", first["stack"])
	}
}

func TestRedact_StackFixKeepsJWTMasked(t *testing.T) {
	event := map[string]any{
		"authz":  realJWT,
		"user":   map[string]any{"stack": echoFrame},
		"error":  map[string]any{"stack": echoFrame + "\npanic: bad token " + realJWT},
		"errors": []any{map[string]any{"stack": "token=x" + realJWT}},
	}
	redact.Default().Apply(event)

	if event["authz"] != "eyJ***.***" {
		t.Errorf("authz = %q, want the jwt pattern to mask a token outside a stack", event["authz"])
	}
	if event["user"].(map[string]any)["stack"] == echoFrame {
		t.Errorf("a user field named stack skipped the jwt pattern, want only error.stack exempt")
	}
	stack := event["error"].(map[string]any)["stack"].(string)
	if strings.Contains(stack, "dozjgNryP4J3") {
		t.Errorf("error.stack = %q, want a real jwt in a stack masked", stack)
	}
	glued := event["errors"].([]any)[0].(map[string]any)["stack"].(string)
	if strings.Contains(glued, "dozjgNryP4J3") {
		t.Errorf("errors[0].stack = %q, want a jwt glued to a word masked", glued)
	}
	if !strings.HasPrefix(stack, echoFrame) {
		t.Errorf("error.stack = %q, want the frame kept next to the masked token", stack)
	}
}
