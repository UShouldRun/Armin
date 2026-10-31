How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 56731 prompt (28992 cached), 900 completion, $0.00544, 5 rounds, 5 tools

## answer












All three per-project stores use the same naming scheme, built in one place: `ProjectKey` / `ProjectDataDir` in `internal/session/session.go:74-99`.

**The project key** (`ProjectKey`, session.go:74-83) is:
- the base name of the project's absolute path, then `-`, then the first **8 hex chars of the SHA-256 of its absolute path**.
- Example: `/home/henrique/Code/armin` → `armin-<8 hex>`.

The hash suffix exists so two checkouts with the same directory name (two `api` repos) don't collide.

**The folder path** (`ProjectDataDir`, session.go:88-99) is:

```
<data>/armin/<kind>/<project key>
```

where `<data>` is `~/.local/share` (or `$XDG_DATA_HOME` if set), and `<kind>` picks the store:

| store | kind | caller |
|---|---|---|
| memory | `memory` | `internal/think/prompt.go:165` |
| scratchpad | `scratch` | `internal/think/prompt.go:175` |
| knowledge tree | `know` | `internal/think/know.go:25` |

So on a default system you get `~/.local/share/armin/memory/<key>`, `…/scratch/<key>`, and `…/know/<key>`, all keyed by the same `<base>-<hash8>` name.

One wrinkle for the scratchpad: it's session-scoped *inside* that folder — for UUID sessions each session gets its own subfolder named by its session id, and deleting a session removes its scratchpad folder too (`internal/repl/sessions.go:194-198`).

## calls
read_file map[path:internal/session/session.go]
search_code map[pattern:scratchpad|knowledge|memory]
search_code map[pattern:ProjectKey]
read_file map[end_line:110 path:internal/session/session.go start_line:55]
search_code map[file_type:go path:internal pattern:ProjectDataDir\(|"know"|kind]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474539443299607"
author a2 armin session "bench-1793474539443299607"

n1 session topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 session/persistence concept inferred a1 born 2026-10-31 seen 2026-10-31
  claim "armin persists each chat session as one JSONL file at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (dir 0700, file 0600). Line 1 is Meta (id, root, model, title, created/updated); each later line is one llm.Message. Save (O_TRUNC full rewrite) is for /save, /title and exit flush; Append is the per-turn hot path writing only history[*persisted:] in O_APPEND. The REPL calls maybeAutosave() each turn (flagAutosave, off with -no-save)."
  evidence file "internal/session/session.go:1-230" 2cda16cd
  evidence file "internal/session/session.type.go:1-28" 336538f9
  evidence session "internal/repl/repl.go:113,317,398-404" 00000000

n3 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n4 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go internal/session/session.go:1-220 internal/session/session.type.go"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-220" 3a162043
  evidence session "internal/session/session.type.go" 00000000

