How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 107692 prompt (94976 cached), 1214 completion, $0.01413, 9 rounds, 12 tools

## answer
All three use the same scheme, via `session.ProjectDataDir(kind, root)` (internal/session/session.go:87):

```
~/.local/share/armin/<kind>/<project key>
```

where the **project key** is the project root's base directory name plus a short hash of its absolute path — `ProjectKey` (internal/session/session.go:71) builds it as `filepath.Base(abs) + "-" + hex(sha256(abs))[:8]`. So `/home/henrique/Code/armin` becomes `armin-<8 hex chars>`; the hash means two checkouts both named `api` don't collide.

The three kinds:

- **memory** — `ProjectDataDir("memory", root)` (internal/think/prompt.go:145)
- **scratchpad** — `ProjectDataDir("scratch", root)` (internal/think/prompt.go:155), with a per-session subfolder: `scratch/<key>/<session UUID>/`
- **knowledge tree** — `ProjectDataDir("know", root)` (internal/think/know.go:25)

DataDir honors `XDG_DATA_HOME` when set. A stability test lives at internal/session/session_test.go:248.

## calls
search_code map[pattern:scratchpad]
search_code map[pattern:knowledge tree|knowledgeTree|knowledge_tree]
search_code map[file_type_ignored:go pattern:ProjectName|projectName|project name]
search_code map[file_type_ignored:go pattern:local/share/armin|UserDataDir|sharedir|dataDir]
search_code map[file_type:go pattern:local/share/armin]
search_code map[file_type:go pattern:projectDir|ProjectDir|projectName|ProjectName]
search_code map[file_type:go pattern:projectMem|knowDir|scratchDir]
search_code map[file_type:go pattern:ProjectSlug|projectSlug|slug]
find_definition map[name:ProjectDataDir]
search_code map[file_type:go path:internal/session pattern:func ProjectDataDir]
read_file map[end_line:140 path:internal/session/session.go start_line:40]
search_code map[file_type:go pattern:ProjectDataDir\("(know|tree|memory|scratch)"]

## tree after

