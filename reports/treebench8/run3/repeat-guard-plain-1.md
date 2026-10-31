How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 94265 prompt (81088 cached), 1408 completion, $0.00881, 7 rounds, 11 tools

## answer



















There's a "repeat guard" around every tool call in the loop: `turn.run` (internal/agent/turn.go:286). The flow:

**Per call (`run`)**
1. Each call gets a stable key — tool name + canonical JSON args (`callKey`, internal/agent/loop.go:204).
2. **Repeat of a failed call:** if that key is in `t.failed` (i.e. it failed earlier this turn with no success of the same call in between), the call is *not executed*. It gets a JSON error back: *"you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop…"*. The refusal bumps `t.blocked` and emits a `guard.blocked` metric (turn.go:288-298). A successful run of the same call clears `failed`, so retrying after a fix is allowed.
3. **Same-output loop (run tools only):** if a program returns byte-identical output as the immediately previous identical run, it's refused as a loop; after unrelated calls in between it runs but with a "same output, your change didn't take effect" note (turn.go:329-346).

**Turn level (`Turn` loop)**
- `maxBlocked = 2` (turn.go:40): once the guard has refused 2 repeats, the loop breaks. Any remaining tool calls in that reply still get a result — `{"error":"not run: this turn is stopping"}` — because every call needs one, but they don't execute (turn.go:302-307).
- The turn then makes one final model call **with no tool definitions**, preceded by `repeatNote` (turn.go:42): "You kept repeating a call that had already failed, so tools are off for the rest of this turn… Tell the user what you were trying to do, what failed… then ask how to continue." So the model is forced to produce a plain-text report instead of looping.

This is distinct from the "stuck" aid (`aids.go`), which injects a "step back" note on repeated failures, and from the round limit, which uses a similar wind-down with `roundLimitNote`.

Covered by `TestTurnBlocksARepeatedFailedCall` and `TestTurnStopsAModelThatKeepsRetrying` (internal/agent/loop_test.go:339, 508) — the latter asserts the turn ends after exactly `1 + 1 + maxBlocked` model invocations.

## calls
search_code map[pattern:repeated|duplicate|repeat.*tool|same.*tool]
semantic_search map[count:8 query:agent loop retry limit for failing or repeated tool calls]
search_code map[file_type:go pattern:repeat|guard]
find_files map[path:internal/agent pattern:*guard*]
search_code map[file_type:go path:internal/agent pattern:maxBlocked|repeatNote|guard|Blocked]
find_files map[path:internal/agent pattern:*.go]
read_file map[end_line:160 path:internal/agent/turn.go start_line:1]
read_file map[end_line:330 path:internal/agent/turn.go start_line:240]
read_file map[end_line:377 path:internal/agent/turn.go start_line:330]
read_file map[end_line:230 path:internal/agent/loop.type.go start_line:190]
search_code map[file_type:go path:internal/agent pattern:func callKey|func errorOf|func runTool]

## tree after

