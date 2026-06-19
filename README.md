# derived-column-translator-processor

Translate Honeycomb [derived columns](https://docs.honeycomb.io/reference/derived-column-formula/)
into [OTTL](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/pkg/ottl)
so an OpenTelemetry Collector `transform` processor can compute them **once at ingest** instead of
the Honeycomb query engine recomputing them **per event on every read**.

Two ways to use it (both share one translation core):

- **`dc2ottl` CLI** — fetches a dataset's derived columns and emits a `transform` processor config
  block plus a report of anything it couldn't translate. You review and paste the result, and
  regenerate periodically as derived columns change. Use this if you want to run the stock
  `transform` processor with no extra component. See [its README](cmd/dc2ottl/README.md) for flags,
  output, and an example.
- **`derivedcolumnprocessor`** — a collector processor that fetches derived columns at startup,
  refreshes on an interval, and applies them inline. It recompiles only when the rules actually
  change. Use this if you don't want to regenerate and redeploy config on every derived-column edit.
  See [its README](derivedcolumnprocessor/README.md) for the full config reference and an example.

## Authentication

Both modes read derived columns through the Honeycomb **v1 Configuration API**
(`GET /1/derived_columns/{dataset}`, `X-Honeycomb-Team` header). That requires a **Configuration
Key** with the **"Manage Queries and Columns"** permission.

- Not an **ingest key** (those only send events).
- Not a **v2 Management Key** — derived columns are not exposed on the v2 Management API.
- The "Manage Queries and Columns" permission is all-or-nothing: it grants read *and* write to
  queries and columns. Honeycomb has no read-only derived-column permission for v1 keys, so scope
  the key tightly and treat it as sensitive.

Pass `__all__` as the dataset to read environment-wide derived columns.

## Status

Early development. Both the `dc2ottl` generator and the `derivedcolumnprocessor` work end to end and
are tested. Not yet tagged or wired into the distro (see Release below).

## Layout

```
cmd/dc2ottl/             CLI generator (root module, no collector dependencies)
pkg/honeycomb/           Honeycomb Configuration API client
pkg/hcdc/                HCDC expression parser → AST
pkg/translate/           AST → ordered (guard, value) branches
pkg/emit/                branches → OTTL statements + transform config + report
derivedcolumnprocessor/  collector processor (separate Go module)
go.work                  ties the two modules together for local dev
```

The root module holds the shared translation core and the CLI with no OpenTelemetry Collector
dependencies. `derivedcolumnprocessor/` is a **separate Go module** so the CLI stays lightweight; it
targets the collector versions the
[honeycomb-collector-distro](https://github.com/honeycombio/honeycomb-collector-distro) tracks
(core `v0.154.0` / contrib `v0.153.0`).

## Caveats (read before adopting)

- **No retroactivity.** Materializing at ingest only affects data sent after deploy. Keep the
  derived column defined in Honeycomb so existing data still works.
- **Storage cost.** The computed value is stored on every event, trading query compute for ingest
  storage and egress.
- **Subset support.** Honeycomb derived-column functions with no OTTL equivalent (e.g. `BUCKET`,
  `RAND`, `MOD`, `METRO_HASH`) are skipped with a warning rather than mistranslated.
- **Output location.** Translated values are written as **span attributes** named after the
  derived-column alias, regardless of where the source columns live.

## Release

The processor module depends on the root module via a local `replace` directive, so it builds in
this workspace. Before it can be pulled into the distro's `builder-config.yaml`, the root module
must be tagged and the processor's `go.mod` must `require` that tag with the `replace` removed.
