When the repeat guard has refused enough calls and stops tools for the rest of the turn, what is the model told?

right true, 36018 prompt (29888 cached), 466 completion, $0.00320, 4 rounds, 4 tools

## answer









The model is told via the `repeatNote` message, injected as a user message in the closing (tool-less) call (`internal/agent/turn.go:42-45`):

> "[armin] You kept repeating a call that had already failed, so tools are off for the rest of this turn. Don't call any more tools. Tell the user what you were trying to do, what failed, and — if they gave feedback — how you'll follow it, then ask how to continue."

Mechanics: after `t.blocked >= maxBlocked` (2 refusals, `turn.go:40,132-134`), the loop breaks and sends that note to a final `ChatStream` call with `nil` tool definitions, so the model can only produce a plain-text report.

## calls
search_code map[pattern:repeat guard]
search_code map[pattern:stops tools]
search_code map[file_type:go path:internal/agent pattern:blocked|maxBlocked|tools off|repeatNote]
read_file map[end_line:150 path:internal/agent/turn.go start_line:40]

## tree after

