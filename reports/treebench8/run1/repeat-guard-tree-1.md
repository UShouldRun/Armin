How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 85297 prompt (59008 cached), 1439 completion, $0.00811, 6 rounds, 6 tools

## answer












The agent loop has a **repeat guard** built into `Turn` (`internal/agent/turn.go`), because small models tend to retry a failed call unchanged. It works in layers:

**1. Call identity** — every call is keyed by tool name + JSON-marshaled args (`callKey`, `loop.go:204`; maps are key-sorted so equal args give equal keys).

**2. Refusing the exact retry** — `t.run` (`turn.go:274`+) keeps `t.failed`: call key → error. If the model re-issues a call whose key is in `failed` and it failed with nothing successful in between, it isn't executed at all; instead it gets a tool result saying "you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop" (`turn.go:292`). A success anywhere clears `t.failed`, so a retry *after* a fix is allowed.

**3. Same-output loop detection** — for program runs, `t.ran` remembers each call's output. The same run with identical output **immediately back-to-back** is refused as a loop (`turn.go:~335`); after intervening calls (e.g. an edit + rebuild) it's allowed but gets a note that nothing changed.

**4. Stopping the turn** — each refusal bumps `t.blocked`. After `maxBlocked = 2` refusals (`turn.go:37-40`), the turn breaks out of the round loop; the remaining tool calls in that reply still get a stub result (`"not run: this turn is stopping"`) so every call ID has a reply, but nothing executes.

**5. Dignified wind-down** — after breaking, the loop appends `repeatNote` ("tools are off for the rest of this turn… tell the user what you were trying to do, what failed… ask how to continue") and makes **one final model call with no tools**, so the user gets a report instead of a dead loop (`turn.go:138-149`).

Separately, the **stuck aid** (`aids.go:137-176`) catches a softer case: 3 consecutive misses (errors, failing tests/builds, non-zero exits) without progress trigger a "step back" nudge telling the model to state what it knows, list hypotheses, and test the cheapest one — up to 2 nudges per turn. This is distinct from the repeat guard (which only fires on *identical* calls) and works even when the model varies arguments slightly and just keeps failing.

The turn's memory for all this lives on the `turn` value (`loop.type.go:200-205`): `failed`, `ran`, `lastCall`, and the `blocked` counter, scoped to one turn so a fresh user request starts clean.

## calls
search_code map[pattern:repeat]
project_manifest map[]
search_code map[file_type:go path:internal/agent pattern:repeat]
read_file map[path:internal/agent/turn.go start_line:30]
search_code map[file_type:go path:internal/agent pattern:func callKey|func (o \*?stuck)|stuckNote =]
read_file map[path:internal/agent/aids.go start_line:90]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475801300653491"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin's-agent file checked a1 born 2026-10-31 seen 2026-10-31
  claim "loop.go:204 turn.go:274 turn.go:292 turn.go:37-40 turn.go:138-149 aids.go:137-176 loop.type.go:200-205"
  evidence file "internal/agent/aids.go:90" e9b25bd7
  evidence file "internal/agent/turn.go:30" 6b1278ab

