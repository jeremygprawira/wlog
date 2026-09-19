package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// ssePing is how often a v2 stream sends a ping comment, so a proxy does not close an
// idle connection.
const ssePing = 15 * time.Second

// StreamHandler answers GET /events/stream with Server-Sent Events. The first frame is
// hello, then one event frame per event, and a ping comment every 15 seconds.
// ?since=<event id or time> replays from that point.
func (m *Memory) StreamHandler(opts ...HandlerOption) http.Handler {
	cfg := resolveHandler(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !handlerAllowed(r, cfg) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		// Subscribe before the replay, so an event sent while the replay is on the wire
		// waits in the channel instead of falling between the snapshot and the
		// subscription.
		ch := m.Subscribe(ctx)

		_, _ = fmt.Fprintf(w, "event: hello\ndata: %s\n\n", helloFrame(m))
		for _, event := range replayEvents(m.Snapshot(), r.URL.Query().Get("since")) {
			if !writeEventFrame(w, event) {
				return
			}
		}
		flusher.Flush()

		ping := time.NewTicker(ssePing)
		defer ping.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ping.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case event, ok := <-ch:
				if !ok {
					return
				}
				if !writeEventFrame(w, event) {
					return
				}
				flusher.Flush()
			}
		}
	})
}

// helloFrame is the first frame of a v2 stream. It names the store and the number of
// events the store holds now.
func helloFrame(m *Memory) string {
	body, err := json.Marshal(map[string]any{
		"version": 1,
		"store":   "memory",
		"size":    len(m.Snapshot()),
	})
	if err != nil {
		return "{}"
	}
	return string(body)
}

// writeEventFrame writes one event as one SSE frame, and it reports whether the write
// reached the client.
func writeEventFrame(w io.Writer, event map[string]any) bool {
	body, err := json.Marshal(event)
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(w, "event: event\ndata: %s\n\n", body)
	return err == nil
}

// replayEvents returns the events after the point that since names: an RFC 3339 time, or
// an event id. An unknown point replays nothing.
func replayEvents(snapshot []map[string]any, since string) []map[string]any {
	if since == "" {
		return nil
	}
	if stamp, err := time.Parse(time.RFC3339, since); err == nil {
		for i, event := range snapshot {
			if at, ok := eventTime(event); ok && !at.Before(stamp) {
				return snapshot[i:]
			}
		}
		return nil
	}
	for i, event := range snapshot {
		if id, _ := event["event_id"].(string); id == since {
			if i+1 < len(snapshot) {
				return snapshot[i+1:]
			}
			return nil
		}
	}
	return nil
}
