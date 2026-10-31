How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right false, 45484 prompt (31360 cached), 1032 completion, $0.00346, 5 rounds, 7 tools

## answer


Recorded that as [n3] in the knowledge tree.

## calls
know_find map[query:tool definitions roles stability]
search_code map[file_type:go pattern:tool.*definition]
read_file map[path:internal/agent/definitions_test.go]
read_file map[path:internal/agent/role.type.go]
search_code map[file_type:go path:internal/agent pattern:defsFor|func (l \*Loop) Definitions]
find_files map[path:internal/agent/testdata pattern:definitions*]
know_write map[claim:Armin keeps per-role tool definitions byte-stable two ways: a Loop caches one roleDefs per role built once via sync.Once (tools.go…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473527245533587"
author a2 armin session "bench-1793473527245533587"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/tools topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/tools/definition-stability fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Armin keeps per-role tool definitions byte-stable two ways: a Loop caches one roleDefs per role built once via sync.Once (tools.go:147), and TestDefinitionsGolden (definitions_test.go:21) goldens the marshaled JSON per role — definitions.json plus definitions.{minion,copilot,advisor}.json in testdata/. Accidental changes break model behavior and the prompt cache; regenerate with `go test ./internal/agent -run Golden -update`."
  evidence file "internal/agent/definitions_test.go:21-80" 1c8503e4
  evidence file "internal/agent/tools.go:142-149" 9210a987
  evidence file "internal/agent/role.type.go:23-31" 3c17162e

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go internal/agent/role.type.go"
  evidence session "internal/agent/definitions_test.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000

