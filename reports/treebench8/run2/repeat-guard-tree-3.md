When the repeat guard has refused enough calls and stops tools for the rest of the turn, what is the model told?

right true, 24075 prompt (18944 cached), 471 completion, $0.00391, 3 rounds, 2 tools

## answer
It's told via `repeatNote`, injected as a stop note on the final tool-less call. When `t.blocked >= maxBlocked` (2 refused repeats, `turn.go:40`), the loop breaks with that note (`turn.go:132-134`), and the exact text (`turn.go:42-45`) is:

> "[armin] You kept repeating a call that had already failed, so tools are off for the rest of this turn. Don't call any more tools. Tell the user what you were trying to do, what failed, and — if they gave feedback — how you'll follow it, then ask how to continue."

That's distinct from the per-call refusal the guard returns to the model earlier in the turn (`turn.go:292-296`): "you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you."

## calls
search_code map[path:internal/agent pattern:repeat]
read_file map[end_line:50 path:internal/agent/turn.go start_line:37]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475912519734905"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin's-agent file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/turn.go:285-340 internal/agent/loop.go:204 turn.go:331 turn.go:288-298 turn.go:334 turn.go:337-349 turn.go:40 turn.go:302-308 internal/agent/turn.go:132-135 turn.go:42-45 internal/agent/aids.go:145-180 tools_delegate.go:144 internal/metrics/report.go:33-34 internal/agent/loop_test.go:508 docs/guide/tools.md:206-210"
  evidence file "internal/agent/aids.go:100-200" f3367d7a
  evidence file "internal/agent/turn.go:42-178" 104415ed

