// Package wloggqlgen adapts http-core to gqlgen, so one GraphQL operation enriches the
// request event that is already open.
//
// Read top to bottom: Extension returns the gqlgen extension. Its operation interceptor
// names the operation and its type on the open event, and its response interceptor adds
// the error count, the partial flag, and the complexity. Recover wraps the app's own
// recover function, so a resolver panic reaches the event as an error.
//
// The extension never records Variables, field arguments, or the raw query, because any of
// them can hold a secret. It records a SHA-256 of the query instead. A body that fails to
// decode has no operation context, and its error holds the whole body, so that case
// records one fixed message.
package wloggqlgen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/99designs/gqlgen/graphql"
	gqlext "github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/jeremygprawira/wlog"
)

// Option configures the extension.
type Option func(*options)

// options holds the resolved settings of one extension.
type options struct{}

// Extension returns the gqlgen extension. Register it with srv.Use, and wrap the handler
// with the net/http middleware, so the fields land on the open request event.
func Extension(opts ...Option) graphql.HandlerExtension {
	cfg := options{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &extension{}
}

// extension adds the rpc fields to the open request event.
type extension struct{}

// ExtensionName names the extension.
func (*extension) ExtensionName() string { return "wlog" }

// Validate satisfies the extension contract. The extension reads no schema.
func (*extension) Validate(graphql.ExecutableSchema) error { return nil }

// InterceptOperation names the operation and its type on the open event.
func (e *extension) InterceptOperation(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
	if graphql.HasOperationContext(ctx) {
		recordOperation(ctx, graphql.GetOperationContext(ctx))
	}
	return next(ctx)
}

// InterceptResponse records the errors, the partial flag, and the complexity of one
// response.
func (e *extension) InterceptResponse(ctx context.Context, next graphql.ResponseHandler) *graphql.Response {
	res := next(ctx)
	if res == nil {
		return nil
	}
	recordResponse(ctx, res)
	return res
}

// recordOperation writes the operation name, its type, and the query hash.
func recordOperation(ctx context.Context, opCtx *graphql.OperationContext) {
	fields := []any{"system", "graphql", "method", operationName(opCtx)}
	group := map[string]any{}
	if opCtx.Operation != nil {
		group["type"] = string(opCtx.Operation.Operation)
	}
	if opCtx.RawQuery != "" {
		sum := sha256.Sum256([]byte(opCtx.RawQuery))
		group["query_sha256"] = hex.EncodeToString(sum[:])
	}
	if len(group) > 0 {
		fields = append(fields, "graphql", group)
	}
	wlog.SetGroup(ctx, "rpc", fields...)
}

// operationName names one operation: the name in the document, then the name the client
// sent, then anonymous.
func operationName(opCtx *graphql.OperationContext) string {
	if opCtx.Operation != nil && opCtx.Operation.Name != "" {
		return opCtx.Operation.Name
	}
	if opCtx.OperationName != "" {
		return opCtx.OperationName
	}
	return "anonymous"
}

// recordResponse writes the response counts, the partial flag, and the complexity.
func recordResponse(ctx context.Context, res *graphql.Response) {
	group := map[string]any{"errors_count": len(res.Errors)}
	if len(res.Errors) > 0 && len(res.Data) > 0 && string(res.Data) != "null" {
		group["partial"] = true
	}
	if graphql.HasOperationContext(ctx) {
		if stats := gqlext.GetComplexityStats(ctx); stats != nil {
			group["complexity"] = stats.Complexity
			if stats.ComplexityLimit > 0 {
				group["complexity_limit"] = stats.ComplexityLimit
			}
		}
	}
	wlog.SetGroup(ctx, "rpc", "graphql", group)
	recordErrors(ctx, res)
}

// protocolCodes names the error codes of the GraphQL transport, which report a fault in
// the request and not in the resolver.
var protocolCodes = map[string]bool{
	"GRAPHQL_PARSE_FAILED":      true,
	"GRAPHQL_VALIDATION_FAILED": true,
	"COMPLEXITY_LIMIT_EXCEEDED": true,
}

// recordErrors records a response's errors, and picks the level the spec asks for: a
// protocol error and a body that failed to decode give warn, and a resolver error gives
// error.
func recordErrors(ctx context.Context, res *graphql.Response) {
	if len(res.Errors) == 0 {
		return
	}
	if !graphql.HasOperationContext(ctx) {
		// A decode error holds the whole body in its message, so the event keeps one
		// fixed message.
		wlog.Error(ctx, errors.New("the GraphQL request body could not be decoded"))
		wlog.SetLevel(ctx, wlog.LevelWarn)
		return
	}
	recorded := false
	for _, item := range res.Errors {
		if protocolCodes[errorCode(item)] {
			continue
		}
		if err := resolverError(item); err != nil {
			wlog.Error(ctx, err)
			recorded = true
		}
	}
	if !recorded {
		wlog.SetLevel(ctx, wlog.LevelWarn)
	}
}

// errorCode returns the code of one GraphQL error, which the transport sets for a parse,
// a validation, and a complexity fault.
func errorCode(item *gqlerror.Error) string {
	if item.Extensions == nil {
		return ""
	}
	code, _ := item.Extensions["code"].(string)
	return code
}

// resolverError returns the error behind one GraphQL error, which the error presenter
// wrapped.
func resolverError(item *gqlerror.Error) error {
	if item.Err != nil {
		return item.Err
	}
	if item.Message != "" {
		return errors.New(item.Message)
	}
	return nil
}
