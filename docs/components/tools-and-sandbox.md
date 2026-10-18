# Tools and the sandbox

What armin's tools do, and the boundaries they work inside.

## Tools (`internal/tools`)

Plain functions: a sandbox (or the symbol index, or web search) and a
typed input in, a typed output or an error out. They know nothing
about models; the agent's tool table decides which the model sees and
calls them (see [the agent](agent.md)).

| group | files | tools |
|---|---|---|
| files | `readfile.go`, `pdf.go`, `listdir.go`, `edit.go` | read, PDFs, folders, write, edit |
| search | `search.go`, `semantic.go`, `websearch.go` | ripgrep, keyword-ranked files, the web |
| code | `symbols.go`, `doccomment.go`, `callgraph.go`, `deps.go`, `repostat.go` | definitions, references, callers, dead code, dependencies, project facts |
| git | `gitinfo.go` | log, blame, diff, show, history search, owners, stats |
| running | `runcommand.go`, `runprogram.go`, `build.go` | read-only commands, approved programs, tests, build, lint |

**Writes** are planned first (`PlanWrite`, `PlanEdit`): a `Change`
with the old and new content, which the agent shows for approval and
only then applies. A `FileTracker` refuses writing a file the model
hasn't read, or one that changed since it did.

## The sandbox (`internal/sandbox`)

The boundaries are enforced in code, not left to the model's judgment:

- **Path jail** (`ResolvePath`): symlinks are resolved before the
  check. Reads stay in the project and the folders you added (`-add-dir`,
  `/add-dir`, the library); writes, edits and programs need the
  project itself (`InRoot`).
- **Allowlists**: `run_command` only runs read-only programs on its
  list (no shell, so no pipes, redirects or `$VARS`; `sed -i` and
  `find -exec` are refused). `run_program` runs build tools and
  interpreters, each run approved by you. Privileged programs (`sudo`
  and the like) are always asked about, even under `-yes`.
- **`Exec`**: the one way a program runs. Always directly, never
  through a shell; under a timeout (`timeout`, or `build_timeout` for
  tests and builds); output cut to `max_output` unless the caller
  parses it. Children run in their own session with no terminal (so
  git and ssh fail instead of waiting on a prompt), get an environment
  without API keys, and a timeout kills their whole process group.
- **Dry run** (`-dry-run`): `run_command` describes instead of runs.
