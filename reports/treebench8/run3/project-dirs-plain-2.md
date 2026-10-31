How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 85165 prompt (70400 cached), 879 completion, $0.01149, 7 rounds, 7 tools

## answer
All three go through one scheme in `internal/session/session.go`:

**The folder name (the "project key")** — `ProjectKey` (session.go:73-85): the project root's base directory name + `-` + the first **8 hex chars of the SHA-256 of its absolute path**, e.g. `armin-1a2b3c4d`. The hash exists so two checkouts both named `api` don't collide.

**The full path** — `ProjectDataDir(kind, root)` (session.go:87-102) builds `<data>/armin/<kind>/<project key>`, where the data dir is `$XDG_DATA_HOME/armin` or `~/.local/share/armin` (session.go:48-60). The `<kind>` segment differs per thing:

- memory → `ProjectDataDir("memory", root)` — think/prompt.go:165
- scratchpad → `ProjectDataDir("scratch", root)` — think/prompt.go:175
- knowledge tree → `ProjectDataDir("know", root)` — think/know.go:25

So e.g. `~/.local/share/armin/know/armin-1a2b3c4d` holds this repo's knowledge tree, with `memory/` and `scratch/` as siblings under the same key.

## calls
project_manifest map[]
search_code map[file_type:go pattern:scratchpad]
search_code map[file_type:go pattern:SHA|sha256]
find_definition map[name:ProjectKey]
search_code map[pattern:ProjectKey]
read_file map[end_line:115 path:internal/session/session.go start_line:45]
find_references map[name:ProjectDataDir]

## tree after

