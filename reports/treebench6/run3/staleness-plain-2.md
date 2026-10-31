When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 60576 prompt (50944 cached), 800 completion, $0.00851, 6 rounds, 8 tools

## answer
**`MarkSuspect` in `internal/know/stale.go:128` does it.**

When a node's file evidence changes, `CheckFile` marks that node `FlagStale` (stale.go:114), and its transitive dependents get `FlagSuspect` via `MarkSuspect` (stale.go:128):

- It does a breadth-first walk over **reverse** `requires` and `derived-from` edges — everything that rests on the stale node, directly or transitively.
- It only flags **live** nodes that aren't already `FlagStale` or `FlagSuspect`, sets `FlagSuspect` on each, and returns how many it marked.

The wiring happens in `refreshKnow` (`internal/think/stale.go:83`): it re-hashes changed files with `CheckFile`, collects the newly-stale nodes, and feeds them into `MarkSuspect` (followed by a `FlagCycles` pass).

Downstream effect: suspect/stale nodes never claim to be "current" in the node lines models read — `internal/know/line.go:72` only prints `current` when the node has no `FlagStale|FlagSuspect` set (both flags are rendered in the line too, line.go:81-82).

## calls
find_callers map[name:InvalidateSubtree]
find_definition map[name:InvalidateSubtree]
search_code map[pattern:func.*Stale]
search_code map[path:internal/know pattern:MarkSuspect|FlagStale|stale]
search_code map[path:internal/know pattern:dependen]
read_file map[end_line:185 path:internal/know/stale.go start_line:125]
find_callers map[name:MarkSuspect]
read_file map[end_line:100 path:internal/think/stale.go start_line:40]

## tree after

