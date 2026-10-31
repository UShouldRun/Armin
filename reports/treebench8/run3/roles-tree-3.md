When I switch to the advisor in the middle of a conversation, how is the model told, and does the system prompt change?

right true, 88263 prompt (77120 cached), 1580 completion, $0.00594, 7 rounds, 10 tools

## answer
















No system-prompt change — the switch is told to the model as a one-line reminder prepended to your next message, and the only other prompt-adjacent thing that changes is which tools are offered. The full path:

1. `/advisor` → `cmdRole` → `switchRole` (internal/repl/roles.go:27-49). If config.conf names a model for the role, it swaps the model first, then calls `r.Loop.SetRole(role)`.

2. `SetRole` (internal/agent/role.go:91-98) just flips `l.Role` and stores the role's text in `l.reminder` — no prompt reconstruction. The advisor text is `roleReminders[RoleAdvisor]` (role.go:36-38): *"Your role is now advisor: help the user make key decisions and learn. You can read the code, the web and the library, but not change files…"*

3. At the start of your next turn, `Turn` calls `takeReminder` (internal/agent/turn.go:55, impl role.go:104-115). If the last history message is from you, it prepends the reminder to that message's content and clears it. It fires exactly once per switch; if the turn starts mid-tool-loop (last message not a user one), it waits for the next user message.

4. The system prompt itself is built once at startup — `mind.SystemPrompt(false)` is passed to `repl.New` (internal/display/display.go:114-117) and stored as `r.system`; nothing in the switch path touches it. That's deliberate: the comment at the top of role.go (role.go:5-8) says the reminder approach exists so "everything before it stays in the server's prompt cache."

So what the model actually perceives on the next turn: same system prompt, same history, your new message prefixed with `[armin] Your role is now advisor: …` — plus a shrunken tool list. `Turn` calls `l.Definitions()` (turn.go:59), which is role-filtered per `id.allows(l.Role)` (tools.go:116-120, 83-87), so the advisor's definitions drop the write tools. Even if the model tried one anyway, execution rejects it with "the advisor role can't use X; the user can switch roles with /copilot" (tools.go:196-201) — the reminder is the explanation, `allows` is the enforcement.



Recorded the finding as n3, linked off the existing roles node [n2].

## calls
search_code map[pattern:advisor]
search_code map[file_type:go pattern:switch.*role|role.*switch]
read_file map[path:internal/agent/role.go]
read_file map[path:internal/repl/roles.go]
find_callers map[name:takeReminder]
search_code map[file_type:go pattern:allows\(]
read_file map[end_line:75 path:internal/agent/turn.go start_line:40]
read_file map[end_line:160 path:internal/agent/tools.go start_line:90]
search_code map[file_type:go pattern:systemPrompt|SystemPrompt]
know_write map[claim:Role switching never rewrites the system prompt (built once at startup via mind.SystemPrompt, display.go:115). SetRole (role.go:91…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793476031593170737"
author a2 copilot model "bench" session "bench-1793476066057488419"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-do-armin's-roles file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/role.type.go:15-19 tools_files.go:100 tools_run.go:50 tools_teach.go:25 tools_delegate.go:43 tools.go:116-118 tools.go:140-160 role.go:83-86 tools.go:195-199 role.go:92-99 role.go:106-115"
  evidence session "internal/agent/role.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:100-210" 338ef86a

n3 sessions/how-do-armin's-roles/how-are-role-switches-told concept checked a2 born 2026-10-31 seen 2026-10-31
  claim "Role switching never rewrites the system prompt (built once at startup via mind.SystemPrompt, display.go:115). SetRole (role.go:91-98) only flips l.Role and queues roleReminders[r] into l.reminder; takeReminder (role.go:104-115, called from Turn at turn.go:55) prepends it to the next user message once, and the tool list shrinks via l.Definitions() → offered() → id.allows(l.Role) (tools.go:116-120, 83-87), with a call-time rejection as backstop (tools.go:196-201). Design goal: keep the server prompt cache intact."
  requires n2
  evidence file "internal/agent/role.go:5-8" be1c9cff
  evidence file "internal/agent/role.go:36-38" 0cb60228
  evidence file "internal/agent/role.go:89-115" f2516af6
  evidence file "internal/agent/turn.go:55" e954e3ff
  evidence file "internal/repl/roles.go:27-49" 90e150be
  evidence file "internal/display/display.go:115" b41228bc

