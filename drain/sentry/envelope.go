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

// buildEventEnvelope renders one envelope holding one error event. Sentry allows at most
// one event item per envelope, and the envelope header carries that item's event id, so a
// reader pairs the two.
func buildEventEnvelope(event map[string]any) ([]byte, error) {
	itemID, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("sentry: %w", err)
	}
	payload, err := json.Marshal(errorPayload(event, itemID))
	if err != nil {
		return nil, fmt.Errorf("sentry: marshal event: %w", err)
	}
	header, err := envelopeHeader(itemID)
	if err != nil {
		return nil, err
	}
	return envelopeBytes(header, []envelopeItem{{
		header:  map[string]any{"type": "event", "length": len(payload)},
		payload: payload,
	}})
}

// buildLogEnvelope renders one envelope holding every event of a batch as a log item. It
// returns nil when the batch holds nothing to log.
func buildLogEnvelope(events []map[string]any) ([]byte, error) {
	if len(events) == 0 {
		return nil, nil
	}
	items := make([]map[string]any, 0, len(events))
	for _, event := range events {
		items = append(items, logPayload(event))
	}
	payload, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		return nil, fmt.Errorf("sentry: marshal logs: %w", err)
	}
	header, err := envelopeHeader("")
	if err != nil {
		return nil, err
	}
	return envelopeBytes(header, []envelopeItem{{
		header: map[string]any{
			"type":         "log",
			"item_count":   len(items),
			"content_type": "application/vnd.sentry.items.log+json",
		},
		payload: payload,
	}})
}

// envelopeHeader renders the envelope header. An envelope that holds no event item leaves
// the event id out.
func envelopeHeader(eventID string) ([]byte, error) {
	header := map[string]any{"sent_at": time.Now().UTC().Format(time.RFC3339Nano)}
	if eventID != "" {
		header["event_id"] = eventID
	}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("sentry: marshal header: %w", err)
	}
	return headerBytes, nil
}

// envelopeBytes joins the header and the items into the on-wire envelope body.
func envelopeBytes(header []byte, items []envelopeItem) ([]byte, error) {
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
//
// Sentry wants one flat attribute map of {value, type} pairs and a trace id, so the event
// is flattened into typed attributes and a missing trace id is generated rather than left
// out.
func logPayload(event map[string]any) map[string]any {
	body := stringField(event, "operation")
	if body == "" {
		body = stringField(event, "message")
	}
	item := map[string]any{
		"timestamp":  secondsOf(event),
		"level":      stringField(event, "level"),
		"body":       body,
		"attributes": logAttributes(event),
	}
	traceID := ""
	if trace, ok := event["trace"].(map[string]any); ok {
		traceID = stringField(trace, "trace_id")
	}
	if traceID == "" {
		traceID, _ = randomID()
	}
	item["trace_id"] = traceID
	return item
}

// logAttributes flattens an event into the typed {value, type} pairs Sentry requires. A
// nested object becomes dotted keys, because Sentry rejects an untyped nested object.
func logAttributes(event map[string]any) map[string]any {
	out := map[string]any{}
	flattenAttributes(out, event, "")
	return out
}

// flattenAttributes copies one level of a nested event into out, joining an inner key to
// the path with a dot.
func flattenAttributes(out map[string]any, value map[string]any, prefix string) {
	for key, item := range value {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if nested, ok := item.(map[string]any); ok {
			flattenAttributes(out, nested, path)
			continue
		}
		out[path] = typedAttribute(item)
	}
}

// typedAttribute wraps one value in the {value, type} pair Sentry requires.
func typedAttribute(value any) map[string]any {
	switch v := value.(type) {
	case nil:
		return map[string]any{"value": "", "type": "string"}
	case bool:
		return map[string]any{"value": v, "type": "boolean"}
	case string:
		return map[string]any{"value": v, "type": "string"}
	case int:
		return map[string]any{"value": int64(v), "type": "integer"}
	case int64:
		return map[string]any{"value": v, "type": "integer"}
	case uint64:
		return map[string]any{"value": v, "type": "integer"}
	case float64:
		return map[string]any{"value": v, "type": "double"}
	case []any:
		return map[string]any{"value": v, "type": "array"}
	default:
		return map[string]any{"value": fmt.Sprint(value), "type": "string"}
	}
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
