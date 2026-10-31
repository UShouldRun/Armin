How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 52311 prompt (42656 cached), 1436 completion, $0.00309, 5 rounds, 7 tools

## answer


Roles act as a per-tool permission mask, enforced at two points: what's offered to the model, and what's allowed at execution time.

## The mechanism

1. **Each tool spec declares which roles admit it** via a `roles` bitmask field (see `internal/agent/toolid.go` and the tool specs). `ToolID.allows()` in `internal/agent/role.go:83` is the check: a spec with no roles set admits every role, and role `0` (no role set) admits every tool:
   ```go
   return r == 0 || roles == 0 || roles & r != 0
   ```

2. **Offer-time filtering.** `Loop.offered()` (`internal/agent/tools.go:116`) is the first gate in the chain: role first, then session read-only, then capability flags (memories, scratch, skills, library, know, minion, learner). `Definitions()` builds the tool list handed to the model from that filter, cached once per role — so the model literally never *sees* the tools its role can't use.

3. **Execute-time enforcement.** If the model calls a tool the role can't use anyway, `execute` returns a structured error instead of running it: `"the advisor role can't use write_file; the user can switch roles with /copilot"` (`tools.go:194-199`).

4. **Switching.** `/minion`, `/copilot`, `/advisor` call `SetRole` (`role.go:102`), which queues a short role reminder prepended to the *next user message* (not the system prompt) so the server's prompt cache stays valid (`takeReminder`, `role.go:108`).

## What actually differs per role

| tool | roles in spec |
|---|---|
| `edit_file`, `write_file` | minion \| copilot (`tools_files.go:100,136`) |
| `run_program` | copilot only (`tools_run.go:50`) |
| `delegate` | copilot \| advisor, plus needs a minion attached (`tools_delegate.go:42`) |
| `learn_explained` (teaching) | advisor only (`tools_teach.go:25`) |
| everything else (read/search, web, library, know) | no `roles` field → all roles |

So: **minion** can't run programs or delegate; **advisor** can read and teach and delegate but not modify files (it's also read-only in practice via its reminder: "when code needs writing, say what, and leave it to the copilot"); **copilot** has everything.

There are a few role-specific restrictions *inside* tools too: the minion gets capped confidence when writing to know (`minionLimits`, `tools_know.go:532`) and can't verify entries (`tools_know.go:706` — "only the copilot or the advisor verifies").

## calls
search_code map[pattern:minion|copilot|advisor]
semantic_search map[query:role-based tool access restrictions]
search_code map[path:internal/agent pattern:allows\(]
search_code map[path:internal/agent pattern:Role =|Role\.|Minion|Copilot|Advisor]
read_file map[path:internal/agent/role.go]
read_file map[end_line:210 path:internal/agent/tools.go start_line:110]
search_code map[path:internal/agent pattern:roles:\s*Role]

## tree after

