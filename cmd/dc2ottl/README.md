# dc2ottl

A command-line tool that fetches the derived columns defined on a Honeycomb dataset or environment,
translates the supported ones into OTTL, and prints a `transform` processor config block plus a
report of anything it couldn't translate.

Use this when you want to run the **stock `transform` processor** with no extra component. You paste
the generated block into your collector config and regenerate it when your derived columns change.
If you'd rather have a processor that refreshes itself, see
[`derivedcolumnprocessor`](../../derivedcolumnprocessor).

See the [repository README](../../README.md) for what does and does not translate and the semantic
caveats (no retroactivity, storage cost, span-attribute output).

## Install

```sh
go install github.com/honeycombio/derived-column-translator-processor/cmd/dc2ottl@latest
```

Or build from a checkout: `make build` (produces `bin/dc2ottl`).

## Authentication

Requires a Honeycomb **v1 Configuration Key** with the **"Manage Queries and Columns"** permission.
Not an ingest key and not a v2 Management Key. Pass it with `-api-key` or the `HONEYCOMB_API_KEY`
environment variable.

## Usage

```sh
dc2ottl -api-key $HONEYCOMB_CONFIG_KEY -dataset my-service > derived_columns.yaml
```

`stdout` is the config block (so it can be redirected or piped); the report goes to `stderr` by
default so it doesn't pollute the redirected config.

### Flags

| Flag          | Default                    | Description |
|---------------|----------------------------|-------------|
| `-api-key`    | `$HONEYCOMB_API_KEY`       | Configuration Key with "Manage Queries and Columns". Required. |
| `-api-url`    | `https://api.honeycomb.io` | API base URL. Use `https://api.eu1.honeycomb.io` for the EU instance. |
| `-dataset`    | `__all__`                  | Dataset slug, or `__all__` for environment-wide derived columns. |
| `-name`       | `derived_columns`          | Name suffix for the generated processor (`transform/<name>`). |
| `-error-mode` | `ignore`                   | `error_mode` for the generated processor: `ignore`, `silent`, or `propagate`. |
| `-report`     | _(stderr)_                 | Write the translation report to this file instead of stderr. |

## Output

### Config (stdout)

For a dataset with these derived columns:

| Alias            | Expression |
|------------------|------------|
| `is_slow`        | `$duration_ms > 1000` |
| `status_class`   | `IF($http.status_code >= 500, "5xx", $http.status_code >= 400, "4xx", "ok")` |
| `endpoint`       | `TO_LOWER(CONCAT($http.method, " ", $http.route))` |
| `latency_bucket` | `BUCKET($duration_ms, 100)` |

`dc2ottl` prints:

```yaml
processors:
  transform/derived_columns:
    error_mode: ignore
    trace_statements:
      - context: span
        statements:
          - 'set(attributes["is_slow"], false)'
          - 'set(attributes["is_slow"], true) where attributes["duration_ms"] > 1000'
          - 'set(attributes["status_class"], "ok")'
          - 'set(attributes["status_class"], "4xx") where attributes["http.status_code"] >= 400'
          - 'set(attributes["status_class"], "5xx") where attributes["http.status_code"] >= 500'
          - 'set(attributes["endpoint"], ToLowerCase(Concat([attributes["http.method"], " ", attributes["http.route"]], "")))'
```

Note how conditional derived columns expand into ordered `set(...) where ...` statements. They are
emitted lowest-priority first: because OTTL runs statements in order and the last write wins, a span
with status 500 matches both the `>= 400` and `>= 500` clauses and correctly ends up `"5xx"`.

### Report (stderr)

```
# Derived column translation report

3 translated, 1 skipped.

- OK   `is_slow` (2 statement(s))
- OK   `status_class` (3 statement(s))
- OK   `endpoint` (1 statement(s))
- SKIP `latency_bucket`: function BUCKET has no OTTL value mapping
```

Anything that can't be translated (functions with no OTTL equivalent, conditions that aren't
boolean expressions, etc.) is skipped with a reason rather than mistranslated.

## Using the output

Merge the generated `processors` block into your collector config and add the processor to a traces
pipeline, before the exporter to Honeycomb:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [transform/derived_columns]
      exporters: [otlp/honeycomb]
```

Regenerate and redeploy when your derived columns change. (The `derivedcolumnprocessor` does this
refresh automatically if you'd prefer not to.)
