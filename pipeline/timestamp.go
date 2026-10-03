// This file holds the timestamp parse the drains used to copy.
package pipeline

import "time"

// ParseTimestamp reads text as an RFC 3339 time.
// ok is false when text is not a timestamp.
func ParseTimestamp(text string) (time.Time, bool) {
	stamp, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, false
	}
	return stamp, true
}
