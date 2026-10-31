When the repeat guard has refused enough calls and stops tools for the rest of the turn, what is the model told?

right true, 45803 prompt (39040 cached), 568 completion, $0.00402, 5 rounds, 5 tools

## answer











The model gets the `repeatNote` (internal/agent/turn.go:42-45), appended as a user message and then one final call runs with tools removed (turn.go:132-143):

> `[armin] You kept repeating a call that had already failed, so tools are off for the rest of this turn. Don't call any more tools. Tell the user what you were trying to do, what failed, and — if they gave feedback — how you'll follow it, then ask how to continue.`

Context for completeness: the guard trips at `maxBlocked = 2` (turn.go:40) — two times a call identical (name+args) to one that already failed gets refused. On the second refusal it also returns an inline tool result `"not run: this turn is stopping"` (turn.go:302-304), then the loop breaks and the model is handed the note above with `nil` tools, so the user gets a report instead of an error.

## calls
search_code map[pattern:repeat guard]
semantic_search map[query:repeat guard refuses repeated tool calls and stops tools for rest of turn]
search_code map[path:internal/agent pattern:blocked]
search_code map[path:internal/agent pattern:repeatNote|stopNote]
read_file map[end_line:50 path:internal/agent/turn.go start_line:36]

## tree after

