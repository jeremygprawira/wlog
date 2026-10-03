// Package wlogmcpgo gives every request a mark3labs/mcp-go server handles one wide event,
// of kind rpc with rpc.system mcp, through Hooks. It gives the same fields and the same
// level rule as ai-mcpsdk, so the two modules produce identical events for the same call.
//
// Read top to bottom: Hooks registers OnBeforeAny, OnSuccess, and OnError. This SDK has no
// single wrapping MethodHandler, and its ToolHandlerFunc middleware misses errors and runs
// twice on a legacy multi-round-trip retry, so Hooks is the only reliable seam. A before
// hook and its matching after hook are two separate calls with no shared context, so
// pending links them by session id and request id, in a bounded map that expires an entry
// after 5 minutes: the doc of [server.Hooks] warns that a handler panic with no recovery
// middleware installed skips both OnSuccess and OnError, which would otherwise leak the
// entry forever.
package wlogmcpgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// Option configures Hooks.
type Option func(*config)

// config holds the resolved options.
type config struct {
	service string
	content bool
}

// WithService names the MCP server in rpc.service and as the first segment of the
// operation, such as "orders-mcp/tools/call". Without it, the operation is the bare
// method, because a *server.MCPServer carries no exported way to read its own name back.
func WithService(name string) Option { return func(c *config) { c.service = name } }

// WithContent opts into the tool call arguments and the tool result. Without it, the
// event holds the shape of the call and no payload. Core redacts the values.
func WithContent() Option { return func(c *config) { c.content = true } }

// resolve applies the options.
func resolve(opts ...Option) config {
	c := config{}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// pendingTTL and pendingCapacity bound the memory a leaked before-hook entry can hold, per
// the ai-mcpgo hook point of SPEC-track-g.md.
const (
	pendingTTL      = 5 * time.Minute
	pendingCapacity = 10_000
)

// pendingKey links one before hook to its after hook: the request id alone is only unique
// within one session, because a server with several sessions can see the same id twice.
type pendingKey struct {
	session string
	id      string
}

// pendingEntry is the state a before hook stashes for its after hook: the event's context,
// so a level set at the after hook lands on the right event, and the work.Handle that ends
// it.
type pendingEntry struct {
	ctx     context.Context
	handle  *work.Handle
	expires time.Time
}

// pending is the bounded, TTL'd map of in-flight requests.
//
// ponytail: prune is an O(n) scan over every entry on each store, which is fine at the
// 10,000-entry cap this map enforces. Move to a time-ordered structure if a server ever
// runs hot enough for that scan to show up in a profile.
type pending struct {
	mu      sync.Mutex
	entries map[pendingKey]pendingEntry
}

func newPending() *pending { return &pending{entries: map[pendingKey]pendingEntry{}} }

// store adds one entry, first dropping every expired one. A map already at the cap drops
// the new entry instead of growing further, so a request that never gets an after hook
// costs bounded memory, not unbounded.
func (p *pending) store(key pendingKey, ctx context.Context, h *work.Handle) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, e := range p.entries {
		if now.After(e.expires) {
			delete(p.entries, k)
		}
	}
	if len(p.entries) >= pendingCapacity {
		return
	}
	p.entries[key] = pendingEntry{ctx: ctx, handle: h, expires: now.Add(pendingTTL)}
}

// take removes and returns one entry at any age. A missing entry reports false, which
// is the normal outcome for the parse and capability failures that fire OnError with no
// prior OnBeforeAny. Age is enforced only by the prune inside store, so a request that
// runs longer than the TTL still ends.
func (p *pending) take(key pendingKey) (context.Context, *work.Handle, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[key]
	if !ok {
		return nil, nil, false
	}
	delete(p.entries, key)
	return entry.ctx, entry.handle, true
}

// Hooks returns the *server.Hooks to pass to server.WithHooks.
func Hooks(log *wlog.Logger, opts ...Option) *server.Hooks {
	cfg := resolve(opts...)
	pend := newPending()
	hooks := &server.Hooks{}

	hooks.AddBeforeAny(func(ctx context.Context, id any, method mcp.MCPMethod, _ any) {
		if strings.HasPrefix(string(method), "notifications/") {
			return
		}
		fields := map[string]any{"system": "mcp", "method": string(method)}
		if cfg.service != "" {
			fields["service"] = cfg.service
		}
		next, h := work.Start(ctx, log, work.Unit{Kind: work.KindRPC, Fields: fields})
		pend.store(keyOf(ctx, id), next, h)
	})
	hooks.AddOnSuccess(func(ctx context.Context, id any, method mcp.MCPMethod, message any, result any) {
		finish(pend, keyOf(ctx, id), message, result, nil, cfg.content)
	})
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		finish(pend, keyOf(ctx, id), message, nil, err, cfg.content)
	})
	return hooks
}

// keyOf reads the session id off ctx, so ai-mcpgo links a before hook to its after hook by
// session and request id, the way SPEC-track-g.md asks. A request with no session yet,
// such as the very first initialize, keys on an empty session id. The id is formatted,
// because a JSON-RPC id may be an array or an object, and a map key must be comparable.
func keyOf(ctx context.Context, id any) pendingKey {
	session := ""
	if s := server.ClientSessionFromContext(ctx); s != nil {
		session = s.SessionID()
	}
	return pendingKey{session: session, id: fmt.Sprintf("%v", id)}
}

// finish looks up the before hook this after hook matches, folds the result or the error
// into the rpc group, and ends the event. A before hook this package never saw, which is
// the documented case for a parse or a capability failure, is left alone.
func finish(pend *pending, key pendingKey, message, result any, err error, content bool) {
	ctx, h, ok := pend.take(key)
	if !ok {
		return
	}

	mcpFields := sessionFields(ctx)
	addMessageFields(mcpFields, message, content)

	outcome, code, level := classify(result, err)
	mcpFields["result"] = outcome
	if state := requestStateOf(result); state != "" {
		mcpFields["request_state"] = hashState(state)
	}
	if content {
		addResultContent(mcpFields, result)
	}
	h.Set("mcp", mcpFields)
	if code != "" {
		h.Set("status_code", code)
	}
	if level != "" {
		wlog.SetLevel(ctx, level)
	}
	h.End(err)
}

// sessionFields reads the session id and, when a request or the session already carries
// one, the client name and version. mcp-go gives no exported accessor for the protocol
// version a legacy session negotiated at initialize, unlike the per-request one SEP-2575
// carries, so this leaves rpc.mcp.protocol_version unset for a legacy session.
func sessionFields(ctx context.Context) map[string]any {
	fields := map[string]any{}
	if info := server.RequestProtocolInfoFromContext(ctx); info != nil {
		if info.ProtocolVersion != "" {
			fields["protocol_version"] = info.ProtocolVersion
		}
		if info.ClientInfo != nil {
			fields["client"] = clientOf(info.ClientInfo.Name, info.ClientInfo.Version)
		}
	}
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return fields
	}
	if id := session.SessionID(); id != "" {
		fields["session_id"] = id
	}
	if _, hasClient := fields["client"]; !hasClient {
		if withInfo, ok := session.(server.SessionWithClientInfo); ok {
			if info := withInfo.GetClientInfo(); info.Name != "" {
				fields["client"] = clientOf(info.Name, info.Version)
			}
		}
	}
	return fields
}

// clientOf formats a client name and version as one string, so rpc.mcp.client stays a
// single field like every other identity field on the event.
func clientOf(name, version string) string {
	if version == "" {
		return name
	}
	return name + "/" + version
}

// addMessageFields reads the tool name, the resource URI, or the prompt name off the
// request message, and the tool arguments when content is set.
func addMessageFields(fields map[string]any, message any, content bool) {
	switch m := message.(type) {
	case *mcp.CallToolRequest:
		fields["tool"] = m.Params.Name
		if content && len(m.Params.RawArguments) > 0 {
			fields["arguments"] = m.Params.RawArguments
		}
	case *mcp.ReadResourceRequest:
		fields["resource_uri"] = m.Params.URI
	case *mcp.GetPromptRequest:
		fields["prompt"] = m.Params.Name
	}
}

// addResultContent adds the tool result's content to fields, under a key distinct from
// "result", which already names the call's ok/tool_error/protocol_error/input_required
// outcome. Only the tools/call shape carries a result content today.
func addResultContent(fields map[string]any, result any) {
	res, ok := result.(*mcp.CallToolResult)
	if !ok || res == nil {
		return
	}
	if len(res.Content) > 0 {
		fields["result_content"] = res.Content
	}
	if res.StructuredContent != nil {
		fields["structured_content"] = res.StructuredContent
	}
}

// jsonRPCErrorer is the shape every error this SDK's request dispatcher hands to onError
// shares: request_handler.go calls err.ToJSONRPCError() on exactly this interface before
// this package ever sees the error, so every error reaching classify satisfies it.
type jsonRPCErrorer interface {
	ToJSONRPCError() mcp.JSONRPCError
}

// classify reads one finished request into its outcome, its JSON-RPC status code, and the
// level that outcome asks for. An empty level leaves the event at whatever level it
// already has, which is info for a plain success.
func classify(result any, err error) (outcome, code string, level wlog.Level) {
	if err != nil {
		if wireErr, ok := err.(jsonRPCErrorer); ok {
			c := wireErr.ToJSONRPCError().Error.Code
			code = strconv.Itoa(c)
			if isClientCode(c) {
				return "protocol_error", code, wlog.LevelWarn
			}
			return "protocol_error", code, wlog.LevelError
		}
		return "protocol_error", "", wlog.LevelError
	}
	if needsInput(result) {
		return "input_required", "", ""
	}
	if isToolError(result) {
		return "tool_error", "", wlog.LevelWarn
	}
	return "ok", "", ""
}

// isClientCode reports whether a JSON-RPC code means the request itself was malformed,
// which is the caller's fault and not the server's.
func isClientCode(code int) bool {
	switch code {
	case mcp.PARSE_ERROR, mcp.INVALID_REQUEST, mcp.METHOD_NOT_FOUND, mcp.INVALID_PARAMS:
		return true
	}
	return false
}

// needsInputResult is the shape every MCP result with an input-required state shares.
type needsInputResult interface{ NeedsInput() bool }

// needsInput reports whether a result asked the client for more input before it can
// finish. A result type this package does not know never does.
func needsInput(result any) bool {
	r, ok := result.(needsInputResult)
	return ok && r.NeedsInput()
}

// isToolError reports whether a tool call result ended in an error. Only tools/call
// carries IsError; every other method's error is a protocol error instead.
func isToolError(result any) bool {
	r, ok := result.(*mcp.CallToolResult)
	return ok && r != nil && r.IsError
}

// requestStateOf reads the opaque requestState a result carries when it needs input, so a
// retry round trip can be linked to the result that asked for it. A nil result, which a
// tool handler may return, has no state.
func requestStateOf(result any) string {
	switch r := result.(type) {
	case *mcp.CallToolResult:
		if r == nil {
			return ""
		}
		return r.RequestState
	case *mcp.ReadResourceResult:
		if r == nil {
			return ""
		}
		return r.RequestState
	case *mcp.GetPromptResult:
		if r == nil {
			return ""
		}
		return r.RequestState
	}
	return ""
}

// hashState hashes an opaque requestState instead of storing it, because the MCP spec
// requires an unauthenticated server to encrypt and sign that value.
func hashState(state string) string {
	sum := sha256.Sum256([]byte(state))
	return hex.EncodeToString(sum[:8])
}
