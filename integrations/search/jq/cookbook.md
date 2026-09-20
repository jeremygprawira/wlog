# jq cookbook

Twelve questions about wlog events, each answered with one `jq` program. The test runs
every program against `events.ndjson` and compares the output with the golden in the
`text` block under it. The programs work with jq and with gojq.

## 1. Which operations failed?

```jq
.[] | select(.level == "error") | .operation
```

```text
"POST /orders/{id}"
```

## 2. How many failures were there?

```jq
[.[] | select(.level == "error")] | length
```

```text
1
```

## 3. Which calls were slower than 100 ms?

```jq
.[] | select(.duration_ms > 100) | [.operation, .duration_ms]
```

```text
["POST /orders/{id}",840]
```

## 4. What is every operation with its duration?

```jq
.[] | [.operation, .duration_ms]
```

```text
["GET /orders/{id}",12]
["POST /orders/{id}",840]
["job cleanup",5]
```

## 5. What was the slowest call?

```jq
[.[] | .duration_ms] | max
```

```text
840
```

## 6. What is the total time in calls?

```jq
[.[] | .duration_ms] | add
```

```text
857
```

## 7. Which error code did a 5xx carry?

```jq
.[] | select(.http.status? and .http.status >= 500) | .error.code
```

```text
"PAYMENT_DECLINED"
```

## 8. What did one user do?

```jq
.[] | select(.user.id? == "u-1") | .operation
```

```text
"GET /orders/{id}"
"POST /orders/{id}"
```

## 9. Which operation belongs to one trace?

```jq
.[] | select(.trace.trace_id == "t-2") | .operation
```

```text
"POST /orders/{id}"
```

## 10. How many requests were there?

```jq
[.[] | select(.kind == "request")] | length
```

```text
2
```

## 11. Which routes were used?

```jq
[.[] | .http.route // empty] | unique
```

```text
["/orders/{id}"]
```

## 12. Which jobs ran?

```jq
.[] | select(.job?) | .job.name
```

```text
"cleanup"
```
