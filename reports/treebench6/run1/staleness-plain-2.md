When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 64201 prompt (54528 cached), 708 completion, $0.00861, 6 rounds, 6 tools

## answer
Dependent nodes get flagged **suspect**, and the function that does it is `Graph.MarkSuspect` — `internal/know/stale.go:128`.

How it works:

- It does a **breadth-first walk backwards** over the reverse `requires` and `derived-from` edges from the stale nodes (`g.In(id, ...)` at stale.go:144–145), so it reaches every node that depends on a stale one, directly or transitively.
- Each live node it finds that isn't already `FlagStale` or `FlagSuspect` gets `FlagSuspect` set on it (stale.go:159–166), and it returns how many it marked.

The caller is `refreshKnow` in `internal/think/stale.go`: after `CheckFile` (`internal/know/stale.go:72`) detects a changed file and marks the direct evidence nodes `FlagStale` (stale.go:114), it passes those newly-stale IDs to `MarkSuspect` (think/stale.go:83), then runs `FlagCycles`. Net effect: the stale node stays flagged stale, and everything resting on it becomes suspect so it can be re-verified.

## calls
semantic_search map[query:stale knowledge tree node invalidate dependent children]
search_code map[pattern:stale]
search_code map[path:internal/know pattern:[Ss]tale|[Ii]nvalid]
read_file map[end_line:185 path:internal/know/stale.go start_line:125]
find_callers map[name:MarkSuspect]
read_file map[end_line:100 path:internal/think/stale.go start_line:55]

## tree after

