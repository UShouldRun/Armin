How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 88040 prompt (73728 cached), 1238 completion, $0.01259, 8 rounds, 9 tools

## answer
All three use the same scheme, in `internal/session/session.go`:

**Project key** — `ProjectKey` (session.go:74) takes the base name of the project root's absolute path and appends the first 8 hex chars of the SHA-256 of that absolute path: `<name>-<sha8>`, e.g. `armin-1f2e3a4b`. The hash part is what stops two checkouts both named `api` from colliding.

**Folder layout** — `ProjectDataDir(kind, root)` (session.go:87) returns:

```
$XDG_DATA_HOME/armin/<kind>/<project key>
```

(defaulting to `~/.local/share/armin/...` when `XDG_DATA_HOME` is unset). The `<kind>` segment picks which thing:

- **memory** → `<data>/armin/memory/<key>` — project memories (`internal/think/prompt.go:145`); user-level memories are separate, at `<data>/armin/memory/user`
- **scratch** → `<data>/armin/scratch/<key>` — the scratchpad (`internal/think/prompt.go:155`)
- **know** → `<data>/armin/know/<key>` — the knowledge tree (`internal/think/know.go:25`)

Note this is distinct from the in-repo `.armin/` directory, which holds per-project `skills/` and `commands.conf` instead.

## calls
project_manifest map[]
search_code map[]
search_code map[path:internal pattern:UserHomeDir|\.armin|stateDir|StateDir]
read_file map[end_line:220 path:internal/think/prompt.go start_line:100]
search_code map[path:internal/know pattern:Slug|shortName|hash|Dir\(]
search_code map[path:internal/session pattern:ProjectDataDir]
search_code map[path:internal/session/session.go pattern:ProjectKey|projectKey]
read_file map[end_line:110 path:internal/session/session.go start_line:40]
find_references map[name:ProjectDataDir]

## tree after

