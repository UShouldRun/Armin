How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 63349 prompt (53504 cached), 963 completion, $0.00919, 7 rounds, 8 tools

## answer
All three use one shared scheme, built by `session.ProjectKey` / `session.ProjectDataDir` in `internal/session/session.go:65-99`:

**Path:** `<data>/armin/<kind>/<project key>`, where the data dir honors `XDG_DATA_HOME` and defaults to `~/.local/share` (`session.go:49-58`).

**The project key** is the project's base directory name plus the first 8 hex chars of the SHA-256 of its absolute path — e.g. `armin-deadbeef`. This keeps two checkouts named `api` from colliding (`session.go:72-83`, doc comment: "its base name plus a short hash of its absolute path").

The `<kind>` component distinguishes the three folders:

| Folder | Kind string | Call site |
|---|---|---|
| Memory | `"memory"` | `internal/think/prompt.go:165` |
| Scratchpad | `"scratch"` | `internal/think/prompt.go:175` |
| Knowledge tree | `"know"` | `internal/think/know.go:25` (`openKnow`) |

So for a repo at `/home/henrique/Code/armin` you'd get, e.g., `~/.local/share/armin/memory/armin-5f1c2a9b`, `~/.local/share/armin/scratch/armin-5f1c2a9b`, and `~/.local/share/armin/know/armin-5f1c2a9b`. (Sessions are the one per-project thing that lives elsewhere: `<data>/armin/sessions/*.jsonl`, `session.go:61-68`.)

## calls
project_manifest map[]
search_code map[pathinternal:k pattern:armin]
search_code map[path:internal pattern:UserHomeDir]
read_file map[end_line:90 path:internal/session/session.go start_line:30]
list_directory map[path:internal/know]
find_references map[name:ProjectDataDir]
read_file map[end_line:115 path:internal/session/session.go start_line:90]
read_file map[end_line:35 path:internal/think/know.go start_line:1]

## tree after

