# The tools and the safety model

## The tools

The model has 37 narrow, structured tools (39 with a reference library) - never a free-form shell
string - plus the memory, scratchpad and skill tools described in
[memory.md](memory.md). Editing:

- **`edit_file`** - exact-string replacement. `old_string` must
  match exactly once (or set `replace_all`); a miss points at the
  line where the text appears with different whitespace.
- **`write_file`** - create a file, or overwrite one already read.
- **`read_pdf`** - the text of a PDF a page range at a time (10 by
  default), with `--- page N ---` markers, via poppler's `pdftotext`
  (`sudo apt install poppler-utils` if it's missing).

Both refuse to touch a file the model hasn't read this session, or
one that changed on disk since it was read. See [usage.md](usage.md#editing-files).

The original five:

- **`search_code`** - wraps `rg`, returns matched lines with a couple
  lines of context, not full files.
- **`read_file`** - reads a line range; files over 400 lines require
  `start_line`/`end_line`, forcing a search-then-read pattern instead of
  dumping whole files into context.
- **`list_directory`** - an indented tree, skips `.git`, `node_modules`,
  `vendor`, and build output.
- **`run_command`** - execs an allowlisted, read-only binary directly
  (`exec.Command(binary, args...)`, never a shell), from the project
  root. The default allowlist: `rg grep find git go cat ls wc head
  tail sed sort uniq cut tr diff stat file tree du df pwd echo which
  uname date basename dirname realpath nl sha256sum md5sum`. `sed`
  always runs with `--sandbox` and `-i` is refused; `find` refuses
  `-exec`, `-delete` and the `-fprint` family.
- **`run_program`** - builds, compiles, tests and runs code: `make`,
  `cmake`, `ninja`, `gcc`, `g++`, `clang`, `mpicc`, `mpirun`,
  `go`, `cargo`, `python3`, `pytest`, `node`, `npm`, `javac`,
  `typst`, `valgrind`, `gdb` and more, or a program the project
  built (`./build/fox`). Same no-shell `binary` + `args` form, an
  optional `dir` inside the project, and the build timeout. Each
  run is shown as `$ command` and asks `run it?`; `a` approves
  the rest of the session and `-yes` skips the question. It never
  runs where nobody can approve (the editor's read-only mode,
  MCP). Paths stay inside the project, and every command armin
  starts gets an environment with API keys and cloud credentials
  removed. `allow_exec = a, b` in config.conf adds programs.
  `rm`, `cp`, `mv`, `scp` and `rsync` are **privileged**: asked
  every time, even under `-yes` or after `a`, which only covers
  that one run for them.
- **`web_search`** - queries a local [SearXNG](https://docs.searxng.org)
  instance for things not in the codebase (library docs, error
  messages, current facts). `internal/searxng` owns a Docker container
  (`armin-searxng`, `searxng/searxng` image) started lazily on the
  first call and stopped on exit - but only if armin itself started
  it, never a container you already had running. Results are capped
  (5 by default, 10 max) and each snippet is truncated to ~200 chars,
  so one search can't dominate a small local model's context budget.

The other 29, grouped by category - full argument lists are in
`agent.ToolDefinitions()`:

**Repo stats** (`internal/tools/repostat.go`)

| Tool | What it does |
|---|---|
| `count_lines` | Recursive line count, total + per top-level directory, in one call - no shell pipes needed. |
| `list_languages` | File/line counts by extension, ranked. |
| `find_files` | Glob match on filenames (e.g. `*_test.go`) - a name-only complement to `search_code`. |
| `file_stat` | Size/lines/mtime for one file, without reading its content. |
| `find_todo_fixme` | Fixed `search_code` preset for TODO/FIXME/XXX. |
| `env_info` | Go version, OS/arch, whether git/ripgrep/docker are installed. |
| `project_manifest` | Directory tree + first README/manifest found - the same note the REPL auto-loads at session start, re-invocable mid-conversation. |

**Symbol navigation** (`internal/tools/symbols.go`, ctags-backed)

| Tool | What it does |
|---|---|
| `find_definition` | Where a symbol is defined - the same ctags fact `textDocument/definition` uses. |
| `list_symbols` | Fuzzy name search across every known symbol. |
| `outline_file` | A file's top-level symbols without reading its content. |
| `find_references` | Word-boundary ripgrep for where an identifier is *used* (ctags only indexes definitions). |

**Git history** (`internal/tools/gitinfo.go`)

| Tool | What it does |
|---|---|
| `git_log` | Structured commit history (hash/author/date/subject), not raw text to re-parse. |
| `git_blame` | Per-line author/date attribution for a range. |
| `git_diff` | Diff between two refs (default HEAD vs. working tree). |
| `git_show` | One commit's full diff + message. |
| `working_tree_diff` | Uncommitted changes only, plus untracked files. |
| `grep_history` | `git log -S`/pickaxe search for when text was introduced/removed. |
| `code_owners` | Commit-count-per-author heuristic for "who owns this." |
| `commit_stats` | Churn/hotspot ranking - most-frequently-changed files. |

**Build/test** (`internal/tools/build.go`)

| Tool | What it does |
|---|---|
| `run_tests` | The project's test command, with pass/fail, a failure summary and the end of the output. |
| `build_check` | The project's build or type-check command. |
| `lint_check` | The project's linter; `available: false` if there is none. |

The command is resolved per call, first match wins:

1. `test_cmd` / `build_cmd` / `lint_cmd` in `config.conf`
2. the same keys in the project's `.armin/commands.conf`
3. a `test:` / `build:` / `lint:` target in the `Makefile`
4. the toolchain: `go.mod` (go test/build/vet, golangci-lint when
   installed), `Cargo.toml` (cargo test/build/clippy), `package.json`
   scripts (npm/pnpm/yarn/bun by lockfile; `tsc --noEmit` for build
   when there's only a `tsconfig.json`), Python (pytest, compileall,
   ruff when installed)

The model can pass `args` (e.g. `["./internal/tools", "-run",
"TestX"]`), which replace the default target. These run under
`build_timeout` (default 10m), and their output is cut from the
front, so the verdict at the end survives. Running tests runs the
repo's own code, which is why `.armin/commands.conf` is trusted the
same way a Makefile is.

**Dependencies** (`internal/tools/deps.go`)

| Tool | What it does |
|---|---|
| `list_dependencies` | Parses whichever manifest exists - go.mod, package.json, requirements.txt, Cargo.toml, pyproject.toml. |
| `module_graph` | Which of this Go module's own packages import which, via `go list -json` (no `golang.org/x/tools` dependency). |

**Documentation** (`internal/tools/doccomment.go`)

| Tool | What it does |
|---|---|
| `read_doc_comment` | Just a symbol's doc comment, without reading the whole file. |

**Call graph** (`internal/tools/callgraph.go`, Go-only, syntactic - not type-checked)

| Tool | What it does |
|---|---|
| `find_callers` | Syntactic call sites of a function/method name. |
| `find_dead_code` | Exported functions/methods with zero syntactic call sites - a signal, not proof (see the tool's own doc comment for a confirmed false-positive source: ctags can't tell a struct field from a method). |
| `impact_analysis` | BFS outward to a bounded depth - what might need re-checking if a function changes. |

**Semantic search** (`internal/tools/semantic.go`)

| Tool | What it does |
|---|---|
| `semantic_search` | Keyword/term-frequency ranking - **not** real embeddings (armin takes on no vector/embedding dependency, same stance as `web_search`'s SearXNG choice). Useful when you don't know the exact string to grep for. |

### Files outside the project

Every path is jailed to the project root. To let armin read
something next to it - slides in the parent folder, a reference
repo - grant it explicitly:

```sh
armin . -add-dir ..          # at startup
/add-dir ~/Documents/papers  # or in the REPL
```

Added directories are **read-only**: read tools (`read_file`,
`read_pdf`, `search_code`, `list_directory`) accept absolute paths
inside them, and every write tool still refuses anything outside
the project. armin tells the model what it can now read and lists
the directory, so "read the slides in the parent folder" works.

You don't have to do it up front: when the model reads a path
outside the project (`read_pdf ../slides.pdf`, `cat ../notes.txt`),
armin asks `grant read access?` before the call runs, and grants
the file's folder on a yes. A folder you decline isn't asked about
again that turn. The model can also ask for a whole folder with
`request_access`, which names a directory and a reason. That question is
asked even under `-yes` or "all this session", which only cover
file writes. Front ends with no one to ask (the editor's
read-only mode, MCP) refuse and tell the model to suggest
`-add-dir`. In the REPL, `add-dir <dir>` without the slash also
works, when `<dir>` exists.

### Web search engines

`web_search` takes a `topic`: `general` (Yandex, Bing, Wikipedia),
`code` (those plus Stack Overflow and GitHub) or `science` (arXiv,
Semantic Scholar, OpenAlex, plus general). The engine list is
armin's own, chosen by testing: SearXNG's defaults (DuckDuckGo,
Startpage, Brave, Qwant…) answer a home connection with CAPTCHAs and
bans. Results that share no word with the query - Bing's habit of
answering an unfamiliar query with popular local pages - are
dropped. When engines fail and nothing comes back, the tool says
which engines failed, and the model is told to report it instead of
answering from memory. armin's generated `settings.yml` is versioned
and upgraded automatically (keeping its secret key); delete its
first line to keep your own edits.

## Safety model

Enforced in code, not left to model judgment:

- **Path jail** - every path resolves to an absolute, symlink-resolved
  path and must stay under `-root`.
- **Binary allowlist** - `run_command` only execs names explicitly
  allowed; extend it via config, never by letting the model ask.
  Its path-like arguments (`..`, anything with a `/`, `~/…`,
  `--flag=path`) go through the same jail as every other tool's.
- **No shell** - arguments are always a literal argv array; there's
  nothing for `;`, `|`, `&&`, `$()` etc. to be interpreted by.
- **Repeat guard** - a call identical to one that failed, with no
  successful call in between, isn't run again, and a run (command,
  build, tests) repeated with exactly the same result is flagged;
  after two of either the turn stops and the model has to report
  back.
- **Strict arguments** - a parameter a tool doesn't have is an error
  listing the real ones, never silently dropped. For `run_command`
  and `run_program`, the shapes small models produce are fixed
  before the call is shown or run: `args` as one string, an alias
  (`arguments`, `argv`), or the whole command line in `binary`.
- **Timeout + output cap** - every subprocess call is bounded and
  truncated, with a clear truncation message telling the model how to
  narrow its query.
- **`-dry-run`** - see exactly what `run_command` would execute before
  trusting it to actually run; file writes are previewed, not
  written.
- **Writes** - jailed like reads, refused inside `.git/`, atomic
  (temp file + rename), only on files read this session and unchanged
  since, and shown as a diff for approval first (unless `-yes`).
  `-lsp` is read-only.

