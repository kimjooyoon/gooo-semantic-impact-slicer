# Gooo Semantic Impact Slicer

An independent Gooo language mechanism for planning semantic impact from a
pinned graph and evidence-lock set to a candidate graph.

The planner selects checks, reusable evidence locks, stale locks, and an
unknown frontier. It never executes checks and never treats a digest or cache
hit as semantic closure.

All repository validation is performed by GitHub Actions. The root README is
intentionally excluded from the generated inventory.
