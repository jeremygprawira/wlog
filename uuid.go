// This file builds the identifiers an event carries: a UUIDv7 event id, and the
// lowercase hex trace and span ids. Everything here uses crypto/rand, so the package
// keeps its standard-library-only rule.
package wlog

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// newEventID returns a UUID version 7 as a string, such as
// "0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f".
//
// The first 48 bits hold the millisecond of now, so the ids of one process sort by
// time, which is what a reader wants from an event id. The version and variant bits
// follow RFC 9562, because a consumer that parses the id expects them.
func newEventID(now time.Time) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice, and an event id is not worth a
		// panic on the emit path. A zero-filled id is still unique per event,
		// because the timestamp bits below stay in place.
		b = [16]byte{}
	}
	ms := uint64(now.UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70 // version 7
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant

	out := make([]byte, 0, 36)
	for i, c := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hexDigits[c>>4], hexDigits[c&0x0f])
	}
	return string(out)
}

// hexDigits is the alphabet of a hex string in lower case, which is the form the spec
// fixes for every id in the trace group.
const hexDigits = "0123456789abcdef"

// newTraceID returns a trace id: 16 random bytes as 32 lower-case hex characters,
// which is the W3C trace context width.
func newTraceID() string { return randomHex(16) }

// newSpanID returns a span id: 8 random bytes as 16 lower-case hex characters.
func newSpanID() string { return randomHex(8) }

// randomHex returns n random bytes as hex. A random source that fails yields an empty
// string, so a caller leaves the id out rather than writing a misleading one.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
