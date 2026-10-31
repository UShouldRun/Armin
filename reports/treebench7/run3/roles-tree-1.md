How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 135055 prompt (120704 cached), 1939 completion, $0.00536, 11 rounds, 14 tools

## answer


Roles are enforced as a **tool permission mask** per tool spec, at three layers:

**1. Per-tool `roles` bitmask** — every tool spec names which roles may call it (`internal/agent/role.type.go:17-21` defines `RoleMinion/Copilot/Advisor` as bitflags). The check is in `allows` (`internal/agent/role.go:83-86`): a spec with `roles: 0` admits every role; a Loop with no role (`r == 0`) admits every tool. Only three tools restrict themselves:

| Tool | Spec | Effect |
|---|---|---|
| `run_program` | `RoleCopilot` (tools_run.go:50) | minion and advisor can't build/run code |
| `edit_file`, `write_file` | `RoleMinion \| RoleCopilot` (tools_files.go:100,136) | advisor can't change files |
| `delegate` | `RoleCopilot \| RoleAdvisor` (tools_delegate.go:43) | the minion itself can't delegate |
| `learn_explained` | `RoleAdvisor` (tools_teach.go:25) | teaching tool is advisor-only |

Everything else (all reads, search, git, know, web, memory, scratchpad…) has no `roles` field, so all roles can use it.

**2. Offer time** — `Loop.offered` (internal/agent/tools.go:116) first checks `id.allows(l.Role)`, so the tool *schema list* sent with each request is filtered per role and cached once per role in `l.roleDefs[l.Role.slot()]` (tools.go:149). The model only ever sees tools its role may call.

**3. Execute time** — the mask is re-checked at call time (tools.go:194-204): if the model calls a tool its role doesn't allow, armin returns an error the model can read — "the %s role can't use %s; the user can switch roles with /copilot" — rather than executing it. This matters because switching roles mid-conversation (tools.go:77-90, `SetRole`) appends a short reminder in front of the next user message instead of rewriting the system prompt, so earlier history stays in the prompt cache; the old role's tool list can still linger in context, and the execution-time refusal is what actually blocks it.

So concretely: **minion** gets everything except `run_program` and `delegate`; **copilot** gets everything (it's the only role that can run code, but has no advisor-only tools); **advisor** gets all reads/search/web/library plus `learn_explained` and `delegate`, but no file writes — and its delegated minion can't edit either (`edits := in.Edits && l.Role != RoleAdvisor`, tools_delegate.go:123). The user switches with `/minion`, `/copilot`, `/advisor` (docs/guide/config.md:58-62).

## calls
list_directory map[path:/Users/konradmuller/Code/armin/docs]
list_directory map[path:/home/henrique/Code/armin]
search_code map[path:internal pattern:minion]
list_directory map[path:/home/henrique/Code/armin/docs]
search_code map[path:internal/tools pattern:Role]
search_code map[path:internal/tools pattern:advisor]
search_code map[path:internal pattern:RoleAdvisor]
search_code map[path:internal pattern:allowedTools|AllowedTools|availableTools]
read_file map[path:/home/henrique/Code/armin/internal/agent/role.go]
search_code map[path:internal/agent pattern:roles:]
search_code map[path:internal/agent pattern:roleDefs|allows\(]
read_file map[end_line:230 path:/home/henrique/Code/armin/internal/agent/tools.go start_line:90]
read_file map[path:/home/henrique/Code/armin/internal/agent/role.type.go]
search_code map[path:docs pattern:advisor]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474539444053118"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-do-armin's-roles file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/role.type.go:17-21 internal/agent/role.go:83-86 tools_run.go:50 tools_files.go:100 tools_delegate.go:43 tools_teach.go:25 internal/agent/tools.go:116 tools.go:149 tools.go:194-204 tools.go:77-90 tools_delegate.go:123 docs/guide/config.md:58-62"
  evidence session "/home/henrique/Code/armin/internal/agent/role.go" 00000000
  evidence session "/home/henrique/Code/armin/internal/agent/role.type.go" 00000000
  evidence file "/home/henrique/Code/armin/internal/agent/tools.go:90-230" 5549be4b

