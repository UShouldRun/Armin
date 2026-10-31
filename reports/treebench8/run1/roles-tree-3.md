When I switch to the advisor in the middle of a conversation, how is the model told, and does the system prompt change?

right true, 64383 prompt (50304 cached), 942 completion, $0.00341, 5 rounds, 7 tools

## answer













**The system prompt does not change.** The model is told via a short reminder prepended to the next user message, deliberately so the system prompt stays byte-stable and the server's prompt cache isn't invalidated (internal/agent/role.go:1-6).

The flow when you switch mid-conversation:

1. **`SetRole`** (internal/agent/role.go:91-98) sets `l.Role` and queues `l.reminder = roleReminders[r]`. For advisor it's the text at role.go:36-39:

   > `[armin] Your role is now advisor: help the user make key decisions and learn. You can read the code, the web and the library, but not change files; when code needs writing, say what, and leave it to the copilot.`

2. **`takeReminder`** (role.go:103-116), called at the start of every turn (internal/agent/turn.go:55), prepends that reminder to the last user message of the history — a brand-new message, so nothing previously cached changes. It's consumed once (`l.reminder = ""`), so subsequent turns are unaffected. It only fires when the last message is actually a user message.

3. **What does change is the tool list, not the prompt.** Each role has its own prebuilt tool definitions (`defsFor`, internal/agent/tools.go:149-173, per-role `roleDefs` cached in `Loop.roleDefs` at loop.type.go:109). `offered()` (tools.go:116-146) filters tools by `id.allows(l.Role)`, so the advisor's request simply carries a different tool list — no write tools (enforced even in `execute` at tools.go:196-202 with an error message the model can read).

So mid-conversation: history unchanged, system prompt unchanged, tool definitions swap per-role, and the new advisor message reads the `[armin] Your role is now advisor...` line at the top of your next user message.

## calls
read_file map[path:internal/agent/role.type.go]
search_code map[path:internal/agent pattern:advisor]
read_file map[path:internal/agent/role.go]
search_code map[path:internal/agent pattern:system|System]
find_references map[name:takeReminder path:internal/agent]
search_code map[path:internal/agent pattern:roleDefs|Role ==|l\.Role]
read_file map[end_line:195 path:internal/agent/tools.go start_line:110]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475801300534889"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-do-armin's-roles file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/role.type.go:15-19 internal/agent/tools_run.go:50 internal/agent/tools_files.go:100 internal/agent/tools_delegate.go:43 internal/agent/tools_teach.go:25 internal/agent/tools.go:116-119 internal/agent/role.go:83-87 tools.go:120-138 internal/agent/tools.go:141-160 internal/agent/tools.go:194-201 internal/agent/role.go:92-98 role.go:31-41 role.go:101-114 internal/agent/role_test.go:26-30"
  evidence session "internal/agent/role.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:100-215" d469d3e6

