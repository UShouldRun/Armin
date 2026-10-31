Where are a project's knowledge tree files stored, and what are they called?

right true, 181675 prompt (168832 cached), 1588 completion, $0.01438, 13 rounds, 16 tools

## answer






























Here's how a project's knowledge tree is stored:

**Location:** `<data dir>/armin/know/<project key>`, where:
- the data dir is the OS-app data directory for `armin` — on Linux, `~/.local/share/armin` (per `session.DataDir()`; `armin-test` uses `~/.local/share/armin-test`, docs/guide/install.md:84)
- the project key is the project's base directory name plus the first 8 hex chars of the SHA-256 of its absolute path, e.g. `armin-1a2b3c4d` — so two checkouts both named `api` don't collide (session.go:71-91, `ProjectKey` / `ProjectDataDir`)
- the `know` segment is passed in at internal/think/know.go:25 (`session.ProjectDataDir("know", root)`)

**Files inside that directory** (created by `know.Open` in internal/know/store.go:60-90):

| file | what it is |
|---|---|
| `graph.snap` | the mmap'd binary snapshot of the whole tree (written on `Compact`, store.go:193) |
| `graph.log` | the append-only op log replayed on top of the snapshot (store.go:103-151) |
| `lock` | an flock file; a second armin opening the tree while another holds it runs read-only (store.go:76-92) |

The tree format is a custom binary graph (binary little-endian ops in `graph.log`, a snapshot image in `graph.snap`), not JSON/TOML — `-know dump` and `-know edit` produce the human-readable text view (docs/guide/usage.md:111).

## calls
read_file map[path:know/sessions/how-does-armin-save.md]
find_files map[pattern:*know*]
search_code map[pattern:knowledge tree]
list_directory map[path:internal/know]
search_code map[path:internal/know pattern:\.json|\.toml|\.yaml]
read_file map[path:internal/know/paths.go]
read_file map[path:internal/know/store.go]
read_file map[end_line:60 path:internal/think/know.go start_line:1]
search_code map[path:internal/know pattern:stamps|\.stamps|hashes]
search_code map[path:internal/think pattern:func ProjectDataDir|projectKey|sha256]
find_definition map[name:ProjectDataDir]
search_code map[pattern:ProjectDataDir]
search_code map[path:internal/know/stale.go pattern:Join\(]
read_file map[end_line:115 path:internal/session/session.go start_line:60]
search_code map[path:internal/know/stale.go pattern:stamps|OpenFile|Create]
search_code map[path:internal/know pattern:stamps]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475912519012784"
author a2 armin session "bench-1793475940945943559"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-save file checked a1 born 2026-10-31 seen 2026-10-31
  claim "session.go:48-69 session.go:121-133 session.type.go:9-21 session.go:148-170 session.go:118 repl/sessions.go:65 session.go:181 repl.type.go:58-62 repl.go:393-407 repl.go:113 repl.go:404 session.go:224-275 session.go:327 session.go:103"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:100-220" 6fefcd9c
  evidence session "internal/session/session.type.go" 00000000

n3 sessions/where-are-a-project's file checked a2 born 2026-10-31 seen 2026-10-31
  claim "docs/guide/install.md:84 session.go:71-91 internal/think/know.go:25 internal/know/store.go:60-90 store.go:193 store.go:103-151 store.go:76-92 docs/guide/usage.md:111"
  evidence session "internal/know/paths.go" 00000000
  evidence session "internal/know/store.go" 00000000
  evidence file "internal/session/session.go:60-115" f838a84b
  evidence file "internal/think/know.go:1-60" 8f16e11c
  evidence session "know/sessions/how-does-armin-save.md" 00000000

