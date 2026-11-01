# ProbeDeck

A small Go CLI for checking synthetic local HTTP services from a JSON file. Write checks with expected status codes and optional text, then get a machine-readable pass/fail report.

## Try it

Requires Go 1.19 or newer. No external Go dependencies. CI tests current stable Go on Linux and macOS.

Start the included synthetic server in one terminal:

```sh
go run ./examples/server
```

In another terminal, from this repository:

```sh
go run . run examples/deck.json
go build -o probedeck .
./probedeck run examples/deck.json
```

The example checks `/health` for status 200 and text `ready`, and `/missing` for status 404. Stop the server with Ctrl-C when finished.

## Deck format

```json
{"checks":[{"name":"health","url":"http://127.0.0.1:8099/health","status":200,"contains":"ready","timeout_ms":1000}]}
```

Each check needs a unique nonempty name, URL, expected status (100–599), and timeout (1–30000 milliseconds). `contains` is an optional case-sensitive byte substring; empty means no text assertion. Unknown fields are rejected. A deck contains 1–100 checks and must fit in 1 MiB. Duplicate JSON object keys are rejected throughout the deck.

Checks execute sequentially in file order, using GET unless an explicit method is supplied. Each timeout covers the request and body read. Response bodies are limited to 1 MiB; larger or unreadable responses fail the check. Later checks still run after failures. Reports include check names, actual status when available, and failure reasons; they do not contain response bodies or URLs.

This first milestone only accepts HTTP URLs with literal loopback IP hosts, such as `127.0.0.1` or `[::1]`. It disables proxies and does not follow redirects. Use the included synthetic service or your own trusted synthetic local endpoints. Local endpoints can still have side effects; only target services you intend to check.

Exit codes for the built binary: **0** all checks pass, **1** at least one fails, **2** invalid input or report output error. Reports are JSON on stdout; input errors go to stderr. `go run` may wrap a nonzero program exit, so use the built binary when checking exit codes.

## Development

```sh
gofmt -w main.go main_test.go examples/server/main.go
go vet ./...
go test -race ./...
go build ./...
```

Tests use in-process synthetic HTTP servers and temporary directories. They cover status/text failures, redirects, timeouts, oversized bodies, invalid inputs and CLI exit codes.

## Scope

This is an initial working slice, not a full workflow runner. It does not support custom request headers, variables between steps, retries, concurrency, saved reports. Request errors are deliberately generic. There is no timing analysis or load testing. The example server is a development fixture, not a production service.

See [ROADMAP.md](ROADMAP.md) for intended follow-up work. No submission eligibility or remaining-hours claim has been made.

MIT licensed.

## Structured response assertions

Optional `headers` compares case-insensitive header names with exact, case-sensitive values. Missing headers fail even when the expected value is empty; repeated header values fail rather than being silently combined.

Optional `json` assertions select values using JSON Pointer (not JSONPath):

```json
{"checks":[{"name":"state","url":"http://127.0.0.1:8099/state","status":200,"timeout_ms":1000,"headers":{"Content-Type":"application/json"},"json":[{"pointer":"/items/0/ready","equals":true}]}]}
```

An empty pointer selects the whole document. Escape `/` as `~1` and `~` as `~0` in keys. Array indices are zero-based without leading zeroes. Missing values differ from JSON null. Objects and arrays compare deeply; types must match. Numbers retain exact JSON spelling, so `1`, `1.0` and `1e0` differ. A JSON assertion requires exactly one valid response JSON value. Failure messages identify the header or pointer without copying response values into reports. Status/text checks still work alongside these assertions. Duplicate response JSON keys currently use the last value.

## Run cancellation

`probedeck run DECK.json --deadline-ms 5000` sets an overall deadline (1–3600000 ms). Ctrl-C cancels active requests and emits a partial JSON report. Completed results remain; unstarted checks have `skipped: true`. A stopped run has `stopped` and exits 1; report-output failures exit 2. Per-check timeouts still apply. Without the option there is no overall deadline. Cancellation does not undo server-side effects.

## Strict deck object keys

Deck loading now rejects repeated JSON object keys at every level, including nested assertion values and equivalent escaped keys. Repeated names in separate objects are permitted. The preliminary validation limits nesting to 128 levels and runs before any HTTP request. This applies to configuration decks; duplicate keys in HTTP response JSON still use the last value.

## Authored requests

Optional `method` supports GET (default), HEAD, POST, PUT, PATCH, DELETE and OPTIONS. Optional `body` is an authored JSON value, including explicit null; when present it is sent with Content-Type application/json. GET/HEAD bodies are rejected. Existing loopback-only, no-redirect, time and size limits apply. Checks remain sequential and independent; variables, dependencies and rollback are not implemented. These requests can change the local service: use only intended synthetic fixtures.
