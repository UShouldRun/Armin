How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 56934 prompt (42784 cached), 789 completion, $0.00240, 5 rounds, 6 tools

## answer


All three live under the same layout, `<data>/armin/<kind>/<project key>`, built by `ProjectDataDir(kind, root)` (internal/session/session.go:87). The project key comes from `ProjectKey(root)` (internal/session/session.go:74): the project's base directory name plus a short hash — the first 8 hex chars of the SHA-256 of its **absolute path** — so two checkouts both named `api` don't collide.

The kinds are passed in by each consumer:

- **memory** — `session.ProjectDataDir("memory", root)` (internal/think/prompt.go:165, and importMemories in internal/display/know.go:157)
- **scratch** — `session.ProjectDataDir("scratch", root)` (internal/think/prompt.go:175); each session gets its own subfolder under it named by the session id
- **knowledge tree** — `session.ProjectDataDir("know", root)` (internal/think/know.go:25)

`<data>` is `DataDir()` (internal/session/session.go:47), honoring `XDG_DATA_HOME` and defaulting to `~/.local/share/armin`. So for `/home/henrique/Code/armin` you'd get e.g. `~/.local/share/armin/know/armin-<8hex>`. There's a test pinning that this is stable and contains the base name: `TestProjectDataDirIsStable` (internal/session/session_test.go:248).

## calls
search_code map[file_type:go pattern:hash]
search_code map[file_type:go pattern:scratchpad]
read_file map[end_line:110 path:internal/session/session.go start_line:60]
search_code map[file_type:go path:internal pattern:ProjectKey\(]
search_code map[file_type:go pattern:ProjectDataDir\(]
search_code map[file_type:go path:internal/session/session.go pattern:func DataDir]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474423248051177"
author a2 armin session "bench-1793474423248051177"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/storage fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Each chat session is one JSONL file <uuid>.jsonl under <XDG_DATA_HOME|~/.local/share>/armin/sessions/: a Meta line (id, root, model, title, timestamps) then one JSON line per llm.Message. Dir 0o700, files 0o600. Not keyed by project — the project root is inside the Meta line."
  evidence file "internal/session/session.go:1-110" 0a192a81
  evidence file "internal/session/session.type.go:10-18" e03daea8

n3 sessions/writing fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Writing: session.Save truncates and rewrites the whole file (infrequent: /save, rename, exit flush via REPL.SaveNow). session.Append is the per-turn autosave path — O_APPEND, writes only history[persisted:], advances the cursor, falls back to Save when persisted==0; commands that rewrite history in place (/clear, /compact) must resync with a full Save."
  derived-from n2
  evidence file "internal/session/session.go:141-220" 030c805e
  evidence file "internal/repl/repl.go:394-420" 6110c0d4
  evidence file "internal/repl/sessions.go:32-74" 5a123697

n4 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go internal/session/session.go:1-220 internal/session/session.go:220-427 internal/session/session.type.go"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence file "internal/session/session.go:220-427" 572f50fc
  evidence session "internal/session/session.type.go" 00000000

