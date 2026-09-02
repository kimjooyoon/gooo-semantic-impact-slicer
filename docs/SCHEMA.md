# JSON shape

`gooo-slicer plan --input request.json` emits a deterministic report containing
the exact metrics `semantic_nodes`, `changed_nodes`, `selected_checks`,
`reusable_locks`, `invalidated_locks`, `unresolved_nodes`, `executed_checks`,
and `generated_artifacts`.

Inputs use node IDs, semantic digests, dependency IDs, authority and
provenance references, candidate checks, and previous evidence locks. A missing
dependency or authority never means unaffected; it widens the unknown frontier.
Conflicting locks are retained as invalidated evidence rather than silently
choosing one.
