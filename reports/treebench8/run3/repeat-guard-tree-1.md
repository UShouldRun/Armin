How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 89502 prompt (79040 cached), 1605 completion, $0.00898, 7 rounds, 9 tools

## answer















There are three layers, all in `internal/agent/turn.go` plus the "stuck" aid in `internal/agent/aids.go`:

**1. The repeat guard (per call)** — `t.run`, turn.go:285-377. It keys every call by name+arguments (`callKey`) and keeps per-turn memory on the turn value: `failed` (call → last error), `ran` (call → last successful output), `lastCall`, `blocked` (loop.type.go:200-205).

- **Exact repeat of a failed call** (turn.go:288-300): if the same call failed earlier in the turn with no *successful* call in between, it isn't executed at all. The model gets an error result: *"you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you."* Any successful call clears the whole `failed` map (turn.go:351), since a success may have fixed what the failed call depended on. This is covered by `TestTurnBlocksARepeatedFailedCall` (loop_test.go:339).
- **Same-output run loop** (turn.go:320-340): a `run_command`/`run_program`/`run_tests`-style call whose output is byte-identical to the previous run. Back-to-back → refused as a loop. With other calls in between it runs, but with a `note`: "same output as the last time you ran this… if you changed something in between, it didn't change this result."

**2. Stall "step back" aid** — `stall.observe`, aids.go:153-173. Counts misses in a row (error result, failing tests/build, non-zero exit); a passing check resets it. Every 3 misses (up to 2 nudges per turn) `call` returns true (turn.go:271) and the turn loop appends `stuckNote` as a user message (turn.go:125-129): list what you know, two or three hypotheses, test the cheapest, or stop and tell the user.

**3. Hard stop** — `maxBlocked = 2` (turn.go:40). After two refusals, the loop breaks with `repeatNote` (turn.go:132-134): tools are off for the rest of the turn and the model must report what it was trying, what failed, and ask how to continue. Any further calls in that reply still get a result (`"not run: this turn is stopping"`, turn.go:302-306) because every tool call needs one, but nothing executes.

Each refusal also bumps `guard.blocked` metrics (turn.go:290). So the escalation is: per-call refusal with coaching → step-back nudge after repeated misses → cut tools and force a final summary.


</invoke>



Saved a node on this to the knowledge tree ([n3]) so it won't need rediscovering.

## calls
search_code map[pattern:repeated|duplicate.*tool|same.*(error|fail).*(again|repeat)]
semantic_search map[query:handling repeated failing tool call in agent loop]
search_code map[path:internal/agent pattern:refus|Guard|guard]
read_file map[end_line:360 path:internal/agent/turn.go start_line:240]
read_file map[end_line:60 path:internal/agent/turn.go start_line:30]
search_code map[path:internal/agent pattern:func.*stall.*observe|stepBack|stuckNote]
search_code map[path:internal/agent/turn.go pattern:repeatNote|maxBlocked]
read_file map[end_line:180 path:internal/agent/aids.go start_line:120]
know_write map[claim:The agent loop's repeat guard (turn.run) refuses to re-execute a tool call identical to one that failed earlier in the turn unless…

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

