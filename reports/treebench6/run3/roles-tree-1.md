How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 118947 prompt (107440 cached), 1847 completion, $0.00489, 9 rounds, 10 tools

## answer


(Saved this as knowledge node `agent/roles` [n2] so it doesn't need rediscovering.)

## calls
know_find map[query:roles minion copilot advisor tool restrictions]
search_code map[pattern:minion|copilot|advisor]
read_file map[path:internal/agent/role.go]
read_file map[path:internal/agent/role.type.go]
search_code map[file_type:go path:internal/agent pattern:allows\(|roleDefs|Definitions\(\)]
search_code map[file_type:go path:internal/agent pattern:roles: Role]
read_file map[end_line:210 path:internal/agent/tools.go start_line:110]
search_code map[file_type:go path:internal/agent pattern:roles:]
read_file map[path:internal/agent/role_test.go]
know_write map[claim:Roles (minion/copilot/advisor) are a Role bitmask; a tool spec's roles mask limits which roles may call it (roles==0 admits all; n…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473527245627183"
author a2 armin session "bench-1793473527245627183"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/roles concept checked a1 born 2026-10-31 seen 2026-10-31
  claim "Roles (minion/copilot/advisor) are a Role bitmask; a tool spec's roles mask limits which roles may call it (roles==0 admits all; no role admits all). Only edit_file/write_file (minion+copilot), run_program (copilot), delegate (copilot+advisor), learn_explained (advisor) carry masks. Enforced at offer time via Loop.offered/Definitions and at execute time with a readable error."
  evidence file "internal/agent/role.type.go:15-19" a56f13bc
  evidence file "internal/agent/role.go:81-86" fe3c0a6b
  evidence session "internal/agent/tools.go:116-119,193-199" 00000000
  evidence file "internal/agent/tools_run.go:50" b5a11498
  evidence file "internal/agent/tools_delegate.go:42" 09853e54
  evidence file "internal/agent/tools_teach.go:25" 6bb3c060
  evidence session "internal/agent/role_test.go" 00000000

n3 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n4 sessions/how-do-armin's-roles file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/role.go internal/agent/role.type.go internal/agent/role_test.go internal/agent/tools.go:110-210"
  evidence session "internal/agent/role.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence session "internal/agent/role_test.go" 00000000
  evidence file "internal/agent/tools.go:110-210" 5bbeede2

