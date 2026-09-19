// This file holds the aggregation of matched events: counts per group, the statistics of
// one numeric field, and the size estimate of the sample.
package query

import (
	"encoding/json"
	"sort"
	"time"
)

// Bucket is one group of events: its key, its count, and the statistics of one field.
type Bucket struct {
	Key   string
	Count int64
	Stats Stats
}

// Stats is the shape of one numeric field over one group: the count, three percentiles,
// and the largest value. A percentile uses the nearest rank, so a hand can check it.
type Stats struct {
	Count int64
	P50   float64
	P95   float64
	P99   float64
	Max   float64
}

// Counts returns one bucket per value of one path, sorted by count, and by key when two
// counts are equal. An event without the path lands in the bucket of the empty key.
func Counts(events []map[string]any, path string) []Bucket {
	index := map[string]int{}
	var buckets []Bucket
	for _, event := range events {
		key := Field(event, path)
		at, ok := index[key]
		if !ok {
			at = len(buckets)
			index[key] = at
			buckets = append(buckets, Bucket{Key: key})
		}
		buckets[at].Count++
	}
	sortBuckets(buckets)
	return buckets
}

// GroupStats returns one bucket per value of the group path, and each bucket holds the
// statistics of the numeric field.
func GroupStats(events []map[string]any, group, field string) []Bucket {
	samples := map[string][]float64{}
	var order []string
	for _, event := range events {
		key := Field(event, group)
		if _, ok := samples[key]; !ok {
			order = append(order, key)
		}
		if value, ok := number(FieldValueOr(event, field)); ok {
			samples[key] = append(samples[key], value)
		}
	}
	buckets := make([]Bucket, 0, len(order))
	for _, key := range order {
		buckets = append(buckets, Bucket{Key: key, Count: int64(len(samples[key])), Stats: summarize(samples[key])})
	}
	sortBuckets(buckets)
	return buckets
}

// Statistics returns the statistics of one numeric field over every event.
func Statistics(events []map[string]any, field string) Stats {
	samples := make([]float64, 0, len(events))
	for _, event := range events {
		if value, ok := number(FieldValueOr(event, field)); ok {
			samples = append(samples, value)
		}
	}
	return summarize(samples)
}

// sortBuckets sorts by count, and by key when two counts are equal, so a report is
// stable.
func sortBuckets(buckets []Bucket) {
	sort.SliceStable(buckets, func(i, j int) bool {
		if buckets[i].Count != buckets[j].Count {
			return buckets[i].Count > buckets[j].Count
		}
		return buckets[i].Key < buckets[j].Key
	})
}

// summarize computes the statistics of one sample, and it answers a zero value for an
// empty sample.
func summarize(samples []float64) Stats {
	if len(samples) == 0 {
		return Stats{}
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	return Stats{
		Count: int64(len(sorted)),
		P50:   percentile(sorted, 50),
		P95:   percentile(sorted, 95),
		P99:   percentile(sorted, 99),
		Max:   sorted[len(sorted)-1],
	}
}

// percentile returns the value at the nearest rank of one sorted sample.
func percentile(sorted []float64, percent int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := (percent*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// SizeRow is the size one kind and operation costs in one sample.
type SizeRow struct {
	Kind          string
	Operation     string
	Events        int64
	Bytes         int64
	BytesPerEvent float64
	GBPerMonth    float64
}

// Sizes returns one row per kind and operation of one sample, with the bytes per event
// and the gigabytes per month at the rate of the sample. A sample with no time span
// reports no rate.
func Sizes(events []map[string]any) []SizeRow {
	type key struct{ kind, operation string }
	index := map[key]int{}
	rows := []SizeRow{}
	var earliest, latest time.Time
	for _, event := range events {
		kind, _ := event["kind"].(string)
		operation, _ := event["operation"].(string)
		at, ok := index[key{kind, operation}]
		if !ok {
			at = len(rows)
			index[key{kind, operation}] = at
			rows = append(rows, SizeRow{Kind: kind, Operation: operation})
		}
		body, err := json.Marshal(event)
		if err != nil {
			continue
		}
		rows[at].Events++
		rows[at].Bytes += int64(len(body))
		if stamp, ok := eventTime(event); ok {
			if earliest.IsZero() || stamp.Before(earliest) {
				earliest = stamp
			}
			if latest.IsZero() || stamp.After(latest) {
				latest = stamp
			}
		}
	}

	// The rate needs a span. One event, or one moment, reports bytes per event alone.
	span := latest.Sub(earliest).Seconds()
	for i := range rows {
		rows[i].BytesPerEvent = float64(rows[i].Bytes) / float64(rows[i].Events)
		if span > 0 {
			monthly := rows[i].BytesPerEvent * float64(rows[i].Events) / span * 30 * 24 * 3600
			rows[i].GBPerMonth = monthly / 1e9
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Bytes != rows[j].Bytes {
			return rows[i].Bytes > rows[j].Bytes
		}
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Operation < rows[j].Operation
	})
	return rows
}

// FieldValueOr returns the raw value at one path, and a nil when the path is absent.
func FieldValueOr(event map[string]any, path string) any {
	value, _ := FieldValue(event, path)
	return value
}
