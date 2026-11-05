# Remaining work

The first milestone provides sequential GET checks, status/text assertions, per-check timeouts and JSON results. The following are proposed work areas to scope and confirm before making a submission estimate:

- Authored multi-step scenarios: request methods, synthetic JSON bodies, explicit dependencies and validated variables passed between steps. Define what happens when a dependency fails.
Completed milestone 2: JSON Pointer deep-value assertions and exact response-header assertions, with missing/null distinctions and diagnostics. Standalone type assertions and stricter duplicate-key handling remain future work.
Completed milestone 3: overall deadline, Ctrl-C cancellation, partial ordered reports and cancellation of active requests.
- Bounded concurrency for independent scenarios: preserve ordered results and respect dependencies. Verify with tests that caps and cancellation work.
- Completed versioned report-file persistence. Atomic publication, comparison, readable summaries and explicit treatment of incompatible results remain.
- Configuration and packaging: stricter duplicate-key handling, validation errors tied to fields, portable release binaries and integration coverage.

Keep fixtures synthetic. Extending beyond loopback requires a deliberate scope decision. This roadmap is not an hours-worked record, a promised delivery schedule, or proof of programme eligibility.

Completed follow-up: strict deck object keys. See README for scope and remaining limitations.

Completed slice: explicit request methods and authored JSON bodies; dependencies and variables remain future work.

Completed scoped milestone: bounded independent concurrency. See README for exact semantics and remaining limitations.

Completed scoped follow-up: standalone json type assertions. See README for acceptance scope and remaining limitations.

Completed scoped follow-up: versioned saved reports. See README for precise behavior and remaining work.

Completed scoped follow-up: offline saved-report comparison. See README for limitations and remaining work.

Completed scoped follow-up: explicit sequential dependencies. See README for remaining limitations.

Completed scoped follow-up: atomic no-overwrite report publication. Directory crash durability and orphan-stage recovery remain outside this slice.

Completed scoped follow-up: optional readable offline comparison summaries with
sorted, escaped check names and a stop-reason change indicator. Detailed
field-level explanations and report provenance remain outside this slice.

Completed packaging slice: CI builds Linux/macOS amd64/arm64 CLI artifacts with
checksums and license after native tests pass. Signed releases, Windows support
and runtime coverage on every target remain unimplemented.

Completed scoped follow-up: response JSON assertions reject ambiguous duplicate keys and excessive nesting, with synthetic HTTP coverage. Status/text-only response handling is unchanged.
