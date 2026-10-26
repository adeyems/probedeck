# Remaining work

The first milestone provides sequential GET checks, status/text assertions, per-check timeouts and JSON results. The following are proposed work areas to scope and confirm before making a submission estimate:

- Authored multi-step scenarios: request methods, synthetic JSON bodies, explicit dependencies and validated variables passed between steps. Define what happens when a dependency fails.
- Structured assertions: JSON field values/types and response headers, with useful diagnostics and clear missing-value semantics.
- Run lifecycle: an overall deadline, graceful Ctrl-C cancellation, partial reports and reliable cleanup of active requests.
- Bounded concurrency for independent scenarios: preserve ordered results and respect dependencies. Verify with tests that caps and cancellation work.
- Report persistence and comparison: versioned result files, readable summaries and explicit treatment of missing or incompatible results.
- Configuration and packaging: stricter duplicate-key handling, validation errors tied to fields, portable release binaries and integration coverage.

Keep fixtures synthetic. Extending beyond loopback requires a deliberate scope decision. This roadmap is not an hours-worked record, a promised delivery schedule, or proof of programme eligibility.
