// Package mcpfixture holds the shared MCP event goldens. ai-mcpsdk and ai-mcpgo both
// compare a finished event to these files, so the two modules cannot drift apart.
package mcpfixture

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/jeremygprawira/wlog/internal/conformance"
)

// Names are the four fixtures criterion 4 names.
const (
	ToolCall      = "tool_call"
	ToolError     = "tool_error"
	UnknownTool   = "unknown_tool"
	InputRequired = "input_required"
)

// golden is one hand-written event. error.message, error.type, and error.cause are
// not part of it, because those three differ between the two SDKs.
var golden = map[string]string{
	ToolCall:      toolCall,
	ToolError:     toolError,
	UnknownTool:   unknownTool,
	InputRequired: inputRequired,
}

// Equal reports whether event matches the named golden. It compares the whole event
// after conformance.Normalize, except error.message, error.type, and error.cause.
func Equal(event map[string]any, name string) error {
	raw, ok := golden[name]
	if !ok {
		return fmt.Errorf("unknown mcp fixture %q", name)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(raw), &want); err != nil {
		return err
	}
	got := roundTrip(dropErrorText(conformance.Normalize(event)))
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("fixture %s\n got %s\nwant %s", name, mustJSON(got), mustJSON(want))
	}
	return nil
}

// dropErrorText removes the three error fields that the two SDKs do not share.
func dropErrorText(event map[string]any) map[string]any {
	group, _ := event["error"].(map[string]any)
	if group == nil {
		return event
	}
	delete(group, "message")
	delete(group, "type")
	delete(group, "cause")
	return event
}

// roundTrip rewrites numbers the way encoding/json does, so a golden and an event
// compare as the same types.
func roundTrip(event map[string]any) map[string]any {
	body, err := json.Marshal(event)
	if err != nil {
		return event
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return event
	}
	return out
}

func mustJSON(v any) string {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(body)
}
