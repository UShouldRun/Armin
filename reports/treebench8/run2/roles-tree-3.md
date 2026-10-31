When I switch to the advisor in the middle of a conversation, how is the model told, and does the system prompt change?

right true, 29566 prompt (27776 cached), 771 completion, $0.00352, 4 rounds, 3 tools

## answer








The system prompt does **not** change. Roles are communicated purely through a one-shot reminder injected into the next user message.

**Mechanism** (internal/agent/role.go):

1. `SetRole` (role.go:91-98) just flips `l.Role` and stores the new role's text in `l.reminder`. No prompt reconstruction.
2. `takeReminder` (role.go:104-115) runs at the start of the next turn: if the last message is from the user, it prepends the reminder to that message's content and clears it. So the model reads it inline, e.g. for advisor (role.go:36-38):
   > `[armin] Your role is now advisor: help the user make key decisions and learn. You can read the code, the web and the library, but not change files; when code needs writing, say what, and leave it to the copilot.`
3. It fires exactly once per switch — if the last message isn't a user turn (e.g. the model is mid-tool-loop), `takeReminder` bails and waits for the next user message.

Why this shape: prepending to the *new* user message is the last position in the history, so everything cached above it is untouched — that's the comment's "nothing cached changes" (role.go:100-101). It also keeps the reminder in recent context, where the model actually pays attention, versus a stale system-prompt line.

The real enforcement isn't the reminder anyway — it's `ToolID.allows` (role.go:83-87), which filters the tool set per role at call time, so the advisor literally can't call edit/write tools even if it tried. The reminder is just the model-facing explanation of that.

## calls
read_file map[end_line:115 path:internal/agent/role.go start_line:60]
search_code map[path:internal/agent pattern:roleReminders]
read_file map[end_line:45 path:internal/agent/role.go start_line:27]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793475912519647362"
author a3 armin session "bench-1793475912519647362"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/roles concept checked a1 born 2026-10-31 seen 2026-10-31
  claim "Roles (minion/copilot/advisor) limit which tools a model can call. Role is a bitmask (role.type.go:15-19); each tool spec has a roles mask and id.allows() (role.go:58-63) checks it. Checked twice: Loop.offered/defsFor (tools.go:116-152) filters the tool list per role (one cached list per role slot for prompt-cache stability), and Loop.execute (tools.go:194-199) refuses disallowed calls with a readable error."
  evidence file "internal/agent/role.type.go:15-19" a56f13bc
  evidence file "internal/agent/role.go:58-63" 7f9d8c9f
  evidence file "internal/agent/tools.go:116-152" 2e75df54
  evidence file "internal/agent/tools.go:194-199" cde35dde

n3 agent/roles/per-role-masks fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Per-role tool masks: edit_file/write_file = minion|copilot (tools_files.go:100,136); run_program = copilot only (tools_run.go:50); delegate = copilot|advisor (tools_delegate.go:43); learn_explained = advisor only (tools_teach.go:25); all other tools (reads, search, run_command, run_tests, know) have no mask = all roles. So minion = reads + small edits, copilot = everything, advisor = reads/web/tests/delegate/teach, no writes or run_program."
  requires n2
  evidence session "internal/agent/tools_files.go:100,136" 00000000
  evidence file "internal/agent/tools_run.go:50" b5a11498
  evidence file "internal/agent/tools_delegate.go:43" 09853e54
  evidence file "internal/agent/tools_teach.go:25" 6bb3c060

n4 agent/roles/switching fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "SetRole (role.go:71-80) does not rewrite the system prompt; it queues a role reminder that takeReminder prepends to the next user message only, keeping earlier messages byte-identical so the prompt cache survives. Pinned by role_test.go (mask + execute-refusal + reminder tests) and golden files testdata/definitions.{minion,copilot,advisor}.json, regenerated with `go test ./internal/agent -run Golden -update` (definitions_test.go:44-79)."
  evidence file "internal/agent/role.go:71-115" 0e3c532c
  evidence file "internal/agent/role_test.go:23-112" 21828139
  evidence file "internal/agent/definitions_test.go:44-79" f9201682

n5 sessions topic inferred a3 born 2026-10-31 seen 2026-10-31

n6 sessions/how-do-armin's-roles file checked a3 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go:1-60 internal/agent/role.go internal/agent/role.type.go internal/agent/role_test.go internal/agent/tools_run.go:30-80"
  evidence file "internal/agent/definitions_test.go:1-60" ef91c048
  evidence session "internal/agent/role.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence session "internal/agent/role_test.go" 00000000
  evidence file "internal/agent/tools_run.go:30-80" 2cbba533

