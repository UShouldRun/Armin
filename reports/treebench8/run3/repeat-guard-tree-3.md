When the repeat guard has refused enough calls and stops tools for the rest of the turn, what is the model told?

right true, 64776 prompt (55936 cached), 682 completion, $0.00318, 6 rounds, 7 tools

## answer














The model is told the `repeatNote` message (internal/agent/turn.go:42-45), appended to history as a user-role message before the final tool-less call:

> "[armin] You kept repeating a call that had already failed, so tools are off for the rest of this turn. Don't call any more tools. Tell the user what you were trying to do, what failed, and — if they gave feedback — how you'll follow it, then ask how to continue."

How it works: once `t.blocked >= maxBlocked` (2, turn.go:40), the loop sets `stopNote = repeatNote` and breaks (turn.go:132-135). The note then goes in as a user message, and one last `ChatStream` call is made with the tool list set to `nil` — so the model answers in plain text with a report for the user (turn.go:141-146).

## calls
search_code map[path:agent/loop pattern:repeat]
find_definition map[name:turn.run]
search_code map[pattern:repeat]
search_code map[file_type:go pattern:refus]
search_code map[file_type:go pattern:maxBlocked|repeatNote]
read_file map[end_line:50 path:internal/agent/turn.go start_line:42]
read_file map[end_line:180 path:internal/agent/turn.go start_line:95]

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

