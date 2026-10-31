When I switch to the advisor in the middle of a conversation, how is the model told, and does the system prompt change?

right true, 154731 prompt (132352 cached), 1660 completion, $0.02054, 9 rounds, 14 tools

## answer
The system prompt does **not** change. The model learns about the switch two other ways, and the tool list changes:

**1. The system prompt is deliberately left byte-stable** — see `internal/agent/role.go:1-7`: "switching appends a short reminder to the next user message instead of rewriting the system prompt, so everything before it stays in the server's prompt cache."

**2. A role reminder is injected into the next user message.** `SetRole` stores `roleReminders[role]` (`internal/agent/role.go:91-98`), and `takeReminder` prepends it to the content of the next user message (`role.go:102-115`). For the advisor it says:

> `[armin] Your role is now advisor: help the user make key decisions and learn. You can read the code, the web and the library, but not change files; when code needs writing, say what, and leave it to the copilot.`

**3. The tool definitions the model sees change.** `definitions.advisor` omits the write tools — `offered()` in `internal/agent/tools.go:116-119` filters specs by the role's bitmask, so `edit_file`, `write_file`, `run_program` are never offered. As a backstop, `execute` refuses any role-forbidden call with `the advisor role can't use X; the user can switch roles with /copilot` (`tools.go:195-202`).

**4. Possibly the model itself.** `switchRole` in `internal/repl/roles.go:25-44` first calls `switchModel` to the model config.conf names for the advisor (`advisor = planner` in your config), and updates the "You are running as model …" line inside the system prompt via `SetModelLine` (`internal/agent/context.go:95-99`) — the only in-place edit made to the system prompt on a role switch. If the role has no configured model, the current model is kept and it says so (`now the advisor, on … (no advisor = in config.conf)`).

## calls
search_code map[pattern:advisor]
search_code map[pattern:Advisor]
search_code map[file_type:go path:internal pattern:/advisor|/copilot|/minion]
find_callers map[name:SetRole]
read_file map[path:internal/repl/roles.go]
search_code map[file_type:go path:internal pattern:systemPrompt|SystemPrompt|system prompt]
read_file map[path:internal/think/prompt.go]
search_code map[file_type:go path:internal/repl pattern:SystemPrompt|system]
search_code map[file_type:go path:internal/agent pattern:SetModelLine|ModelLine]
search_code map[file_type:go path:internal/repl pattern:func (r \*REPL) switchModel]
search_code map[file_type:go path:internal/repl pattern:switchModel]
search_code map[file_type:go path:internal/agent pattern:func (l \*Loop) SetRole|Role Role|l.Role]
read_file map[end_line:240 path:internal/agent/tools.go start_line:180]
read_file map[end_line:120 path:internal/agent/role.go start_line:1]

## tree after

