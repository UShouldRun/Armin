How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 100173 prompt (75072 cached), 1388 completion, $0.00913, 7 rounds, 9 tools

## answer

















The agent loop (in `internal/agent/turn.go`) has a three-layer defense against a model that hammers a failing call:

**1. Exact-repeat guard in `t.run` (turn.go:280)**

Every call gets a key — tool name + canonicalized JSON arguments (`callKey`, loop.go:204). Per turn the `turn` value keeps a `failed` map (loop.type.go:200):

- When a call errors (`errorOf` finds an `"error"` key in the result), its error message is stored under that key.
- If the *exact same* call is made again before anything succeeded, it **isn't executed at all** — the model gets back `{"error": "you already made this exact call this turn and it failed: <prev error>. Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you."}`
- Any successful call clears the whole `failed` map, so retrying a previously-failed call after something working in between is allowed (the success may have fixed its dependency).

**2. Run-loop detection (turn.go:335)**

For "run" tools (build, test, program), a successful result is compared against the last successful output of the identical call:

- Back-to-back with nothing in between and byte-identical output → refused as a loop: *"you just ran this exact call and it gave exactly the same result... running it again won't change anything"*.
- Same output but after other calls (e.g. an edit or rebuild) → allowed, but annotated: *"same output as the last time you ran this... if you changed something in between, it didn't change this result"* — which catches the "my edit didn't take effect" case.

**3. Wind-down after 2 blocks (`maxBlocked = 2`, turn.go:40)**

Each refusal increments `t.blocked`. Once it hits 2, the loop breaks the turn entirely; any remaining tool calls in the same reply get `"not run: this turn is stopping"` so every call still has a result. Then one **final tool-less model call** goes out with `repeatNote`: *"You kept repeating a call that had already failed, so tools are off for the rest of this turn... Tell the user what you were trying to do, what failed... and ask how to continue."* So the user gets a plain-language report, not a hang and not a raw error.

There's a complementary, softer mechanism: the **stuck aid** (`AidStuck`, aids.go:21). `t.stall.observe` counts consecutive "misses" (errors, failing tests/build, non-zero exit) per turn; after `stallLimit = 3` in a row it injects a `stuckNote` asking the model to step back — state what it knows, list hypotheses, pick the cheapest one to test — at most `maxNudges = 2` times per turn. That's the "step back after repeated failures" behavior you can toggle with `/aid`, while the guard in `run` is unconditional.

Both are visible in the telemetry: `guard.blocked` metric on each refusal, and the treebench logs label a run variant "repeat-guard".

## calls
project_manifest map[]
search_code map[pattern:repeated|repeat]
search_code map[file_type:go path:internal/agent pattern:repeat|stuck|duplicate]
list_directory map[path:internal/agent]
read_file map[end_line:200 path:internal/agent/turn.go start_line:1]
read_file map[end_line:377 path:internal/agent/turn.go start_line:240]
read_file map[end_line:200 path:internal/agent/aids.go start_line:100]
search_code map[path:internal/agent/turn.go pattern:func callKey|func errorOf|func runTool]
search_code map[file_type:go pattern:func callKey|func errorOf|func runTool]

## tree after

