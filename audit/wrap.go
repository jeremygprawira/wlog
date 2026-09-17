package audit

import (
	"context"
	"net/http"

	"github.com/jeremygprawira/wlog"
)

// Deny records a refused action. It is Do with outcome "denied", so a denial and a
// failure stay apart in a query.
func Deny(ctx context.Context, r Record) {
	r.Outcome = "denied"
	Do(ctx, r)
}

// Only emits the audit record as its own standalone event, even inside a request, and
// leaves the current event unchanged. Use it when the audit fact must not ride on the
// request event.
func Only(ctx context.Context, r Record) {
	r.Version = versionOf(r)
	ctx, end := wlog.Start(ctx, "audit."+r.Action)
	defer end()
	wlog.Append(ctx, auditKey, r)
}

// Wrap runs fn and records one audit fact with the outcome fn produced. A nil error
// records "success". Any other error records the failure on the event, keeps that
// error's code in audit.error_code, and records the outcome "error". An error the
// extractor marks 401 or 403, meaning a refusal, records "denied" instead, so a denial
// and a failure stay apart in a query.
//
// Wrap records the error through wlog.Error, so the Logger's own extractor runs and
// audit.error_code always agrees with error.code on the same event. A context with no
// open event falls back to the core extractor, which is the only one available there.
func Wrap(ctx context.Context, r Record, fn func() error) error {
	err := fn()
	if err == nil {
		r.Outcome = "success"
		Do(ctx, r)
		return nil
	}

	r.Outcome = "error"
	if wlog.HasEvent(ctx) {
		wlog.Error(ctx, err)
		if info, ok := wlog.CurrentError(ctx); ok {
			r.ErrorCode = info.Code
			if info.Status == http.StatusUnauthorized || info.Status == http.StatusForbidden {
				r.Outcome = "denied"
			}
		}
	} else {
		r.ErrorCode = wlog.DefaultExtractor().Extract(err).Code
	}
	Do(ctx, r)
	return err
}
