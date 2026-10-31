How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 63467 prompt (57088 cached), 1526 completion, $0.00334, 6 rounds, 8 tools

## answer


The loop has a "repeat guard" around tool execution in `internal/agent/turn.go`, and it escalates in two stages:

**1. Refuse the repeated call** — `turn.run()` (turn.go:255-336):
- Every call is keyed by tool name + JSON-marshalled args (map keys sort, so equal args give equal keys — `callKey`, loop.go:204-207).
- When a call fails, its error is stored in `t.failed[key]`. If the model issues that **exact** call again, it's never executed; the model just gets a JSON error: *"you already made this exact call this turn and it failed ... Don't repeat it: change the arguments, use another tool, or stop and tell the user"* (turn.go:271-276).
- Any successful call does `clear(t.failed)` (turn.go:324) — so a previously failed call is allowed to run again after something in between may have fixed what it depended on (that's the point of the comment at turn.go:257-260: a success in between means it runs).
- Bonus guard for program runs: a run giving **identical output** back-to-back is refused as a loop ("running it again won't change anything"); the same re-run with other calls in between runs, but with a "same output as last time" note (turn.go:309-322).

**2. Stop the turn** — each refusal bumps `t.blocked`; at `maxBlocked = 2` (turn.go:34) the round loop breaks (turn.go:121-124) and one final call is made **with no tools** (`defs` passed as `nil`), prompted with `repeatNote`: *"tools are off for the rest of this turn ... Tell the user what you were trying to do, what failed ... then ask how to continue"* (turn.go:36-38, 127-140). Any other calls in the same reply still get a stub result ("not run: this turn is stopping") so every tool call has a response in history (turn.go:280-286).

So it never hard-fails on a stuck model: it refuses the repeat, warns, and after two refusals winds the turn down into a plain-text report to the user. Separately, the `AidStuck` aid can inject a `stuckNote` to make the model "step back" after repeated failures (turn.go:114-119), and the system prompt itself tells the model to stop after two failed attempts at the same thing (prompt.go:50).

## calls
know_find map[query:agent loop repeated failed tool call dedupe retry]
search_code map[file_type:go pattern:repeat]
read_file map[end_line:160 path:internal/agent/turn.go start_line:60]
read_file map[end_line:310 path:internal/agent/turn.go start_line:245]
read_file map[end_line:60 path:internal/agent/turn.go start_line:1]
read_file map[end_line:336 path:internal/agent/turn.go start_line:310]
search_code map[file_type:go path:internal/agent pattern:func callKey|failed\s+map|ran\s+map]
know_write map[claim:The agent loop's repeat guard: turn.run() keys each call by name+sorted-args JSON (callKey); a failed call is recorded in t.failed…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473478106821302"
author a2 armin session "bench-1793473478106821302"

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

