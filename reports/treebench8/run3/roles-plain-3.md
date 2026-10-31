When I switch to the advisor in the middle of a conversation, how is the model told, and does the system prompt change?

right true, 71056 prompt (59392 cached), 1136 completion, $0.01052, 6 rounds, 8 tools

## answer
The system prompt is **not** changed. Instead, a one-shot reminder gets prepended to your next user message, deliberately so, to keep the llama-server's prompt cache warm (a mid-conversation system-prompt edit would invalidate everything cached before it).

The flow:

1. **`/advisor`** → `cmdRole`/`switchRole` in `internal/repl/roles.go`. If `config.conf` names an `Advisor` model and it differs from the current one, the model switches first, then `Loop.SetRole(RoleAdvisor)` is called.

2. **`SetRole`** (`internal/agent/role.go:91`) just sets `l.Role` and stores the new role's text in `l.reminder`. Nothing is sent to the model yet.

3. **At the start of the next `Turn`** (`internal/agent/turn.go:55`), `takeReminder` (`role.go:103`) prepends the reminder to the last history entry — but only if it's a user message, so it lands exactly once and only in a new, uncached message:

   > `[armin] Your role is now advisor: help the user make key decisions and learn. You can read the code, the web and the library, but not change files; when code needs writing, say what, and leave it to the copilot.`

   If the last message isn't a user turn (e.g. you switch mid tool-loop), it bails and waits for the next user message. After it's used, `l.reminder` is cleared, so the model reads it exactly once per switch.

What *does* change per turn besides the reminder: the tool list offered to the model. Role is a permission mask checked via `ToolID.allows` (`role.go:77`), so the advisor simply gets read-only code/web/library/teaching tools and `delegate` — no write tools or `run_program`.

(One known wrinkle noted in `docs/design/cloud-note-round7.md`: because `takeReminder` runs before `recall`, the reminder text pollutes the BM25 recall query — the intended fix is to run `recall` first.)

## calls
search_code map[pattern:advisor]
project_manifest map[]
search_code map[pattern:takeReminder]
search_code map[file_type:go pattern:/advisor|/copilot]
read_file map[path:internal/agent/role.go]
search_code map[path:internal/agent pattern:takeReminder]
read_file map[path:internal/repl/roles.go]
read_file map[end_line:80 path:internal/agent/turn.go start_line:40]

## tree after

