How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 82761 prompt (69632 cached), 1059 completion, $0.01154, 7 rounds, 10 tools

## answer
All three per-project folders live under `$XDG_DATA_HOME/armin/<kind>/<project key>` (i.e. `~/.local/share/armin/...`), where the folder name is built by `session.ProjectKey` (internal/session/session.go:74):

- **Name = `<base name of the project root>-<first 8 hex chars of the SHA-256 of its absolute path>`** — e.g. `armin-3f9c2a1b`. The hash suffix is so two checkouts both named `api` don't collide.
- The three kinds (via `session.ProjectDataDir`, session.go:87):
  - `memory/` — memories for that project (internal/think/prompt.go:145)
  - `scratch/` — the scratchpad, one subfolder per session UUID (prompt.go:155)
  - `know/` — the knowledge tree (internal/think/know.go:25)
- Sessions are the exception: not per-project at all, they sit in `armin/sessions/<uuid>.jsonl` regardless of the project root [n2].

So for a project at `/home/henrique/Code/armin` you'd get e.g. `~/.local/share/armin/know/armin-<8 hex digits>` — same key suffix across all three kinds, only the `<kind>` segment differs.

## calls
know_find map[query:per-project folder names memory scratchpad knowledge tree]
project_manifest map[]
search_code map[file_type7:go pattern:armin\.local|scratchpad|scratchpad\.md|know\.json|knowledge]
search_code map[pattern:scratchpad]
search_code map[pattern:\.local|armin/]
find_definition map[name:ProjectKey]
search_code map[path:internal pattern:ProjectKey]
read_file map[end_line:140 path:internal/session/session.go start_line:60]
search_code map[path:internal/notes pattern:ProjectKey|memory/|know]
search_code map[path:internal pattern:ProjectDataDir\(]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473527245271763"
author a2 armin session "bench-1793473527245271763"
author a3 armin session "bench-1793473539008617526"

n1 session topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 session/storage fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Each chat session is one JSON-Lines file at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (or ~/.local/share/armin/sessions). Line 1 is a Meta (id, root, model, title, timestamps), then one JSON line per llm.Message. Save() rewrites the whole file (rename, /save, exit flush); Append() writes only new messages using a *persisted counter (O(1) per turn). Load() reads a file by id or unique prefix; List() reads only first lines."
  evidence file "internal/session/session.go:46-210" bdfbdb50
  evidence file "internal/session/session.type.go:1-28" 336538f9

n3 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n4 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "session.go:62 session.go:37 session.type.go:10-19 session.go:1-17 session.go:117 session.go:180 session.go:223 session.go:272 session.go:326 session.go:387 session.go:304-307"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence session "internal/session/session.type.go" 00000000

n5 sessions/how-does-armin-name file checked a3 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go:74 session.go:87 internal/think/prompt.go:145 prompt.go:155 internal/think/know.go:25"
  evidence file "internal/session/session.go:60-140" 8e4be5f5

