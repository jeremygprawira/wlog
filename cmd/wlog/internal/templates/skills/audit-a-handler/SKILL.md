---
name: audit-a-handler
description: Decide what needs an audit record, and write it with a catalog entry.
---

<!-- wlog:skill -->

# Audit a handler

Record an audit fact for a security-sensitive action: login, role change, refund, export,
or deletion.

1. Declare the code once in a catalog:
   `catalog.Entry{Code: "refund", Audit: &catalog.Audit{Action: "invoice.refund", ReasonRequired: true}}`.
2. Record it in the handler with `audit.Do(ctx, audit.Record{Actor: ..., Action: ...,
   Target: ..., Outcome: ...})`.
3. For a refused action use `audit.Deny`. For a wrapped call use `audit.Wrap`.
4. Add `audit.Enricher()`-style context through `audit.Catalog(registry)`.

An audit event always passes sampling. Run `audit.Journal(path)` next to the main drain,
and check the file with `audit.Verify(path)`.
