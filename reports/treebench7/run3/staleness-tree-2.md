When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 17573 prompt (12864 cached), 672 completion, $0.00213, 2 rounds, 2 tools

## answer





It's `MarkSuspect` in `internal/know/stale.go:110` — it does a breadth-first walk over the **reverse** `requires` and `derived-from` edges (i.e., everything that depends on the stale node, transitively) and flags those nodes `FlagSuspect`, not `FlagStale`. Key details:

- **Only dependents get marked**, via `g.In(id, ...)` following inbound `EdgeRequires` / `EdgeDerivedFrom` edges (stale.go:127-131). Nodes already stale or suspect are visited but not re-marked (stale.go:135-138), so the BFS terminates and returns how many it newly flagged.
- **Stale ≠ suspect is deliberate**: the node whose cited code actually changed is `FlagStale` (from `CheckFile`); things resting on it are `FlagSuspect` — they *might* be wrong and deserve re-checking, but nothing has been verified against them yet. That's why the injection to you shows "trust it" for `current` nodes and "re-check" for `STALE`/`SUSPECT` (the n2 node above shows the format).

The full pipeline runs at session start in `refreshKnow` (`internal/think/stale.go:47`):
1. Stamp-compare each cited file (skip unchanged ones);
2. `CheckFile` re-hashes changed files — lines unchanged → fine, moved → re-anchored, else the node is marked stale (stale.go:72-130 in know);
3. `MarkSuspect(stale)` propagates suspicion to all dependents (think/stale.go:106);
4. `FlagCycles` records open requires-loops as question nodes.

One asymmetry to note: an `explains` edge doesn't propagate — only `requires`/`derived-from` do, so a node merely *describing* a stale node (not built on it) is left untouched.

## calls
read_file map[path:internal/know/stale.go]
read_file map[path:internal/think/stale.go]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474539444632263"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-the-knowledge file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/think/think.go:273 internal/think/stale.go:28 internal/know/stale.go:23 internal/think/stale.go:116 internal/think/stale.go:133-181 internal/know/stale.go:52 internal/know/hash.go:60 internal/know/stale.go:101"
  evidence file "internal/know/hash.go:55-90" f428fb96
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000
  evidence file "internal/think/think.go:255-290" 25aa569f

