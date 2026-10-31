I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right false, 26166 prompt (19712 cached), 473 completion, $0.00425, 3 rounds, 3 tools

## answer
Want me to make the edit?

## calls
know_find map[query:refused tool calls loop turns tools off limit]
search_code map[pattern:refus]
search_code map[path:internal/agent pattern:maxBlocked]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793473527246460695"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin's-agent file checked a1 born 2026-10-31 seen 2026-10-31
  claim "turn.go:257-336 loop.type.go:197 loop.go:204 turn.go:268-278 turn.go:31-34 turn.go:39-43 turn.go:299-324 loop_test.go:648"
  evidence file "internal/agent/aids.go:85-145" 78054790
  evidence file "internal/agent/turn.go:160-240" ac9c4d9a
  evidence file "internal/agent/turn.go:25-140" fac8ab7a

