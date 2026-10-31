How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 101724 prompt (84992 cached), 1038 completion, $0.01359, 8 rounds, 12 tools

## answer
All three use one shared scheme — a "project key" built by `ProjectKey` in `internal/session/session.go:74`:

**Folder name:** the project root's base directory name, then a `-`, then the first **8 hex chars of the SHA-256 of its absolute path**. E.g. `/home/henrique/Code/armin` → `armin-<8 hex>`. The hash is there so two checkouts named `api` don't collide.

**Full path:** `ProjectDataDir(kind, root)` (`session.go:87`) returns `$XDG_DATA_HOME/armin/<kind>/<key>` (data dir default `~/.local/share/armin`), and the only thing that differs per concern is the `<kind>` segment:

- memory → `.../armin/memory/<key>` — `internal/think/prompt.go:145`
- scratchpad → `.../armin/scratch/<key>` — `internal/think/prompt.go:155` (then a per-session subfolder `<key>/<session-uuid>/`)
- knowledge tree → `.../armin/know/<key>` — `internal/think/know.go:25`

Sessions themselves are not in these dirs; they live under `.../armin/sessions/` as plain `<uuid>.jsonl` files [n3].

## calls
know_find map[query:per-project folders memory scratchpad knowledge tree naming]
search_code map[pattern:scratchpad]
search_code map[path:internal/notes pattern:ProjectSlug|projectName|slug]
search_code map[file_typego:</function>
</tool_call>
<tool_call>
<function=search_code>
<parameter=path>
internal/know pattern:scratch.*%s|%v.*scratch p…
search_code map[pattern:func .*[Pp]rojectName]
search_code map[path:internal/notes pattern:scratch]
find_callers map[name:NewScratchpad]
search_code map[path:internal pattern:knowledge|knowtree|KnowTree]
search_code map[pattern:ProjectKey]
read_file map[end_line:110 path:internal/session/session.go start_line:40]
search_code map[pattern:ProjectDataDir\("]
read_file map[end_line:48 path:internal/think/know.go start_line:1]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473424974417517"
author a2 armin session "bench-1793473424974417517"
author a3 armin session "bench-1793473436708753258"

n1 persistence topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 persistence/sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 persistence/sessions/storage fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Each chat session is one JSONL file &lt;uuid&gt;.jsonl under &lt;XDG_DATA_HOME|~/.local/share&gt;/armin/sessions: a Meta line (id, root, model, title, timestamps) then one JSON line per llm.Message. Dir 0700, file 0600."
  evidence file "internal/session/session.go:36-198" d2686290
  evidence file "internal/session/session.type.go:1-28" 336538f9

n4 persistence/sessions/writing fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Writing: session.Save truncates and rewrites the whole file (infrequent: /save, rename, exit flush via REPL SaveNow). session.Append writes only history[*persisted:], advancing *persisted — the per-turn autosave path (REPL maybeAutosave, web UI), O(1) amortized. /clear and /compact must Save and reset persisted."
  derived-from n3
  evidence file "internal/session/session.go:140-247" 7edf4d7d
  evidence file "internal/repl/repl.go:397-420" 94b69314
  evidence session "internal/repl/sessions.go:44,65" 00000000
  evidence file "internal/web/web.go:111" e97f1d25

n5 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n6 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/repl/repl.go:385-421 internal/session/session.go internal/session/session.go:1-220 internal/session/session.type.go"
  evidence file "internal/repl/repl.go:385-421" 27a14fe0
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence session "internal/session/session.type.go" 00000000

n7 sessions/how-does-armin-name file checked a3 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go:74 session.go:87 internal/think/prompt.go:145 internal/think/prompt.go:155 internal/think/know.go:25"
  evidence file "internal/session/session.go:40-110" a26f7773
  evidence file "internal/think/know.go:1-48" b15a2220

