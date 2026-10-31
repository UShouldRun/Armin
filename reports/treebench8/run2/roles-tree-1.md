How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 127856 prompt (110512 cached), 2509 completion, $0.00607, 8 rounds, 15 tools

## answer


Roles are a bitmask filter that's checked twice: once when building the tool list the model sees, and again at execution time.

**The mechanism**

- `Role` is a `uint8` bitmask: `RoleMinion=0b001`, `RoleCopilot=0b010`, `RoleAdvisor=0b100` (`internal/agent/role.type.go:15-19`). A Loop holds one role at a time, or none.
- Each tool spec carries a `roles` mask. `id.allows(r)` (`internal/agent/role.go:58-63`) says yes if the role is none, the spec has no roles, or `roles & r != 0`.
- **Offerting**: `Loop.offered` (`internal/agent/tools.go:116-120`) drops any tool the role isn't admitted to, and `defsFor` (`tools.go:149-152`) builds one cached `[]llm.ToolDefinition` list per role slot — so each role's list is stable and prompt-cache-friendly. A tool never offered is never in the model's tool list.
- **Execution**: `Loop.execute` (`internal/agent/tools.go:194-199`) re-checks `offered` and returns a readable error (`"the advisor role can't use write_file; switch roles with /copilot"`) rather than running it — covering hallucinated or stale calls.

**Per-role access** (from the `roles:` fields in the specs):

| Tool | mask | who gets it |
|---|---|---|
| `edit_file`, `write_file` (`tools_files.go:100,136`) | minion\|copilot | minion, copilot |
| `run_program` (`tools_run.go:50`) | copilot | copilot only |
| `delegate` (`tools_delegate.go:43`) | copilot\|advisor | copilot, advisor (hands work to the minion model) |
| `learn_explained` (`tools_teach.go:25`) | advisor | advisor only |
| everything else (reads, search, `run_command`, `run_tests`, know tools…) | no mask | all roles |

So: **minion** = reads + small edits, no `run_program`; **copilot** = every tool; **advisor** = reads, web, tests, delegate and teach, but no writes or `run_program`.

**Switching roles** (`role.go:71-115`): `SetRole` doesn't rewrite the system prompt; it queues a per-role reminder that `takeReminder` prepends to the next user message only — earlier messages stay byte-identical so the prompt cache survives. This is pinned by `internal/agent/role_test.go` (mask assertions at `:26-30`, the execute-time refusal at `:57-68`) and by golden files `internal/agent/testdata/definitions.{minion,copilot,advisor}.json`, regenerated with `go test ./internal/agent -run Golden -update` (`internal/agent/definitions_test.go:44-79`).





Saved the role mechanics, per-role masks, and switching behavior to the knowledge tree (n2–n4) so this isn't re-derived next time.

## calls
search_code map[pattern:minion|copilot|advisor]
search_code map[pattern:role]
read_file map[path:internal/agent/role.type.go]
read_file map[path:internal/agent/role.go]
read_file map[end_line:80 path:internal/agent/tools_run.go start_line:30]
search_code map[path:internal/agent pattern:roles:\s*Role]
search_code map[path:internal/agent pattern:flagWrite|flagRun|flagAgent]
search_code map[path:internal/agent/tools.go pattern:allows|roleDefs]
run_command map[args:[internal/agent/definitions.advisor.json internal/agent/definitions.minion.json internal/agent/definitions.copilot.json] binary:wc]
find_files map[pattern:definitions*]
read_file map[path:internal/agent/role_test.go]
read_file map[end_line:60 path:internal/agent/definitions_test.go start_line:1]
know_write map[claim:Roles (minion/copilot/advisor) limit which tools a model can call. Role is a bitmask (role.type.go:15-19); each tool spec has a ro…
know_write map[claim:Per-role tool masks: edit_file/write_file = minion|copilot (tools_files.go:100,136); run_program = copilot only (tools_run.go:50);…
know_write map[claim:SetRole (role.go:71-80) does not rewrite the system prompt; it queues a role reminder that takeReminder prepends to the next user …

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

