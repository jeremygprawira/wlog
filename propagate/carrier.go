// This file holds the carriers: the three shapes of header store an adapter meets. A
// carrier reads and writes one key at a time, so Extract and Inject never care which
// library carried the message.
package propagate

import (
	"net/http"
	"sort"
)

// Carrier holds the headers of one incoming or outgoing message. Get matches a key
// without regard to case for a HeaderCarrier, and exactly for the other carriers.
type Carrier interface {
	Get(key string) string
	Set(key, value string)
	Keys() []string
}

// HeaderCarrier adapts an HTTP header set, which is also how gRPC metadata arrives
// through an adapter. The keys follow the canonical form of net/http, so Get ignores
// case.
type HeaderCarrier http.Header

// Get returns the first value of key, and an empty string when key is absent.
func (h HeaderCarrier) Get(key string) string { return http.Header(h).Get(key) }

// Set replaces every value of key with value.
func (h HeaderCarrier) Set(key, value string) { http.Header(h).Set(key, value) }

// Keys returns the header names this carrier holds, sorted.
func (h HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// MapCarrier adapts a plain map of string to string, which is what SQS attributes,
// Pub/Sub attributes, and CloudEvents carry. A key matches exactly, because such a map
// has no canonical form.
type MapCarrier map[string]string

// Get returns the value of key exactly.
func (m MapCarrier) Get(key string) string { return m[key] }

// Set stores value under key exactly.
func (m MapCarrier) Set(key, value string) { m[key] = value }

// Keys returns the keys this carrier holds, sorted.
func (m MapCarrier) Keys() []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// BytesCarrier adapts headers that hold bytes, which is how Kafka and NATS carry them. A
// key matches exactly.
type BytesCarrier struct{ values map[string][]byte }

// NewBytesCarrier wraps a map of byte headers. A nil map is allowed, and Set fills it.
func NewBytesCarrier(values map[string][]byte) *BytesCarrier {
	if values == nil {
		values = map[string][]byte{}
	}
	return &BytesCarrier{values: values}
}

// Get returns the bytes of key as text, and an empty string when key is absent.
func (c *BytesCarrier) Get(key string) string { return string(c.values[key]) }

// Set stores value as the bytes of key.
func (c *BytesCarrier) Set(key, value string) { c.values[key] = []byte(value) }

// Keys returns the keys this carrier holds, sorted.
func (c *BytesCarrier) Keys() []string {
	keys := make([]string, 0, len(c.values))
	for key := range c.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
