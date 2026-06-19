# derivedcolumn processor

A trace processor that reads the derived columns defined on a Honeycomb dataset or environment,
translates the supported ones into OTTL, and applies them to spans at ingest, materializing each
derived column as a span attribute. It refreshes on an interval and recompiles only when the rules
change.

This is the always-on alternative to the [`dc2ottl`](../cmd/dc2ottl) generator: instead of
generating `transform` config you paste and redeploy, the processor checks in with the Honeycomb
API itself. Both share the same translation core, so the OTTL they produce is identical.

See the [repository README](../README.md) for what does and does not translate, the semantic
caveats (no retroactivity, storage cost, span-attribute output), and authentication details.

## Authentication

Requires a Honeycomb **v1 Configuration Key** with the **"Manage Queries and Columns"** permission,
supplied as `api_key`. Not an ingest key and not a v2 Management Key. See the repository README's
Authentication section.

## Configuration

| Field              | Type              | Default                     | Required | Description |
|--------------------|-------------------|-----------------------------|----------|-------------|
| `api_key`          | string (secret)   | —                           | yes      | Configuration Key with "Manage Queries and Columns". |
| `api_url`          | string            | `https://api.honeycomb.io`  | no       | API base URL. Use `https://api.eu1.honeycomb.io` for the EU instance. |
| `dataset`          | string            | `__all__`                   | no       | Dataset slug to read derived columns from, or `__all__` for environment-wide derived columns. |
| `refresh_interval` | duration          | `5m`                        | no       | How often to re-fetch and recompile. Must be positive. |
| `error_mode`       | string            | `ignore`                    | no       | OTTL error handling: `ignore` (skip the failing statement), `silent` (skip, no log), or `propagate` (fail the batch). |
| `column_overrides` | map[string]string | `{}`                        | no       | Map a Honeycomb column name to an explicit OTTL path. Unlisted columns resolve to `attributes["<name>"]` (a span attribute). |

The processor also embeds the standard collector
[`confighttp.ClientConfig`](https://github.com/open-telemetry/opentelemetry-collector/blob/main/config/confighttp/README.md)
fields (squashed at the top level), so options such as `timeout`, `tls`, `proxy_url`, and extra
`headers` can be set alongside the fields above.

## Behaviour

- **Startup.** Fetches and compiles once. If the initial fetch fails, the processor logs a warning
  and starts with no statements (passing spans through unchanged); the refresh loop retries.
- **Refresh.** Every `refresh_interval`, re-fetches the derived columns and regenerates statements.
  It hashes the generated statement list and only recompiles and swaps when the hash changes, so a
  no-op refresh is cheap. A failed refresh keeps the last good statements.
- **Untranslatable derived columns.** Anything that can't be translated (e.g. `BUCKET`, `RAND`,
  `MOD`) is skipped; the count is logged. The rest still apply.
- **Output.** Each derived column is written to a span attribute named after its alias.

## Example

```yaml
receivers:
  otlp:
    protocols:
      grpc:
      http:

processors:
  derivedcolumn:
    api_key: ${env:HONEYCOMB_CONFIG_KEY}
    dataset: my-service
    refresh_interval: 5m
    error_mode: ignore
    column_overrides:
      service.name: resource.attributes["service.name"]

exporters:
  otlp/honeycomb:
    endpoint: api.honeycomb.io:443
    headers:
      x-honeycomb-team: ${env:HONEYCOMB_INGEST_KEY}

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [derivedcolumn]
      exporters: [otlp/honeycomb]
```

Place `derivedcolumn` after any processor that produces the columns its derived columns reference,
and before exporting to Honeycomb.
