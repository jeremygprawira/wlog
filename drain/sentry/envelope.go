package sentry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// envelopeItem is one item inside a Sentry envelope: a header line and a payload.
type envelopeItem struct {
	header  map[string]any
	payload []byte
}

// buildEnvelope renders one Sentry envelope from a batch. It returns nil when the batch
// has nothing to send.
func buildEnvelope(events []map[string]any, allEvents bool) ([]byte, error) {
	envelopeID, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("sentry: %w", err)
	}

	var items []envelopeItem
	var logItems []map[string]any
	for _, event := range events {
		if isErrorEvent(event) {
			itemID, err := randomID()
			if err != nil {
				return nil, fmt.Errorf("sentry: %w", err)
			}
			payload, err := json.Marshal(errorPayload(event, itemID))
			if err != nil {
				return nil, fmt.Errorf("sentry: marshal event: %w", err)
			}
			items = append(items, envelopeItem{
				header:  map[string]any{"type": "event", "length": len(payload)},
				payload: payload,
			})
		}
		if allEvents {
			logItems = append(logItems, logPayload(event))
		}
	}
	if len(logItems) > 0 {
		payload, err := json.Marshal(map[string]any{"items": logItems})
		if err != nil {
			return nil, fmt.Errorf("sentry: marshal logs: %w", err)
		}
		items = append(items, envelopeItem{
			header: map[string]any{
				"type":         "log",
				"item_count":   len(logItems),
				"content_type": "application/vnd.sentry.items.log+json",
			},
			payload: payload,
		})
	}
	if len(items) == 0 {
		return nil, nil
	}

	header, err := json.Marshal(map[string]any{
		"event_id": envelopeID,
		"sent_at":  time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, fmt.Errorf("sentry: marshal header: %w", err)
	}

	var body strings.Builder
	body.Write(header)
	body.WriteByte('\n')
	for _, item := range items {
		itemHeader, err := json.Marshal(item.header)
		if err != nil {
			return nil, fmt.Errorf("sentry: marshal item header: %w", err)
		}
		body.Write(itemHeader)
		body.WriteByte('\n')
		body.Write(item.payload)
		body.WriteByte('\n')
	}
	return []byte(body.String()), nil
}

// isErrorEvent reports whether an event belongs in a Sentry issue.
func isErrorEvent(event map[string]any) bool {
	if level, _ := event["level"].(string); level == "error" {
		return true
	}
	_, hasError := event["error"]
	return hasError
}

// errorPayload maps one event to a Sentry event item. The fingerprint is the error code,
// then the error kind, then INTERNAL, so the same code always groups into one issue.
func errorPayload(event map[string]any, itemID string) map[string]any {
	errInfo, _ := event["error"].(map[string]any)
	fingerprint := stringField(errInfo, "code")
	if fingerprint == "" {
		fingerprint = stringField(errInfo, "kind")
	}
	if fingerprint == "" {
		fingerprint = "INTERNAL"
	}

	message := stringField(errInfo, "message")
	if message == "" {
		message = stringField(event, "operation")
	}

	payload := map[string]any{
		"event_id":    itemID,
		"platform":    "go",
		"level":       "error",
		"logger":      "wlog",
		"message":     map[string]any{"formatted": message},
		"fingerprint": []string{fingerprint},
		"tags":        tagsFor(event),
		"contexts":    map[string]any{"wlog": event},
	}
	if timestamp := stringField(event, "timestamp"); timestamp != "" {
		payload["timestamp"] = timestamp
	}
	if exception := exceptionFor(errInfo); exception != nil {
		payload["exception"] = exception
	}
	return payload
}

// exceptionFor builds a Sentry exception from the error detail, when there is one.
func exceptionFor(errInfo map[string]any) map[string]any {
	if errInfo == nil {
		return nil
	}
	value := stringField(errInfo, "message")
	kind := stringField(errInfo, "kind")
	if value == "" && kind == "" {
		return nil
	}
	if kind == "" {
		kind = "error"
	}
	return map[string]any{"values": []map[string]any{{"type": kind, "value": value}}}
}

// tagsFor reads the tags Sentry filters on: service, env, level, and operation.
func tagsFor(event map[string]any) map[string]string {
	tags := map[string]string{}
	if service, ok := event["service"].(map[string]any); ok {
		if name := stringField(service, "name"); name != "" {
			tags["service"] = name
		}
		if env := stringField(service, "env"); env != "" {
			tags["env"] = env
		}
	}
	if level := stringField(event, "level"); level != "" {
		tags["level"] = level
	}
	if operation := stringField(event, "operation"); operation != "" {
		tags["operation"] = operation
	}
	return tags
}

// logPayload maps one event to a Sentry log item for AllEvents mode.
func logPayload(event map[string]any) map[string]any {
	body := stringField(event, "operation")
	if body == "" {
		body = stringField(event, "message")
	}
	item := map[string]any{
		"timestamp":  secondsOf(event),
		"level":      stringField(event, "level"),
		"body":       body,
		"attributes": map[string]any{"wlog": event},
	}
	if trace, ok := event["trace"].(map[string]any); ok {
		if traceID := stringField(trace, "trace_id"); traceID != "" {
			item["trace_id"] = traceID
		}
	}
	return item
}

// secondsOf reads the event timestamp as Unix seconds with a fractional part.
func secondsOf(event map[string]any) float64 {
	if text := stringField(event, "timestamp"); text != "" {
		if t, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return float64(t.UnixNano()) / 1e9
		}
	}
	return float64(time.Now().UnixNano()) / 1e9
}

// stringField reads one string field, treating a missing or non-string value as "".
func stringField(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return value
}

// randomID returns 32 lowercase hex characters, the Sentry event id shape.
func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("random id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
