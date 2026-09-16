package clickhouse

// DDL returns the CREATE TABLE statement for the recommended schema that rowFor fills.
// Run it once, by hand or in a migration. The drain never creates or alters a table.
func DDL(table string) string {
	return `CREATE TABLE IF NOT EXISTS ` + table + ` (
    timestamp DateTime64(9),
    level LowCardinality(String),
    operation String,
    duration_ms UInt64,
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
    event JSON
) ENGINE = MergeTree
ORDER BY timestamp`
}
