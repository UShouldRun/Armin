When the repeat guard has refused enough calls and stops tools for the rest of the turn, what is the model told?

right true, 39264 prompt (27712 cached), 911 completion, $0.00311, 4 rounds, 4 tools

## answer









After `maxBlocked = 2` refused repeats (turn.go:37-40), the turn breaks out of its round loop and the model gets `repeatNote` as a user message — turn.go:42-45:

> "[armin] You kept repeating a call that had already failed, so tools are off for the rest of this turn. Don't call any more tools. Tell the user what you were trying to do, what failed, and — if they gave feedback — how you'll follow it, then ask how to continue."

It's then sent to the model one last time with `defs=nil` — i.e. no tool definitions at all — so it can only answer in plain text (turn.go:138-143). The result is a report to the user instead of an error.

For context, before that hard stop, each individual refusal gets a per-call JSON error back (turn.go:292-296): "you already made this exact call this turn and it failed: \<prev\> Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you." Two of those and the turn ends with the note above.

[n3]

## calls
search_code map[path:internal/agent pattern:repeat]
read_file map[end_line:200 path:internal/agent/aids.go start_line:120]
read_file map[end_line:150 path:internal/agent/turn.go start_line:42]
know_write map[claim:When the repeat guard has refused maxBlocked=2 repeats (turn.go:40,132), the turn breaks and repeatNote (turn.go:42-45) is appende…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475801300653491"
author a2 copilot model "bench" session "bench-1793475826884045818"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin's-agent file checked a1 born 2026-10-31 seen 2026-10-31
  claim "loop.go:204 turn.go:274 turn.go:292 turn.go:37-40 turn.go:138-149 aids.go:137-176 loop.type.go:200-205"
  evidence file "internal/agent/aids.go:90" e9b25bd7
  evidence file "internal/agent/turn.go:30" 6b1278ab

n3 sessions/how-does-armin's-agent/repeat-guard-note fact checked a2 born 2026-10-31 seen 2026-10-31
  claim "When the repeat guard has refused maxBlocked=2 repeats (turn.go:40,132), the turn breaks and repeatNote (turn.go:42-45) is appended as a user message, then one final tool-less ChatStream call (defs=nil) asks the model to report what it was trying, what failed, and ask the user how to continue. Per-rejection the model instead gets a JSON error result: \"you already made this exact call this turn and it failed... Don't repeat it: change the arguments, use another tool, or stop\" (turn.go:292-296)."
  evidence file "internal/agent/turn.go:40-45" c6f99262
  evidence file "internal/agent/turn.go:132-143" 09caea00
  evidence file "internal/agent/turn.go:292-296" 30a94e2d

