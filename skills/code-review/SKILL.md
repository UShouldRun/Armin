---
name: code-review
description: Review a diff or files for bugs first, then performance and style — loading the matching language style skill — and report findings ranked by severity with file:line and a concrete fix. Load when asked to review code.
---
## Steps
1. Get the change: `working_tree_diff` / `git_diff` against the base,
   or the files named. Read enough surrounding code to understand
   each change — callers (`find_callers`, `find_references`) and the
   types involved.
2. Load the style skill for each language touched (go-style,
   c-style, cpp-style, js-style, react-style, sql-style).
3. Look, in this order:
   - **Correctness**: logic errors, off-by-one, nil/NULL, error paths
     that leak or swallow, concurrency (races, lock order, goroutines
     that never exit), resource lifetimes, integer overflow and sign,
     undefined behavior in C/C++.
   - **Security**: injection (shell, SQL), path traversal, secrets in
     code or logs, unchecked input sizes.
   - **Performance**: allocations in hot paths, O(n²) over growing
     data, repeated work across a session, wasteful queries (names
     instead of ids, per-row work before limit).
   - **Tests**: is the new behavior covered; would the tests catch a
     regression?
   - **Style**: per the style skill. Batch these; don't let them bury
     real bugs.
4. Build and run the tests (`build_check`, `run_tests`) — a review
   that didn't compile the code says so.

## Report
Ranked most severe first. For each finding:
- `file:line` — one sentence stating the defect
- the concrete scenario where it goes wrong (inputs → wrong result)
- the fix, as a short code suggestion when it helps

Mark anything uncertain as such instead of overstating it. If
nothing is wrong, say so plainly. Don't edit files unless asked.
