How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 68053 prompt (57600 cached), 1408 completion, $0.00336, 6 rounds, 9 tools

## answer


Let me read the full guard implementation:




Armin handles this with a **repeat guard** in `internal/agent/turn.go`, layered in three escalating steps:

**1. Per-call refusal (the guard proper)** — `turn.run()` (turn.go:273):
- Every call gets a key from name + arguments (`callKey`, loop.go:204, using `json.Marshal`'s sorted keys so equal args give equal keys).
- If that exact call **failed earlier this turn and nothing succeeded in between**, the repeat is *not executed*. The model gets a synthetic tool result: `"you already made this exact call this turn and it failed: <original error>. Don't repeat it: change the arguments, use another tool, or stop and tell the user..."` (turn.go:280). So it's cheap per-call feedback, not a run interruption.
- Any successful call clears the whole `failed` map (turn.go:343) — a success may have fixed what the failed call depended on, so retries after that are allowed.
- It also catches output loops: a *run* tool (command/build/test) giving **byte-identical output** right after the previous identical run is refused as a loop; if there were other calls in between, it runs but gets a `"same output as last time"` note.

**2. Step-back nudge (soft)** — a separate "stuck" aid (`stall.observe`, aids.go:149) counts consecutive misses (errors, failing tests/build, non-zero exits). After 3 misses with no passing check in between, the loop injects `stuckNote` as a user message: "Step back: your last attempts failed without progress. (1) say what you know for sure, (2) list two or three hypotheses, (3) pick the cheapest one to test..." (aids.go:137). Capped at 2 nudges per turn (`maxNudges`).

**3. Turn shutdown (hard)** — each refusal bumps `t.blocked`; after `maxBlocked = 2` (turn.go:35) refusals, the turn stops calling tools: the remaining calls in the current reply still get a placeholder result (`"not run: this turn is stopping"`, since every tool call needs a response), then the loop breaks and makes **one final model call with no tools**, using `repeatNote`: "You kept repeating a call that had already failed, so tools are off for the rest of this turn. Tell the user what you were trying to do, what failed... then ask how to continue" (turn.go:36-41, 129-143). The user gets a report instead of an error.

All refusals are counted in metrics (`guard.blocked`) and logged as events. The system prompt also tells the model up front: "After two failed attempts at the same thing, stop and tell the user" (think/prompt.go:50) — but the guard is what actually enforces it, since as the code comment puts it, "a model that keeps retrying a blocked call won't stop on its own."

## calls
search_code map[pattern:repeated|repeatedly|duplicate.*tool|tool.*repeat]
semantic_search map[query:agent loop detecting repeated identical failing tool calls]
search_code map[file_type:go path:internal/agent pattern:guard|refus|stuckNote|stepBack]
outline_file map[path:internal/agent/loop.go]
read_file map[end_line:130 path:internal/agent/turn.go start_line:1]
read_file map[end_line:340 path:internal/agent/turn.go start_line:230]
search_code map[file_type:go path:internal/agent pattern:stuckNote|func \(t \*turn\) observe|func callKey|func errorOf]
read_file map[end_line:160 path:internal/agent/turn.go start_line:127]
read_file map[end_line:175 path:internal/agent/aids.go start_line:100]

## tree after

