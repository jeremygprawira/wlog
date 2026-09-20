-- Views for the three questions a team asks first: which operations fail, how slow they
-- are, and which systems the app called. wlog writes one JSON object per line, and
-- ClickHouse reads it with JSONEachRow.

CREATE VIEW IF NOT EXISTS wlog_errors_by_operation AS
SELECT
    JSONExtractString(event, 'operation') AS operation,
    JSONExtractString(event, 'error.code') AS code,
    count() AS events
FROM wlog_events
WHERE JSONExtractString(event, 'level') = 'error'
GROUP BY operation, code
ORDER BY events DESC;

CREATE VIEW IF NOT EXISTS wlog_p95_by_operation AS
SELECT
    JSONExtractString(event, 'operation') AS operation,
    quantile(0.95)(JSONExtractFloat(event, 'duration_ms')) AS p95_ms,
    count() AS events
FROM wlog_events
GROUP BY operation
ORDER BY p95_ms DESC;

CREATE VIEW IF NOT EXISTS wlog_calls_by_system AS
SELECT
    JSONExtractString(call, 'system') AS system,
    JSONExtractString(call, 'kind') AS kind,
    count() AS calls,
    sum(JSONExtractFloat(call, 'duration_ms')) AS total_ms
FROM wlog_events
ARRAY JOIN JSONExtractArrayRaw(event, 'calls') AS call
GROUP BY system, kind
ORDER BY calls DESC;
