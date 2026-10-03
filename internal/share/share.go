// Package share holds the helpers the drains used to copy.
// One home for each helper keeps the copies from drifting apart.
package share

import "os"

// FirstEnv returns the first name that holds a non-empty value.
// A blank value is skipped, so it does not hide the next name.
func FirstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

// ChunkEnd returns the end index of the next chunk.
// The chunk starts at start and stays under maxBytes.
// One event is always included, even when it alone passes the cap.
// sizeAt returns the encoded size of the event at that index.
// A negative size ends the chunk after that event.
func ChunkEnd(count, start, maxBytes int, sizeAt func(index int) int) int {
	size := 0
	for i := start; i < count; i++ {
		eventSize := sizeAt(i)
		if eventSize < 0 {
			return i + 1
		}
		if i > start && size+eventSize > maxBytes {
			return i
		}
		size += eventSize
	}
	return count
}
