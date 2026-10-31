How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 66125 prompt (38464 cached), 1207 completion, $0.00647, 6 rounds, 7 tools

## answer











The mechanism is called the "repeat guard" and lives in `internal/agent/turn.go` (the `run` method, ~line 285) plus the state fields on the `turn` value (`loop.type.go:200-205`). How it works, layer by layer:

**1. Fingerprint every call** — `callKey` (`loop.go:204`) builds `name + canonical-args` (JSON marshaling sorts map keys, so equivalent args give the same key).

**2. Refuse re-running a failed call** — after each call, `run` records failures in `t.failed[key] = msg` (where `errorOf` extracts the `error` field from the JSON result). If the model re-issues the *exact same* call that failed earlier in the turn, `run` refuses to execute it and instead feeds back a JSON error: *"you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you."* Crucially, any successful call in between clears the whole `t.failed` map — the rationale in the comment is that a success may have fixed what the failed call depended on, so retrying is legitimate again.

**3. Detect same-output loops for run-type tools** — separately, successful program runs (`runTool`) remember their output in `t.ran[key]`. If the model re-runs a call that produced *byte-identical* output:
- back-to-back (nothing in between, `t.lastCall == key`): refused as a loop — "running it again won't change anything";
- after intervening work (an edit/rebuild): it's allowed, but the result is annotated with a note that the output didn't change, in case the change didn't take effect.

**4. Escalate to stopping the turn** — every refusal bumps `t.blocked` (metrics: `guard.blocked`). Once `t.blocked >= maxBlocked` (= 2, `turn.go:40`), the turn is declared stopping: any remaining calls in the same reply get a stub result `"not run: this turn is stopping"` (every tool call needs a result to keep the conversation valid), and the round loop breaks.

**5. Wind down with a tool-less summary** — the turn appends `repeatNote` to history and makes one final call with **no tools** (`defs = nil`): *"You kept repeating a call that had already failed, so tools are off for the rest of this turn…"* — telling the user what was attempted, what failed, and asking how to continue, instead of returning an error.

There's also a related but separate safety net, the `AidStuck` "stepping back" aid (`aids.go`): `stall.observe` tracks error/failing-build/non-zero-exit results as misses and, when several accumulate, injects a `stuckNote` asking the model to state what it knows, list hypotheses, and reconsider its approach before trying again.

So the defense is: soft refusal with guidance first (and it's resettable by progress), then a hard stop with tools disabled and a forced plain-text summary, with per-call keys making "the same call" precise rather than name-based.

## calls
project_manifest map[]
semantic_search map[query:agent loop repeated identical tool call failure dedup]
search_code map[path:internal/agent pattern:repeat]
outline_file map[path:internal/agent/turn.go]
read_file map[path:internal/agent/turn.go]
search_code map[path:internal/agent pattern:func (callKey|errorOf|runTool)]
search_code map[path:internal/agent/aids.go pattern:observe|stuckNote]

## tree after

