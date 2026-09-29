// Package wlogmcp gives every request an modelcontextprotocol/go-sdk server handles one
// wide event, of kind rpc with rpc.system mcp, through Middleware.
//
// Read top to bottom: Middleware wraps the server's receiving method handler. It skips a
// notification, because the MCP spec gives one no result to report and no caller waiting
// on one. classify reads the result and the error of a request into the event's outcome:
// ok, tool_error, protocol_error, or input_required, following the level rule of
// SPEC-track-g.md. Tool arguments and the tool result are never stored unless the caller
// passes WithContent. Core redacts those values like any other.
package wlogmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// Option configures Middleware.
type Option func(*config)

// config holds the resolved options.
type config struct {
	service string
	content bool
}

// WithService names the MCP server in rpc.service and as the first segment of the
// operation, such as "orders-mcp/tools/call". Without it, the operation is the bare
// method, because a *mcp.Server carries no exported way to read its own name back.
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

// Middleware returns the mcp.Middleware to pass to (*mcp.Server).AddReceivingMiddleware.
func Middleware(log *wlog.Logger, opts ...Option) mcp.Middleware {
	cfg := resolve(opts...)
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if strings.HasPrefix(method, "notifications/") {
				return next(ctx, method, req)
			}

			fields := map[string]any{"system": "mcp", "method": method}
			if cfg.service != "" {
				fields["service"] = cfg.service
			}
			ctx, h := work.Start(ctx, log, work.Unit{Kind: work.KindRPC, Fields: fields})

			result, err := next(ctx, method, req)
			record(ctx, h, req, result, err, cfg.content)
			return result, err
		}
	}
}

// record folds one finished request into the rpc group and picks the event's level, per
// the rules of SPEC-track-g.md: a tool error is the caller's fault, so it gives warn; the
// four codes that mean a malformed request also give warn; every other protocol error
// gives error.
func record(ctx context.Context, h *work.Handle, req mcp.Request, result mcp.Result, err error, content bool) {
	mcpFields := sessionFields(req)
	addParamFields(mcpFields, req, content)

	outcome, code, level := classify(result, err)
	mcpFields["result"] = outcome
	// result is meaningless on a protocol error: the dispatcher's own concrete return
	// type, boxed into the Result interface, is a typed nil here, and every accessor
	// below panics on one.
	if err == nil {
		if state := requestStateOf(result); state != "" {
			mcpFields["request_state"] = hashState(state)
		}
		if content {
			addResultContent(mcpFields, result)
		}
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

// sessionFields reads the session id, the negotiated protocol version, and the client
// name and version off the request's session. A request before the handshake, or from a
// session type this package does not know, gives an empty map.
func sessionFields(req mcp.Request) map[string]any {
	fields := map[string]any{}
	session, ok := req.GetSession().(*mcp.ServerSession)
	if !ok || session == nil {
		return fields
	}
	if id := session.ID(); id != "" {
		fields["session_id"] = id
	}
	params := session.InitializeParams()
	if params == nil {
		return fields
	}
	if params.ProtocolVersion != "" {
		fields["protocol_version"] = params.ProtocolVersion
	}
	if params.ClientInfo != nil {
		fields["client"] = clientOf(params.ClientInfo.Name, params.ClientInfo.Version)
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

// addParamFields reads the tool name, the resource URI, or the prompt name off the
// request's params, and the tool arguments when content is set.
func addParamFields(fields map[string]any, req mcp.Request, content bool) {
	switch params := req.GetParams().(type) {
	case *mcp.CallToolParamsRaw:
		fields["tool"] = params.Name
		if content && len(params.Arguments) > 0 {
			fields["arguments"] = params.Arguments
		}
	case *mcp.ReadResourceParams:
		fields["resource_uri"] = params.URI
	case *mcp.GetPromptParams:
		fields["prompt"] = params.Name
	}
}

// addResultContent adds the tool result's content to fields, under a key distinct from
// "result", which already names the call's ok/tool_error/protocol_error/input_required
// outcome. Only the tools/call shape carries a result content today.
func addResultContent(fields map[string]any, result mcp.Result) {
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

// classify reads one finished request into its outcome, its JSON-RPC status code, and the
// level that outcome asks for. An empty level leaves the event at whatever level it
// already has, which is info for a plain success.
func classify(result mcp.Result, err error) (outcome, code string, level wlog.Level) {
	if err != nil {
		var wireErr *jsonrpc.Error
		if errors.As(err, &wireErr) {
			code = strconv.FormatInt(wireErr.Code, 10)
			if isClientCode(wireErr.Code) {
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
func isClientCode(code int64) bool {
	switch code {
	case jsonrpc.CodeParseError, jsonrpc.CodeInvalidRequest, jsonrpc.CodeMethodNotFound, jsonrpc.CodeInvalidParams:
		return true
	}
	return false
}

// needsInputResult is the shape every MCP result with an input-required state shares.
type needsInputResult interface{ NeedsInput() bool }

// needsInput reports whether a result asked the client for more input before it can
// finish. A result type this package does not know never does.
func needsInput(result mcp.Result) bool {
	r, ok := result.(needsInputResult)
	return ok && r.NeedsInput()
}

// isToolError reports whether a tool call result ended in an error. Only tools/call
// carries IsError; every other method's error is a protocol error instead.
func isToolError(result mcp.Result) bool {
	r, ok := result.(*mcp.CallToolResult)
	return ok && r != nil && r.IsError
}

// requestStateOf reads the opaque requestState a result carries when it needs input, so a
// retry round trip can be linked to the result that asked for it.
func requestStateOf(result mcp.Result) string {
	switch r := result.(type) {
	case *mcp.CallToolResult:
		return r.RequestState
	case *mcp.ReadResourceResult:
		return r.RequestState
	case *mcp.GetPromptResult:
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
