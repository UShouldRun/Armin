When the repeat guard has refused enough calls and stops tools for the rest of the turn, what is the model told?

right true, 38467 prompt (25536 cached), 560 completion, $0.00357, 4 rounds, 5 tools

## answer










Once `t.blocked` reaches `maxBlocked` (2), the turn is cut short and the model gets a final call **without tools**, preceded by a user-role message — `repeatNote` at `internal/agent/turn.go:42-46`:

> "[armin] You kept repeating a call that had already failed, so tools are off for the rest of this turn. Don't call any more tools. Tell the user what you were trying to do, what failed, and — if they gave feedback — how you'll follow it, then ask how to continue."

That message is appended as a `user` message (`turn.go:140-142`) before the last tool-less reply. Meanwhile, any tool calls in the reply that triggered the stop are given a stub result `"error": "not run: this turn is stopping"` (`turn.go:302-304`) so every call gets a result, but none of them actually run.

## calls
search_code map[pattern:repeat guard]
semantic_search map[query:repeated tool call guard stops tools for rest of turn]
search_code map[path:internal/agent pattern:blocked|stuck|maxBlocked]
read_file map[end_line:80 path:internal/agent/turn.go start_line:30]
search_code map[path:internal/agent pattern:stopNote]

## tree after

