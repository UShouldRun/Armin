I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 77512 prompt (66816 cached), 1213 completion, $0.01119, 7 rounds, 8 tools

## answer
One place in code: the constant that everything else is keyed on.

**`internal/agent/turn.go:34`**
```go
const maxBlocked = 2
```
change to `3`.

Why that's all you need:
- Both enforcement points use the constant, not a literal: the per-call cutoff at `turn.go:281` (`if t.blocked >= maxBlocked` → remaining calls get "not run") and the loop break at `turn.go:121` (attaches `repeatNote` and stops).
- The counter `t.blocked` is incremented on each refusal at `turn.go:271` and `turn.go:309`; it's declared in `internal/agent/loop.type.go:197` — no change there.
- The test `TestTurnStopsAModelThatKeepsRetrying` (`internal/agent/loop_test.go:548`) compares against `maxBlocked` itself, so it keeps passing without edits.

One prose fix to stay honest: `docs/components/agent.md:30` says "after two refusals the turn winds down" — update that to three.

Want me to make the change?

## calls
know_find map[query:refused repeats limit tool loop]
search_code map[pattern:refus]
know_find map[ids:[n3 n5] query:]
search_code map[path:internal/agent pattern:refus|refusal]
read_file map[end_line:45 path:internal/agent/turn.go start_line:28]
read_file map[end_line:340 path:internal/agent/turn.go start_line:266]
read_file map[end_line:560 path:internal/agent/loop_test.go start_line:500]
search_code map[path:internal pattern:maxBlocked]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473478106821302"
author a2 armin session "bench-1793473478106821302"
author a3 armin session "bench-1793473502839732039"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/loop topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/loop/repeat-guard concept checked a1 born 2026-10-31 seen 2026-10-31
  claim "The agent loop's repeat guard: turn.run() keys each call by name+sorted-args JSON (callKey); a failed call is recorded in t.failed and re-issuing it is refused with an error telling the model to change args (clears when a success happens). Program runs giving identical output back-to-back are also refused (t.ran). Each refusal increments t.blocked; at maxBlocked=2 the turn breaks with repeatNote and one tool-less closing call. The stuck aid can additionally inject stuckNote to step the model back."
  evidence file "internal/agent/turn.go:36-38" 3c73fecb
  evidence file "internal/agent/turn.go:121-124" e4a83c49
  evidence file "internal/agent/turn.go:255-336" 0eca247d
  evidence file "internal/agent/loop.go:204-207" 4980d417
  evidence file "internal/agent/loop.type.go:192-197" 95512c47

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin's-agent file checked a2 born 2026-10-31 seen 2026-10-31
  claim "turn.go:255-336 loop.go:204-207 turn.go:271-276 turn.go:324 turn.go:257-260 turn.go:309-322 turn.go:34 turn.go:121-124 turn.go:36-38 turn.go:280-286 turn.go:114-119 prompt.go:50"
  evidence file "internal/agent/turn.go:1-60" bd3cdd3d
  evidence file "internal/agent/turn.go:245-310" 964932e4
  evidence file "internal/agent/turn.go:310-336" 9c15fdaa
  evidence file "internal/agent/turn.go:60-160" 87de13b2

n6 sessions/i-want-the-loop file checked a3 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/turn.go:34 turn.go:281 turn.go:121 turn.go:271 turn.go:309 internal/agent/loop.type.go:197 internal/agent/loop_test.go:548 docs/components/agent.md:30"
  evidence file "internal/agent/loop_test.go:500-560" 3eac0fb1
  evidence file "internal/agent/turn.go:28-45" 1a239ba9

