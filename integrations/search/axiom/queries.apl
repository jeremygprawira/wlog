// Saved queries for wlog on Axiom. Each line is one query with its name.

['errors by operation']
['wlog'] | where level == "error" | summarize events = count() by operation, ['error.code'] | sort by events desc

['p95 by operation']
['wlog'] | summarize p95 = percentile(duration_ms, 95) by operation | sort by p95 desc

['slowest calls']
['wlog'] | where duration_ms > 1000 | project timestamp, operation, duration_ms, summary | sort by duration_ms desc | limit 50

['one trace']
['wlog'] | where ['trace.trace_id'] == "4bf92f3577b34da6a3ce929d0e0e4736" | project timestamp, operation, summary | sort by timestamp asc
