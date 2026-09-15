// Command logger-output shows wlog events reaching a team's existing loggers, and a
// slog call made inside an event folding back into that event's logs[].
package main

import (
	"context"
	"log/slog"
	"os"

	"go.uber.org/zap"

	"github.com/jeremygprawira/wlog"
	wlogslog "github.com/jeremygprawira/wlog/log/slog"
	wlogzap "github.com/jeremygprawira/wlog/log/zap"
)

// newLogger writes every event to both zapLogger and slogHandler.
func newLogger(zapLogger *zap.Logger, slogHandler slog.Handler) *wlog.Logger {
	return wlog.New(wlog.WithDrains(
		wlogzap.Drain(zapLogger),
		wlogslog.Drain(slogHandler),
	))
}

func main() {
	logger := newLogger(zap.NewExample(), slog.NewJSONHandler(os.Stdout, nil))
	ctx := logger.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "job.run")
	// A library that logs through slog inside the event folds into logs[].
	slog.New(wlogslog.Handler(slog.NewJSONHandler(os.Stdout, nil))).InfoContext(ctx, "step done", "step", 1)
	end()
}
