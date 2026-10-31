How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 76132 prompt (65280 cached), 1274 completion, $0.01123, 8 rounds, 10 tools

## answer
All three use the same naming scheme, built by `session.ProjectKey` (internal/session/session.go:71-79):

**The folder name** is `<project basename>-<hash8>` — the base name of the project root's absolute path, plus the first 8 hex chars of the SHA-256 of the absolute path, so two checkouts both named "api" don't collide. E.g. `~/Code/armin` becomes something like `armin-3f2a1b0c`.

**The per-store paths** come from `ProjectDataDir(kind, root)` (internal/session/session.go:87-97), which yields `<data dir>/armin/<kind>/<project key>`:

| store | path | used by |
|---|---|---|
| memory | `~/.local/share/armin/memory/<key>/` | `openNotes` (internal/think/prompt.go:165) |
| scratchpad | `~/.local/share/armin/scratch/<key>/<session id>/` | `openNotes` (prompt.go:175) — one subfolder per session |
| knowledge tree | `~/.local/share/armin/know/<key>/` | `openKnow` (internal/think/know.go:25) |

Sessions themselves are global under `~/.local/share/armin/sessions/` and not per-project-key.

One note: I first tried a web search for this, but web search is unavailable in this session (docker not found on this machine at armin startup), so everything above is from the repo itself.

## calls
web_search map[query:armin CLI coding assistant per-project memory scratchpad knowledge tree folders topic:general]
search_code map[pattern:scratchpad]
list_directory map[]
search_code map[file_typego:</function>
</tool_call>
<tool_call>
<function=search_code>
<parameter=pattern>
knowledge tree pattern:ProjectKey]
search_code map[file_typego:</function>
</tool_call>
<tool_call>
<function=search_code>
<parameter=pattern>
knowledge tree pattern:ProjectKey]
search_code map[file_type:go pattern:ProjectKey]
search_code map[pattern:knowledge tree]
read_file map[end_line:110 path:internal/session/session.go start_line:60]
search_code map[path--:internal>
</function>
</tool_call>
<tool_call>
<function=search_code>
</function>
</tool_call>
<tool_call>
<function=search_code>…
search_code map[file_type:go pattern:ProjectDataDir\(]

## tree after

