package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// sseWriteTimeout bounds one write to a subscribed client. A client that stops reading its
// socket must not hold the handler open forever, which would leak a goroutine per stalled
// viewer.
const sseWriteTimeout = 10 * time.Second

// SSEHandler streams every Send as a Server-Sent Event. A "?replay=N" query param
// sends the N most recent buffered events first, then switches to live. The stream
// closes (and its Subscribe registration is cleaned up, leaking no goroutine) when
// the client disconnects.
func (m *Memory) SSEHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// Subscribe BEFORE the replay is read, so an event sent while the replay is on the
		// wire waits in the channel instead of falling between the snapshot and the
		// subscription. At worst that delivers such an event twice, which a reader can
		// handle; losing it is not recoverable.
		ch := m.Subscribe(ctx)

		if replay, ok := replayCount(r); ok && replay > 0 {
			snap := m.Snapshot()
			if replay < len(snap) {
				snap = snap[len(snap)-replay:]
			}
			for _, e := range snap {
				if !writeSSE(w, e) {
					return
				}
			}
			flusher.Flush()
		}

		for {
			select {
			case e, ok := <-ch:
				if !ok {
					return
				}
				if !writeSSE(w, e) {
					return
				}
				// Each write gets its own deadline, so a stalled client is dropped rather
				// than parked forever. A ResponseWriter that cannot take a deadline is not
				// an error: the request context still bounds the handler.
				_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(sseWriteTimeout))
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
}

func replayCount(r *http.Request) (int, bool) {
	v := r.URL.Query().Get("replay")
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

// writeSSE writes one "data: <json>\n\n" frame. It reports false (stop streaming) if
// the write failed, e.g. because the client already disconnected.
func writeSSE(w http.ResponseWriter, event map[string]any) bool {
	b, err := json.Marshal(event)
	if err != nil {
		return true // skip an unmarshalable event, don't kill the stream over it
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", b)
	return err == nil
}
