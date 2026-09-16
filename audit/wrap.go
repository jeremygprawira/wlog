package audit

import (
	"context"

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
	wlog.Set(ctx, "audit", r)
}

// Wrap runs fn and records one audit fact with the outcome fn produced. A nil error
// records "success". Any other error records "error" and keeps that error's code in
// audit.error_code, so a denied action and a failed action stay apart.
func Wrap(ctx context.Context, r Record, fn func() error) error {
	err := fn()
	if err != nil {
		r.Outcome = "error"
		r.ErrorCode = wlog.DefaultExtractor().Extract(err).Code
	} else {
		r.Outcome = "success"
	}
	Do(ctx, r)
	return err
}
