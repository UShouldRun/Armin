How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 60084 prompt (52480 cached), 989 completion, $0.00872, 7 rounds, 8 tools

## answer
All three use the same scheme: `<data>/armin/<kind>/<project key>` where the kind is `memory`, `scratch`, and `know` — so on a default system you get e.g. `~/.local/share/armin/memory/<key>`, `~/.local/share/armin/scratch/<key>`, `~/.local/share/armin/know/<key>`.

The project key itself is `ProjectKey` (internal/session/session.go:74): the **basename of the absolute project root plus a `-` plus the first 8 hex chars of its SHA-256** — e.g. `myapi-a1b2c3d4` for `/home/u/Code/myapi`. The hash exists so two checkouts named "api" don't collide (session.go:71-73).

`ProjectDataDir` (session.go:87) builds the path; it's used with kind `memory` and `scratch` for openNotes (internal/think/prompt.go:145,155) and `know` for the knowledge tree (internal/think/know.go:25).

## calls
know_find map[query:per-project folder naming memory scratchpad knowledge]
search_code map[file_type.go: pattern:XDG_CONFIG_HOME|xdg]
semantic_search map[query:project name hash per-project directory memory scratchpad knowledge]
search_code map[pattern:projectSlug|projectHash|slugFor|projectDir]
search_code map[pathinternal:armin/memory pattern:scratchpad|knowledge]
search_code map[path:internal pattern:ProjectKey|ProjectDataDir]
read_file map[end_line:105 path:internal/session/session.go start_line:55]
know_write map[claim:Per-project state lives at $XDG_DATA_HOME/armin/<kind>/<project key> (kind: memory, scratch, know), via ProjectDataDir. The projec…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473478106232441"
author a2 armin session "bench-1793473478106232441"
author a3 copilot model "bench" session "bench-1793473491055247189"
author a4 armin session "bench-1793473491055247189"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/persistence fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Sessions persist as one JSONL file per session at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (default ~/.local/share/armin/sessions), dir 0o700, file 0o600. First line is the Meta (id, project root, model, title, timestamps), then one line per llm.Message. Save() rewrites the whole file (used by /save, exit flush SaveNow, /clear, /compact resync); Append() opens O_APPEND and writes only history[*persisted:] (per-turn autosave in the REPL maybeAutosave and web, O(1) amortized). Callers that mutate history in place must Save + reset persisted to 0. Load/List/resolve support ids or unique prefixes; legacy pre-id files <dir>-<hash>.jsonl still load, id taken from the filename."
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence file "internal/repl/repl.go:394-420" 6110c0d4
  evidence file "internal/repl/sessions.go:33-67" ed55b8c5
  evidence file "internal/web/web.go:109-113" 2be50de5

n3 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "session.go:55-70 session.go:39-48 session.go:1-220 session.go:8-17 session.go:117 repl/sessions.go:65 repl/repl.go:410-420 repl/sessions.go:44 session.go:180 repl/repl.go:398-408 web/web.go:111 session.go:126-132 session.go:313"
  evidence file "internal/repl/repl.go:390-421" 4bec4d83
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence file "internal/session/session.go:395-427" 57dd6f7e

n4 sessions/per-project-folders fact checked a3 born 2026-10-31 seen 2026-10-31
  claim "Per-project state lives at $XDG_DATA_HOME/armin/<kind>/<project key> (kind: memory, scratch, know), via ProjectDataDir. The project key is the basename of the absolute project root plus \"-\" plus the first 8 hex chars of sha256 of the absolute path (ProjectKey), so two \"api\" checkouts don't collide."
  derived-from n2
  evidence file "internal/session/session.go:71-99" e57356ce
  evidence file "internal/think/prompt.go:145-156" 8f42b104
  evidence file "internal/think/know.go:25" adb3d23e

n5 sessions/how-does-armin-name file checked a4 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go:74 session.go:71-73 session.go:87 internal/think/prompt.go:145 internal/think/know.go:25"
  evidence file "internal/session/session.go:55-105" a37b926b

