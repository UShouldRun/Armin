When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 50466 prompt (41472 cached), 753 completion, $0.00741, 5 rounds, 6 tools

## answer
Dependents don't go stale themselves — they're flagged **suspect**. `(*Graph).MarkSuspect` in `internal/know/stale.go:127` does it:

- **BFS over reverse edges** — starting from the stale nodes, it walks `g.In(id)` and follows only `EdgeRequires` and `EdgeDerivedFrom` edges backwards, i.e. everything that *requires* the stale node or was *derived from* it, transitively.
- It marks each live node with `FlagSuspect` (`0b00000010`, "something it rests on did" — `internal/know/graph.type.go:49`), skipping nodes already `FlagStale`/`FlagSuspect` and using a bitset to avoid re-visits; it returns how many it marked.
- Suspect is a softer state than stale: the node's own evidence hasn't changed, only its foundation has. It stays valid-looking until re-verified (an `edit` with `current` clears both flags — `internal/agent/tools_know.go:804`).

The caller is `refreshKnow` in `internal/think/stale.go:93`, which re-hashes stamp-changed files via `CheckFile`, collects the newly stale nodes, then calls `g.MarkSuspect(stale)` and reports the count as the `stale.suspect` metric.

## calls
semantic_search map[query:knowledge tree node stale depends invalidate]
search_code map[pattern:stale]
search_code map[path:internal pattern:Suspect|suspect]
search_code map[path:internal pattern:func.*[Ss]tale]
read_file map[end_line:160 path:internal/know/stale.go start_line:118]
read_file map[end_line:185 path:internal/know/stale.go start_line:160]

## tree after

