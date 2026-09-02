# Planner contract

The `.gooo` sources are the semantic source of truth. `gooo-gen` projects them
to `internal/generated/model.gen.go`; CI regenerates that projection and fails
if the committed projection is stale.

The planner consumes a pinned `previous` snapshot and a `candidate` snapshot.
It computes:

- changed semantic nodes by deterministic node fingerprints;
- transitive dependents from candidate dependency edges, with visited-set cycle
  termination;
- selected checks for impacted or unresolved nodes;
- reusable locks only when status, provenance, authority, toolchain, evaluator,
  and impact scope all match;
- invalidated locks for changes, stale scope, changed authority, conflicts,
  non-closed status, missing provenance, or removed nodes;
- an explicit unknown frontier with stage, step, reason, unknown class, next
  operation, and blockers.

Status precedence is `REFUTED > UNKNOWN > CLOSED`. It is never reduced to a
scalar score. Every report contains per-indicator vectors and exact counts.
`executed_checks` is always zero because this product selects work but does not
run it. Runtime repository writes are zero, and output is written to stdout for
the caller to capture.

Performance remains `UNKNOWN` until an exact same-scenario, toolchain, and
evaluator before/after pair is provided. Wall time may be recorded only for the
slicer when `plan --measure` is requested.
