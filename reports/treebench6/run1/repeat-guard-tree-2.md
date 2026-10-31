I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 45563 prompt (40192 cached), 635 completion, $0.00628, 6 rounds, 5 tools

## answer
One line: `internal/agent/turn.go:34`

```go
const maxBlocked = 2
```
change to `3`.

How it works: `t.blocked` counts refused repeats (turn.go:270, turn.go:304); at `t.blocked >= maxBlocked` the guard stops executing the rest of the reply's calls (turn.go:281) and the loop breaks out to wind down the turn tool-less (turn.go:121). Bumping the constant to 3 means refusals 1–2 get an error back, refusal 3 triggers the wind-down — exactly the limit you're after.

No test changes needed: `loop_test.go:548-549` computes the expected call count as `1 + 1 + maxBlocked`, so it adapts to the constant automatically. The only doc tweak worth making is the comment on turn.go:31-33, which currently says "how many repeats the guard refuses" — still accurate, so optional.

Want me to make the edit and run the tests?

## calls
know_find map[query:refused repeats tools off limit loop]
read_file map[end_lineLines:336 path:internal/agent/turn.go start_line:255]
read_file map[end_line:336 path:internal/agent/turn.go start_line:255]
search_code map[path:internal/agent pattern:maxBlocked]
read_file map[end_line:130 path:internal/agent/turn.go start_line:100]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473424974661027"
author a2 armin session "bench-1793473424974661027"
author a3 armin session "bench-1793473472511342583"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/loop topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/loop/repeat-guard concept checked a1 born 2026-10-31 seen 2026-10-31
  claim "turn.run (internal/agent/turn.go:255) guards against a model repeating a failed tool call: an exact retry of a call that failed this turn (with no success in between, which clears t.failed) is refused with an error result; a run-tool giving byte-identical output back-to-back is refused as a loop (identical output with calls in between runs, with a \"nothing changed\" note). Each refusal bumps t.blocked; at maxBlocked=2 (turn.go:34) the turn stops, remaining calls get \"not run\", and one final tool-less call with repeatNote (turn.go:36) lets the model report to the user. Backed by prompt.go:50's \"don't repeat a failing tool\" rule and the stuck aid (aids.go) after three consecutive failures."
  evidence file "internal/agent/turn.go:255-336" 0eca247d
  evidence file "internal/agent/turn.go:31-38" 7390e0e2
  evidence file "internal/think/prompt.go:50" 9c1fed98

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin's-agent file checked a2 born 2026-10-31 seen 2026-10-31
  claim "docs/components/agent.md internal/agent/turn.go:1-70 internal/agent/turn.go:253-345"
  evidence session "docs/components/agent.md" 00000000
  evidence file "internal/agent/turn.go:1-70" 09ab6a79

n6 sessions/i-want-the-loop file checked a3 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/turn.go:34 turn.go:270 turn.go:304 turn.go:281 turn.go:121 loop_test.go:548-549 turn.go:31-33"
  evidence file "internal/agent/turn.go:100-130" f7f554ae
  evidence file "internal/agent/turn.go:255" 5501d8d1
  evidence file "internal/agent/turn.go:255-336" 0eca247d

