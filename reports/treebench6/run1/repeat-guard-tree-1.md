How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right false, 59160 prompt (43456 cached), 1294 completion, $0.00316, 5 rounds, 7 tools

## answer


Recorded as [n3] in the knowledge tree (`agent/loop/repeat-guard`) so I don't have to re-derive it next time.

## calls
know_find map[query:agent loop repeated failing tool call retry]
search_code map[pattern:repeated|repeat|duplicate.*tool|consecutive.*fail]
read_file map[path:docs/components/agent.md]
search_code map[path:internal/agent pattern:repeat|block]
read_file map[end_line:70 path:internal/agent/turn.go start_line:1]
read_file map[end_line:345 path:internal/agent/turn.go start_line:253]
know_write map[claim:turn.run (internal/agent/turn.go:255) guards against a model repeating a failed tool call: an exact retry of a call that failed th…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473424974661027"
author a2 armin session "bench-1793473424974661027"

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

