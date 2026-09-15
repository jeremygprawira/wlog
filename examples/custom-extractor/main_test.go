package main

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestCustomExtractor_MapsErrorCode proves the custom extractor's code reaches the
// event's error.code.
func TestCustomExtractor_MapsErrorCode(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithErrorExtractor(codedExtractor{}))
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "payment.charge")
	decline(ctx)
	end()

	rec.RequireErrorCode(t, "PAYMENT_DECLINED")
}
