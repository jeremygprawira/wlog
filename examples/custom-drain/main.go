// Command custom-drain shows a Drain that counts events. A real drain would send them
// to a backend.
package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/jeremygprawira/wlog"
)

// countingDrain counts the events it receives. It is mutex-guarded, since drains can
// run from many goroutines at once.
type countingDrain struct {
	mu    sync.Mutex
	count int
}

// Send counts one event.
func (d *countingDrain) Send(_ context.Context, _ map[string]any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count++
}

// Count returns how many events Send has seen so far.
func (d *countingDrain) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.count
}

func main() {
	drain := &countingDrain{}
	log := wlog.New(wlog.WithDrains(drain))

	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "job.run")
	end()

	fmt.Println("events sent:", drain.Count())
}
