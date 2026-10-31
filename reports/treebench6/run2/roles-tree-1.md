How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 166284 prompt (148544 cached), 2843 completion, $0.00722, 10 rounds, 13 tools

## answer


Same question I just answered — here it is, from what's in the tree [n2] plus the execute path.

**The gate.** Each tool spec declares admitting roles via a `roles:` bitmask field. `Loop.offered()` (`internal/agent/tools.go:118`) checks `id.allows(l.Role)` first; if it fails, the tool is dropped from `Definitions()`, the per-role schema list sent with every request — so the model never even sees a disallowed tool. Only four tools restrict roles:

- `run_program` — `RoleCopilot` (`tools_run.go:50`)
- `edit_file` / `write_file` — `RoleMinion | RoleCopilot` (`tools_files.go:100,136`)
- `delegate` — `RoleCopilot | RoleAdvisor` (`tools_delegate.go:42`)
- `learn_explained` — `RoleAdvisor` (`tools_teach.go:25`)

Everything else has `roles: 0` and goes to all roles.

**Second layer — execute-time refusal.** Even if a call for a disallowed tool arrives, `execute` returns `"the <role> role can't use <tool>; the user can switch roles with /copilot"` (`internal/agent/tools.go:194-199`). So it's both hidden and refused, not just hidden.

**Net effect per role:**
- **minion**: everything except `run_program` (and effectively `delegate`/`learn_explained`). Its `know_write` is additionally capped — no decision nodes, confidence clamped to `inferred`, can't verify others (`tools_know.go:530-545`, `:706`).
- **copilot**: the full set — the only role with `run_program`.
- **advisor**: reads plus `delegate` and `learn_explained`, but no `edit_file`/`write_file`/`run_program`.

`SetRole` (`role.go:91`) just swaps the role and queues a reminder prepended to the next user message (`takeReminder`, `role.go:101`) — it doesn't rewrite the system prompt, so the prompt cache survives; each role's tool list is kept byte-stable on purpose (`definitions_test.go:54-56`).

## calls
search_code map[pattern:minion|copilot|advisor]
know_find map[query:roles minion copilot advisor tool access]
search_code map[file_type:go pattern:Role|role ==|switch role]
search_code map[file_type:go path:internal/agent pattern:RoleMinion|RoleCopilot|RoleAdvisor]
read_file map[path:internal/agent/role.type.go]
search_code map[file_type:go path:internal/agent pattern:SetRole|func .*Definitions|roles &]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:roles:]
run_command map[args:[-n roles: internal/agent] binary:grep]
read_file map[end_line:160 path:internal/agent/tools.go start_line:100]
search_code map[file_type:go path:internal/agent pattern:can't use|allows\(]
search_code map[file_type:go path:internal/agent/tools_know.go pattern:minion|verified|Role]
know_write map[claim:A Role (minion/copilot/advisor, bitmask at agent/role.type.go:17) gates which tools a Loop offers. Each tool spec names admitting …

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473478106548487"
author a2 armin session "bench-1793473478106548487"

n1 armin topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 armin/roles fact tested a1 born 2026-10-31 seen 2026-10-31
  claim "A Role (minion/copilot/advisor, bitmask at agent/role.type.go:17) gates which tools a Loop offers. Each tool spec names admitting roles via roles:; allows() (agent/role.go:83) admits tools with no roles for every role. Role-restricted tools: run_program=copilot only, edit_file/write_file=minion|copilot, delegate=copilot|advisor, learn_explained=advisor only. Unlisted tools go to all roles."
  evidence file "internal/agent/role.type.go:17-20" 53b99ab0
  evidence file "internal/agent/role.go:83-87" a619b3a1
  evidence file "internal/agent/tools_run.go:50" b5a11498
  evidence session "internal/agent/tools_files.go:100,136" 00000000
  evidence file "internal/agent/tools_delegate.go:42" 09853e54
  evidence file "internal/agent/tools_teach.go:25" 6bb3c060

n3 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n4 sessions/how-do-armin's-roles file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/tools.go:118 tools_run.go:50 tools_files.go:100 tools_delegate.go:42 tools_teach.go:25 internal/agent/tools.go:194-199 tools_know.go:530-545 role.go:91 role.go:101 definitions_test.go:54-56"
  evidence session "internal/agent/role.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:100-160" 2ec3f4b9

