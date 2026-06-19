# derived-column-translator-processor

Translate Honeycomb [derived columns](https://docs.honeycomb.io/reference/derived-column-formula/)
into [OTTL](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/pkg/ottl)
so an OpenTelemetry Collector `transform` processor can compute them **once at ingest** instead of
the Honeycomb query engine recomputing them **per event on every read**.

Two ways to use it (both share one translation core):

- **`dc2ottl` CLI** — fetches a dataset's derived columns and emits a `transform` processor config
  block plus a report of anything it couldn't translate. You review and paste the result.
- **`derivedcolumnprocessor`** (planned) — a collector processor that fetches derived columns at
  startup, refreshes periodically, and applies them inline. No config regeneration on DC edits.

## Status

Early development. The `dc2ottl` generator works end to end: fetch a dataset's derived columns,
translate the supported subset, and emit a `transform` processor config block plus a report.
The live `derivedcolumnprocessor` is not built yet.

## Layout

```
cmd/dc2ottl/          CLI generator (no collector dependencies)
pkg/honeycomb/        Honeycomb Configuration API client
pkg/hcdc/             HCDC expression parser → AST
pkg/translate/        AST → ordered (guard, value) branches
pkg/emit/             branches → OTTL statements + transform config + report
```

The root module holds the shared translation core and the CLI with no OpenTelemetry Collector
dependencies. The planned `derivedcolumnprocessor/` will be a **separate Go module** (tied in via
`go.work` for local dev) so the CLI stays lightweight. It will target the collector versions the
[honeycomb-collector-distro](https://github.com/honeycombio/honeycomb-collector-distro) tracks
(currently core `v0.154.0` / contrib `v0.153.0`).

## Caveats (read before adopting)

- **No retroactivity.** Materializing at ingest only affects data sent after deploy. Keep the
  derived column defined in Honeycomb so existing data still works.
- **Storage cost.** The computed value is stored on every event, trading query compute for ingest
  storage and egress.
- **Subset support.** Honeycomb derived-column functions with no OTTL equivalent (e.g. `BUCKET`,
  `RAND`, `MOD`, `METRO_HASH`) are skipped with a warning rather than mistranslated.
- **Output location.** Translated values are written as **span attributes** named after the
  derived-column alias, regardless of where the source columns live.
