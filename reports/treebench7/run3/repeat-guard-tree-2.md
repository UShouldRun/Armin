I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 34190 prompt (15552 cached), 828 completion, $0.00313, 4 rounds, 4 tools

## answer









One line: **`internal/agent/turn.go:36`** — the guard's limit is a constant:

```go
const maxBlocked = 2
```

Change it to:

```go
const maxBlocked = 3
```

That's all that's needed. Everything else is driven by it:

- **turn.go:290** — `if t.blocked >= maxBlocked` returns `"not run: this turn is stopping"` for each subsequent call (this is where tools effectively turn off mid-reply)
- **turn.go:128** — `if t.blocked >= maxBlocked` breaks the loop after the reply, so the turn ends with a tool-less final answer

`blocked` increments once per refused repeat in `t.run` (turn.go:278-279), so `maxBlocked = 3` means the model gets 3 refusals with feedback before the turn winds down. The test at `internal/agent/loop_test.go:548` computes its expected count from the constant (`1 + 1 + maxBlocked`), so it follows the new value automatically — no test edits.

## calls
search_code map[file_type:go path:internal/agent pattern:refus]
search_code map[path:internal pattern:maxBlocked]
read_file map[end_line:140 path:internal/agent/turn.go start_line:100]
read_file map[end_line:315 path:internal/agent/turn.go start_line:273]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474539443697272"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin's-agent file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/turn.go:273 internal/agent/loop.go:204 turn.go:289 turn.go:309-343 turn.go:36 turn.go:128-143 internal/agent/aids.go:137 turn.go:116-124 internal/agent/loop.type.go:199-205 internal/agent/loop_test.go:508"
  evidence file "internal/agent/turn.go:100-140" f6bb66c1
  evidence file "internal/agent/turn.go:255-330" c79f90f5
  evidence file "internal/agent/turn.go:330-365" b71a0597

