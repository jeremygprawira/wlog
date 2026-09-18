package clickhouse

import "strings"

// DDL returns the CREATE TABLE statement for the recommended schema that the drain fills,
// with a String column for the whole event. A String column works on every ClickHouse
// version, so it is the safe choice. Run the statement once, by hand or in a migration:
// the drain never creates or alters a table.
func DDL(database, table string) string {
	return `CREATE TABLE IF NOT EXISTS ` + database + `.` + table + ` (
    timestamp DateTime64(9),
    level LowCardinality(String),
    operation String,
    duration_ms Float64,
    outcome LowCardinality(String),
    service_name LowCardinality(String),
    service_version String,
    service_env LowCardinality(String),
    trace_id String,
    span_id String,
    request_id String,
    http_method LowCardinality(String),
    http_route String,
    http_status UInt16,
    error_code LowCardinality(String),
    error_message String,
    event String
) ENGINE = MergeTree
ORDER BY timestamp`
}

// DDLJSON is DDL with a JSON column for the whole event, which lets a query read one event
// field without parsing the text. The JSON type is production-ready from ClickHouse 25.3,
// so use DDL on an older server.
func DDLJSON(database, table string) string {
	return strings.Replace(DDL(database, table), "event String", "event JSON", 1)
}
