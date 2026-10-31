Can the advisor role call run_program? Where is that decided in the code?

right true, 68519 prompt (56064 cached), 991 completion, $0.00665, 7 rounds, 8 tools

## answer


No. `run_program` is copilot-only, and the mask is enforced in two places:

- **The decision**: each tool declares which roles admit it in its spec. `run_program` has `roles: RoleCopilot` — internal/agent/tools_run.go:48 (flags: write | run | agent). So an advisor, even though its role rank is higher (role.go:68-75), is not in that mask.
- **The enforcement**: `(id ToolID).allows(r)` checks `specs[id].roles & r` — internal/agent/role.go:83-86. It's called two ways:
  - `Loop.offered(id)` (internal/agent/tools.go:116-119) filters which tools are sent with each request, so the advisor never even sees `run_program` in its tool list.
  - The dispatcher re-checks it at call time (internal/agent/tools.go:192-196), returning an explicit "tool not available in this role" error if the model tries to call it anyway.

Consistent with the role reminder text at role.go:32-35: advisor "can read the code, the web and the library, but not change files" — and running programs counts as write/run territory.

## calls
know_find map[query:advisor role run_program permission]
search_code map[pattern:advisor]
know_find map[ids:[n2]]
read_file map[end_line:80 path:tools_run.go start_line:30]
read_file map[end_line:80 path:internal/agent/tools_run.go start_line:30]
read_file map[end_line:120 path:internal/agent/role.go start_line:70]
read_file map[end_line:69 path:internal/agent/role.go start_line:1]
search_code map[path:internal/agent pattern:allows\(|roleDefs]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793473424974848350"
author a2 armin session "bench-1793473444231005860"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-do-armin's-roles file checked a1 born 2026-10-31 seen 2026-10-31
  claim "tools_files.go:100 tools_run.go:50 tools_delegate.go:42 tools_teach.go:25 tools.go:116-119 role.go:83-86 tools.go:193-199 role_test.go:24-28 role_test.go:55-65 role.go:79-115 tools_know.go:532"
  evidence session "internal/agent/role.go" 00000000

n3 sessions/can-the-advisor-role file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/tools_run.go:48 role.go:68-75 internal/agent/role.go:83-86 internal/agent/tools.go:116-119 internal/agent/tools.go:192-196 role.go:32-35"
  evidence file "internal/agent/role.go:1-69" 7db61ac6
  evidence file "internal/agent/tools_run.go:30-80" 2cbba533

