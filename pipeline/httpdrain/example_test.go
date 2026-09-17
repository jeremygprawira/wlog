package httpdrain_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// exampleSender is a third-party drain: it turns one batch of events into one POST. A
// real drain maps the events to its vendor's body shape here.
type exampleSender struct{ client *httpdrain.Client }

// SendBatch posts the batch.
func (s exampleSender) SendBatch(ctx context.Context, events []map[string]any) error {
	body, err := json.Marshal(events)
	if err != nil {
		return err
	}
	return s.client.Post(ctx, body, "application/json")
}

// Example shows the shape a third-party drain uses: an httpdrain.Client for the wire
// format, and pipeline.Wrap for the batching, retry, and buffer. It is the same helper
// every built-in network drain builds on.
func Example() {
	var received atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := httpdrain.New(srv.URL, httpdrain.WithSource("example"), httpdrain.WithGzip(true))
	drain := pipeline.Wrap(exampleSender{client})

	drain.Send(context.Background(), map[string]any{"message": "hello"})
	// Close flushes the batch. Logger.Close does this for a drain the Logger owns.
	closer, ok := drain.(interface{ Close(context.Context) error })
	if !ok {
		panic("pipeline.Wrap has no Close")
	}
	_ = closer.Close(context.Background())

	fmt.Println(received.Load())
	// Output: 1
}
