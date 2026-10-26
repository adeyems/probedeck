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

Each check needs a unique nonempty name, URL, expected status (100–599), and timeout (1–30000 milliseconds). `contains` is an optional case-sensitive byte substring; empty means no text assertion. Unknown fields are rejected. A deck contains 1–100 checks and must fit in 1 MiB. Like Go's standard JSON decoder, duplicate JSON object keys use the last value; avoid duplicate keys.

Checks execute sequentially in file order using GET. Each timeout covers the request and body read. Response bodies are limited to 1 MiB; larger or unreadable responses fail the check. Later checks still run after failures. Reports include check names, actual status when available, and failure reasons; they do not contain response bodies or URLs.

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

This is an initial working slice, not a full workflow runner. It does not support POST requests, custom headers, JSON-path assertions, variables between steps, retries, concurrency, saved reports, or graceful signal cancellation. Request errors are deliberately generic. There is no timing analysis or load testing. The example server is a development fixture, not a production service.

See [ROADMAP.md](ROADMAP.md) for intended follow-up work. No submission eligibility or remaining-hours claim has been made.

MIT licensed.
