Can the advisor role call run_program? Where is that decided in the code?

right true, 35523 prompt (29184 cached), 629 completion, $0.00547, 4 rounds, 4 tools

## answer
No — the advisor can't call `run_program`. It's allowed for copilot only.

Where that's decided:

1. **The spec** — `internal/agent/tools_run.go:50-52`: `run_program`'s tool spec declares `roles: RoleCopilot` (the bitmask constants are in `internal/agent/role.type.go:15-19`, so copilot is `0b10`).
2. **The check** — `internal/agent/role.go:83-87`: `ToolID.allows(r)` returns true only if `roles & r != 0`. Since the advisor's bit isn't in `RoleCopilot`, the tool is simply not offered to an advisor-role Loop. A spec with no roles admits every role, but `run_program` has one, so it's restricted.
3. **Covered by test** — `internal/agent/role_test.go:26-29` explicitly asserts the advisor is not offered `edit_file`, `write_file`, or `run_program`, and `role_test.go:38` asserts the same for the minion.

This matches the knowledge tree: per-role tool masks put `run_program` in copilot's set only ([n3]).

## calls
search_code map[file_type:go path:internal/agent pattern:run_program]
read_file map[end_line:30 path:internal/agent/role.type.go start_line:1]
read_file map[end_line:75 path:internal/agent/tools_run.go start_line:30]
search_code map[path:internal/agent/role.go pattern:roles]

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

