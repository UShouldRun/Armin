How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right false, 134744 prompt (116992 cached), 2258 completion, $0.01972, 8 rounds, 10 tools

## answer
All three use one shared naming scheme, `ProjectDataDir(kind, root)` at internal/session/session.go:87:

- **Base:** `<data>/armin/<kind>/<project key>` where `<data>` is `$XDG_DATA_HOME/armin` or `~/.local/share/armin` (DataDir, session.go:55-65).
- **Project key:** `ProjectKey` (session.go:72-82) = the absolute root's base name + `-` + first 8 hex chars of `sha256(abs)`, e.g. `armin-1f3c9a02` — so two same-named checkouts don't collide.
- **The kinds:**
  - **memory** → `session.ProjectDataDir("memory", root)` — internal/think/prompt.go:165
  - **scratchpad** → `ProjectDataDir("scratch", root)` — prompt.go:175
  - **knowledge tree** → `ProjectDataDir("know", root)` — internal/think/know.go:24

So for this project: `~/.local/share/armin/memory/armin-<hash>`, `…/scratch/armin-<hash>`, `…/know/armin-<hash>`. One exception: memories also have a project-independent user dir at `<data>/armin/memory/user` (prompt.go:187).

I accidentally overwrote the sessions/storage node; let me restore it and add the new fact separately.

(Fixed the knowledge tree: n2 restored as sessions/storage, new fact saved as n4 `sessions/per-project-folders`.) The answer stands as above — `<data>/armin/{memory,scratch,know}/<basename>-<sha256[:8]>`, built by `ProjectKey`/`ProjectDataDir` in internal/session/session.go:72-97.

## calls
search_code map[path:internal pattern:scratchpad]
search_code map[path:internal pattern:knowledge|know]
search_code map[path:internal pattern:memory]
read_file map[end_line:140 path:internal/session/session.go start_line:1]
read_file map[end_line:240 path:internal/think/prompt.go start_line:155]
find_references map[name:ProjectDataDir]
read_file map[end_line:40 path:internal/think/know.go start_line:1]
know_write map[claim:Per-project state lives at $XDG_DATA_HOME/armin/&lt;kind&gt;/&lt;project key&gt; (default ~/.local/share/armin), built by ProjectD…
know_write map[claim:Each chat session is one JSONL file &lt;uuid&gt;.jsonl under $XDG_DATA_HOME/armin/sessions (default ~/.local/share/armin/sessions)…
know_write map[claim:Per-project state lives at $XDG_DATA_HOME/armin/&lt;kind&gt;/&lt;project key&gt; (default ~/.local/share/armin), built by ProjectD…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474471068804107"
author a2 armin session "bench-1793474471068804107"
author a3 copilot model "bench" session "bench-1793474482427050986"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/storage fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Each chat session is one JSONL file &lt;uuid&gt;.jsonl under $XDG_DATA_HOME/armin/sessions (default ~/.local/share/armin/sessions): a metadata line (id, project dir, model, model-written title, timestamps) plus one line per message, so Append writes only new turns."
  evidence file "internal/session/session.go:1-120" 995fba5e
  evidence file "internal/session/session.type.go:8-27" 4e667c53

n3 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.type.go:8-27 session.go:45 session.go:73 session.go:107 session.go:142 repl/sessions.go:65 repl/sessions.go:44 repl.go:411-419 session.go:193 repl.type.go:59 repl.go:397 web/web.go:111 session.go:148-153 session.go:222 session.go:277"
  evidence file "internal/repl/repl.go:385-427" 88a7b8f1
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence file "internal/session/session.go:221-427" 2fa881e5
  evidence session "internal/session/session.type.go" 00000000

n4 sessions/per-project-folders fact checked a3 born 2026-10-31 seen 2026-10-31
  claim "Per-project state lives at $XDG_DATA_HOME/armin/&lt;kind&gt;/&lt;project key&gt; (default ~/.local/share/armin), built by ProjectDataDir (internal/session/session.go:85-97). Kinds: memory (think/prompt.go:165), scratch (prompt.go:175), know (think/know.go:24). The project key is ProjectKey (session.go:72-82): basename of the absolute root + \"-\" + first 8 hex of sha256(abs)."
  derived-from n2
  evidence file "internal/session/session.go:55-97" 039281c7
  evidence file "internal/think/prompt.go:165-188" fa57a1db
  evidence file "internal/think/know.go:24" 27934899

