// Package wlogopenfeature records OpenFeature flag evaluations on the event of the call's
// context. The hook folds one entry per evaluation into the feature_flags array.
package wlogopenfeature

import (
	"context"
	"strings"

	"github.com/open-feature/go-sdk/openfeature"

	"github.com/jeremygprawira/wlog"
)

// maxEntries is the most entries one event keeps. Later evaluations are dropped.
const maxEntries = 50

// Option configures the hook.
type Option func(*hook)

// WithValues also records the bool, int, and float values of an evaluation.
func WithValues() Option {
	return func(h *hook) { h.values = true }
}

// Hook returns the OpenFeature hook. Pass it to openfeature.WithHooks.
func Hook(opts ...Option) openfeature.Hook {
	h := &hook{}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// hook folds each evaluation into the event of the context.
type hook struct {
	values bool
}

// Before does nothing, because the event needs no change before an evaluation.
func (h *hook) Before(ctx context.Context, hookContext openfeature.HookContext, hints openfeature.HookHints) (*openfeature.EvaluationContext, error) {
	defer h.recover()
	return nil, nil
}

// After records one evaluation that reached a variant.
func (h *hook) After(ctx context.Context, hookContext openfeature.HookContext, details openfeature.InterfaceEvaluationDetails, hints openfeature.HookHints) error {
	defer h.recover()
	h.record(ctx, hookContext, details, "")
	return nil
}

// Error records one evaluation that failed, with the error code of the failure.
func (h *hook) Error(ctx context.Context, hookContext openfeature.HookContext, err error, hints openfeature.HookHints) {
	defer h.recover()
	h.record(ctx, hookContext, openfeature.InterfaceEvaluationDetails{}, errorCodeOf(err))
}

// Finally does nothing, because After and Error already recorded the evaluation.
func (h *hook) Finally(ctx context.Context, hookContext openfeature.HookContext, details openfeature.InterfaceEvaluationDetails, hints openfeature.HookHints) {
	defer h.recover()
}

// recover keeps a panic inside the hook away from the SDK, which does not recover hook
// panics.
func (h *hook) recover() {
	_ = recover()
}

// record appends one entry to the feature_flags array of the event. It never records the
// evaluation context, a string or object value, the flag metadata, or the default value.
func (h *hook) record(ctx context.Context, hookContext openfeature.HookContext, details openfeature.InterfaceEvaluationDetails, errorCode string) {
	if !wlog.HasEvent(ctx) || h.full(ctx) {
		return
	}
	reason := string(details.Reason)
	if errorCode != "" {
		reason = string(openfeature.ErrorReason)
	}
	entry := map[string]any{
		"key":        hookContext.FlagKey(),
		"type":       hookContext.FlagType().String(),
		"provider":   hookContext.ProviderMetadata().Name,
		"variant":    details.Variant,
		"reason":     reason,
		"error_code": errorCode,
	}
	if h.values {
		switch value := details.Value.(type) {
		case bool, int, int64, float32, float64:
			entry["value"] = value
		}
	}
	wlog.Append(ctx, "feature_flags", entry)
}

// errorCodeOf reads the code of an OpenFeature error. The SDK keeps the code unexported
// and writes it into the message, so the message is the only source.
// ponytail: message match on the SDK's own code list; call a Code() method when the SDK
// exports one.
func errorCodeOf(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, code := range []openfeature.ErrorCode{
		openfeature.ProviderNotReadyCode,
		openfeature.ProviderFatalCode,
		openfeature.FlagNotFoundCode,
		openfeature.ParseErrorCode,
		openfeature.TypeMismatchCode,
		openfeature.TargetingKeyMissingCode,
		openfeature.InvalidContextCode,
		openfeature.GeneralCode,
	} {
		if strings.Contains(message, string(code)) {
			return string(code)
		}
	}
	return ""
}

// full reports whether the event already holds the most entries.
func (h *hook) full(ctx context.Context) bool {
	value, ok := wlog.Field(ctx, "feature_flags")
	if !ok {
		return false
	}
	entries, ok := value.([]any)
	return ok && len(entries) >= maxEntries
}
