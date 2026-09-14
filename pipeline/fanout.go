package pipeline

import (
	"context"

	"github.com/jeremygprawira/wlog"
)

// FanOut returns a wlog.Drain whose Send delivers to every drain concurrently,
// waiting for all to return. Each drain is already panic-isolated by
// wlog.Logger's own dispatch; FanOut adds no isolation of its own — it exists only so
// one slow or hanging drain does not delay (or block) delivery to the others.
func FanOut(drains ...wlog.Drain) wlog.Drain {
	return &fanOut{drains: drains}
}

type fanOut struct{ drains []wlog.Drain }

func (f *fanOut) Send(ctx context.Context, event map[string]any) {
	for _, d := range f.drains {
		go d.Send(ctx, event)
	}
}
