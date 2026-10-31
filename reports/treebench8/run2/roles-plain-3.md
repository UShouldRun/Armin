When I switch to the advisor in the middle of a conversation, how is the model told, and does the system prompt change?

right true, 65009 prompt (49152 cached), 1170 completion, $0.01052, 5 rounds, 8 tools

## answer
Short answer: the system prompt is **not** rewritten. Instead, the model gets a short `[armin]` reminder line prepended to your **next user message**.

How it works:

1. `/advisor` → `cmdRole` → `switchRole` (`internal/repl/roles.go:28`), which first moves to the role's configured model if one is set, then calls `l.Loop.SetRole(role)`.
2. `SetRole` (`internal/agent/role.go:92`) just sets `l.Role` and stores `roleReminders[role]` in `l.reminder` — that's it. The file's header comment spells out the reason: the switch "appends a short reminder to the next user message instead of rewriting the system prompt, so everything before it stays in the server's prompt cache."
3. When your next message starts a turn, `l.takeReminder(*history)` runs first (`internal/agent/turn.go:55`). It prepends the reminder to the last (new) user message and clears it. For the advisor, the text is:

   > `[armin] Your role is now advisor: help the user make key decisions and learn. You can read the code, the web and the library, but not change files; when code needs writing, say what, and leave it to the copilot.`

   (`roleReminders`, `internal/agent/role.go:28-40`)

The enforcement side is separate from the reminder: the advisor's tool list is filtered by the role bitmask (`ToolID.allows`, `role.go:79`) — no `edit_file`/`write_file`/`run_program`, plus advisor-only tools like `learn_explained`. So the model learns about the switch from the reminder, and the sandbox/tool layer enforces it even if the model ignores it.

Edge case: if the last message in history isn't a user message, `takeReminder` skips and the reminder stays queued for a later turn; also `/clear` resets history to just the system prompt, and on resume the saved session's system prompt is replaced with today's (`internal/repl/sessions.go:229`).

## calls
project_manifest map[]
search_code map[pattern:advisor]
search_code map[file_type:go path:internal pattern:Advisor]
search_code map[file_type:go path:internal pattern:system prompt|SystemPrompt|systemPrompt]
search_code map[file_type:go path:internal/repl pattern:switchRole|cmdAdvisor|cmdCopilot|cmdMinion]
read_file map[path:internal/agent/role.go]
find_references map[name:takeReminder]
read_file map[path:internal/repl/roles.go]

## tree after

