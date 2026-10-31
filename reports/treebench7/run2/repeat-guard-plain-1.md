How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 32512 prompt (21280 cached), 924 completion, $0.00208, 3 rounds, 4 tools

## answer


It's a repeat guard built into the turn's tool runner, in `internal/agent/turn.go`. The mechanism:

**1. Identity per call** — every call is keyed by tool name + JSON-marshalled args (`callKey`, `internal/agent/loop.go:204`), so "repeat" means the exact same tool with the exact same arguments.

**2. Failed calls are remembered** — when a call's result contains an error, `turn.run` stores the error under that key in `t.failed` (`turn.go:274-313`). If the model makes that same call again *with nothing successful in between*, it's refused without executing and the result is:

> "you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you." (`turn.go:280-286`)

A success in between clears `t.failed`, so retrying after a legitimate fix is allowed.

**3. Run-output loops** — separately, a program run giving byte-identical output back-to-back (nothing in between) is refused as a loop; re-running the same thing after edits is allowed but gets a "same output as last time" note (`turn.go:317-334`).

**4. Escalation to a hard stop** — each refusal bumps `t.blocked` (`loop.type.go:205`). Once `blocked >= maxBlocked` (2, `turn.go:36`):
- the remaining calls in that reply still get a tool result ("not run: this turn is stopping") so every call ID is answered (`turn.go:290-295`),
- the round loop breaks with `stopNote = repeatNote` (`turn.go:128-130`),
- the turn does one final model call **with no tools**, telling the model: "tools are off for the rest of this turn … tell the user what you were trying to do, what failed, and ask how to continue" (`turn.go:38-41`, `turn.go:136-152`). So the user gets a report instead of an infinite loop.

There are two supporting layers too:
- **stuck aid**: after three consecutive misses (tool errors, failing tests/builds), the model gets a "step back" note asking what it knows for sure before it tries again (`internal/agent` aids, `turn.go:113-124`).
- **system prompt**: the model is instructed up front not to retry failed tools, so the guard is a backstop for when it ignores that (see `docs/components/agent.md:25-27`).

There's a dedicated test for the hard-stop path: `TestTurnStopsAModelThatKeepsRetrying` (`internal/agent/loop_test.go:508`) — it asserts the model gets called exactly `1 + 1 + maxBlocked` times (first call, one refusal, then stop) and that the turn ends in a tool-less answer.

## calls
search_code map[pattern:repeat]
search_code map[pattern:failed.*tool|tool.*fail]
read_file map[path:internal/agent/turn.go]
search_code map[path:internal/agent pattern:callKey|maxBlocked|blocked]

## tree after

