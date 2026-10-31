I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 55681 prompt (48192 cached), 585 completion, $0.00464, 5 rounds, 5 tools

## answer



</invoke>










One line: **`internal/agent/turn.go:40`**

```go
const maxBlocked = 2
```

Change to:

```go
const maxBlocked = 3
```

`maxBlocked` is the single limit the repeat guard uses: the loop checks `t.blocked >= maxBlocked` before stopping (turn.go:132) and `run()` checks it to stop executing calls (turn.go:302). The test `TestTurnStopsAModelThatKeepsRetrying` (loop_test.go:548) references the constant rather than a literal, so it keeps passing without edits. I'd suggest updating the comment at turn.go:37-39 too if it states a count, but as written it's count-agnostic — the constant change alone is enough.

## calls
search_code map[pattern:repeat]
search_code map[file_type:go path:internal pattern:refus]
find_files map[path:internal pattern:turn*.go]
search_code map[path:internal/agent pattern:maxBlocked]
read_file map[end_line:46 path:internal/agent/turn.go start_line:35]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793476031593358528"
author a2 armin session "bench-1793476031593358528"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/loop topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/loop/repeat-guard concept inferred a1 born 2026-10-31 seen 2026-10-31
  claim "The agent loop's repeat guard (turn.run) refuses to re-execute a tool call identical to one that failed earlier in the turn unless a successful call happened in between (any success clears the failed map). Repeated refusals (maxBlocked=2) end the turn with repeatNote, cutting all tools and forcing a summary; the stall aid nudges \"step back\" after 3 consecutive misses."

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin's-agent file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/aids.go:120-180 internal/agent/turn.go:240-360 internal/agent/turn.go:30-60"
  evidence file "internal/agent/aids.go:120-180" 07bb8594
  evidence file "internal/agent/turn.go:240-360" 394dc75f
  evidence file "internal/agent/turn.go:30-60" 1228c922

