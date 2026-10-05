# wlog and evlog

evlog is the TypeScript logger whose model wlog follows: one wide event per unit of work,
enriched while the work runs, redacted once, and sent to any backend. wlog is that model in
Go.

## Where the mapping lives

`docs/evlog-parity.md` maps every page of the evlog documentation index to a wlog module, a
documented decision, or a gap. That page is the comparison. It names the wlog package for
each evlog page, and it says which surface wlog does not adopt.

## The shared shape

Both projects write one event per unit of work. Both enrich the event while the work runs.
Both redact the event once before a sink reads it. Both send the event to a list of drains.
The stages, the field names, and the drain interface follow that shape.

## What wlog does not adopt

The evlog surface that is TypeScript, browser, or vendor specific has no wlog counterpart.
The parity page lists it: browser and client logging, the Vite plugin, NuxtHub storage,
Better Auth, and the eve agent framework. The concept behind the last one, LLM
observability, is built as `llm`.

## Not measured

No page here holds a throughput comparison between wlog and evlog. The two run on
different runtimes, so a number from one says nothing about the other. The repository
measures wlog alone, with `make bench` and the budget in `bench/baseline.txt`.
