// Command custom-extractor shows a custom ErrorExtractor that reads a code from the
// error instead of using wlog's default INTERNAL code.
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jeremygprawira/wlog"
)

// codedError carries a machine-readable code, the way a payments library would.
type codedError struct{ code string }

// Error returns a human-readable message.
func (e codedError) Error() string { return "declined: " + e.code }

// codedExtractor maps a codedError to its code, and every other error to INTERNAL.
type codedExtractor struct{}

// Extract reads the error and returns the fields the event should carry.
func (codedExtractor) Extract(err error) wlog.ErrorInfo {
	var coded codedError
	if errors.As(err, &coded) {
		return wlog.ErrorInfo{Code: coded.code, Message: coded.Error()}
	}
	return wlog.ErrorInfo{Code: "INTERNAL", Message: err.Error()}
}

// decline reports a failed charge on the current event.
func decline(ctx context.Context) {
	wlog.Error(ctx, codedError{code: "PAYMENT_DECLINED"})
}

func main() {
	log := wlog.New(wlog.WithErrorExtractor(codedExtractor{}))
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "payment.charge")
	decline(ctx)
	end()

	fmt.Println("charge failed")
}
