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

This is an initial working slice, not a full workflow runner. It does not support custom request headers, variables between steps or retries; bounded independent concurrency, saved reports and sequential dependencies are described below. Request errors are deliberately generic. There is no timing analysis or load testing. The example server is a development fixture, not a production service.

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

Optional `method` supports GET (default), HEAD, POST, PUT, PATCH, DELETE and OPTIONS. Optional `body` is an authored JSON value, including explicit null; when present it is sent with Content-Type application/json. GET/HEAD bodies are rejected. Existing loopback-only, no-redirect, time and size limits apply. Checks run sequentially by default (opt-in independent concurrency is described below); variables and rollback remain unimplemented; sequential dependencies are described below. These requests can change the local service: use only intended synthetic fixtures.

## Bounded independent concurrency

`probedeck run deck.json --concurrency 4 --deadline-ms 5000` runs up to four independent checks concurrently (1–32; default 1). Reports remain in authored order even when requests finish out of order. Cancellation stops active requests and marks unstarted checks skipped. Each worker owns its request lifecycle. This is not dependency-aware workflow execution: do not enable concurrency for checks that rely on each other or mutate shared synthetic state in a particular order. Duplicate or unknown run options are rejected.

## Standalone JSON type assertions

A JSON assertion can use `{"pointer":"/items","type":"array"}` instead of `equals`. Supported types are object, array, string, number, boolean and null. Each assertion must specify exactly one of type or equals. Missing paths fail even when type is null; number includes all JSON numbers, with no integer subtype or coercion. Multiple assertions can check type and exact value separately. Existing equals assertions remain compatible.

## Versioned saved reports

`probedeck run DECK.json --output NEW.json` saves `{ "version": 1, "report": ... }` while retaining the existing stdout report and pass/fail exit code. Existing output paths are rejected before requests; atomic no-overwrite publication also prevents overwrites if a destination appears during execution. Results include stopped/skipped checks. Files are synced and use mode 0600 where supported. The limit is 8 MiB before writing. A write failure returns exit 2. Reports are written and synced to a private same-directory staging file, then published with a hard link. Handled failures clean up staging files; a process crash can leave a staging file but cannot expose a partially written destination. Filesystems without hard-link support fail without a non-atomic fallback. Directory entries are not synced, so crash durability is not guaranteed. Saving occurs after requests, so a failed save does not undo requests. Migration support remains future work; offline version-1 comparison is described below.

## Offline saved-report comparison

`probedeck compare BEFORE.json AFTER.json` reads only version-1 saved reports, bounded to 8 MiB each, rejecting duplicate/unknown keys, invalid required outcomes, duplicate check names and inconsistent summaries. Checks match by name, independent of order; additions, removals, changed results and stopped-reason changes are reported. Problems compare exactly in order. Exit codes are 0 match, 1 differences, 2 invalid input/write error. No requests run. Matching reports do not prove equivalent decks, URLs or assertions because version 1 does not store deck provenance.

## Explicit sequential dependencies

Optional `depends_on` lists distinct earlier check names. All dependencies must pass before a check runs; failed, skipped or cancelled dependencies cause a visible skipped result, and independent checks still run. Earlier-only references reject unknown names, forward references and cycles during deck validation. Dependent decks require concurrency 1; selecting higher concurrency fails before requests. This provides ordering and failure propagation, not response-variable extraction, transactional rollback or concurrent dependency scheduling.

### Readable offline comparison

`go run . compare baseline.json candidate.json --text` prints a short summary
with added, removed and changed check names in sorted order, plus whether the
run stop reason changed. Names are quoted and control characters escaped.
Without `--text`, the existing JSON comparison format remains the default.
Exit codes remain 0 for matching reports, 1 for differences, and 2 for invalid
input or output errors. Both modes validate the reports before writing results
and make no HTTP requests. This summary identifies changed checks; it does not
show field-level differences or establish that two reports used the same deck.

## CI binary artifacts

Successful CI runs build CGO-free CLI binaries for Linux and macOS on amd64 and
arm64. Download the matching `probedeck-OS-ARCH` artifact from the GitHub Actions
run. Each includes the MIT license and `SHA256SUMS`; verify it with
`sha256sum -c SHA256SUMS` (Linux) or `shasum -a 256 -c SHA256SUMS` (macOS), then
run `chmod +x probedeck` because artifact downloads do not preserve executable
permissions. Choose arm64 for Apple Silicon and amd64 for Intel Macs.

Artifacts are produced only after Linux/macOS tests pass. They are development
builds tied to a CI commit, not signed/notarized releases. Cross-compilation does
not establish runtime coverage on every architecture. Windows binaries and
public release publication remain outside this milestone.
