# armin

A terminal chat assistant, backed by a local Ollama model, that reads and
searches your codebase to answer questions grounded in what's actually
there — not generic training data. Named after the *Attack on Titan*
strategist: the one who reads the terrain before anyone moves.

Three ways to use the same sandboxed tool layer:
- **REPL** — a chat loop in your terminal, talking to a local model.
- **MCP server** (`-mcp`) — the same tools, exposed over stdio
  JSON-RPC, for any MCP client (Claude Desktop, etc.) to reuse.
- **LSP server** (`-lsp`) — hover-to-explain and an ask command, over
  the Language Server Protocol, for editor integration. One Lua file
  gets you this in Neovim with zero plugins — see
  [`docs/nvim.md`](docs/nvim.md).

## Requirements

- Go 1.23+ to build (the binary itself has no runtime dependencies)
- [Ollama](https://ollama.com) running locally, with a tool-calling model
  pulled (`ollama pull qwen3:4b` is the default)
- [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) on `PATH` —
  `search_code` shells out to it
- Optional: [Docker](https://docker.com), for the `web_search` tool —
  it runs a local [SearXNG](https://docs.searxng.org) instance,
  started on first search and stopped on exit (see "The tools"
  below). Missing docker just means `web_search` reports a clean
  "unavailable" error; everything else works as normal.
- Optional: [Universal Ctags](https://ctags.io) (`universal-ctags`) for
  `-lsp`'s go-to-definition and workspace-symbol search — not the
  Emacs `ctags` many distros ship as `/usr/bin/ctags`, which is a
  different, incompatible tool despite the name. Build it if your
  package manager doesn't have `universal-ctags` separately:
  ```sh
  git clone --depth 1 https://github.com/universal-ctags/ctags.git
  cd ctags && ./autogen.sh
  ./configure --prefix="$HOME/.local" --program-prefix=universal-
  make && make install
  ```

The Go module itself pulls in exactly one dependency,
`golang.org/x/sys`, for the handful of raw-terminal-mode syscalls
behind `/ghosttext` — everything else (the REPL, the LSP/MCP
JSON-RPC servers, the Ollama client, ANSI styling) is hand-rolled
against the standard library.

## Build

```sh
go build -o armin ./cmd/armin
```

## Quickstart

```sh
./armin -root /path/to/your/project
```

```
armin — terminal codebase assistant
  root: /path/to/your/project
  model: qwen3:4b (http://localhost:11434)
  type /help for commands, /quit to exit

❯ what does this project do?
```

On startup, armin silently looks at the directory tree and any
README/go.mod/package.json it finds, so the first answer doesn't require
explaining the project by hand.

## Slash commands

| Command | Does |
|---|---|
| `/help`, `/?` | list commands |
| `/quit`, `/exit`, `/q` | leave |
| `/clear` | reset the conversation, keeping only the system prompt |
| `/model [name]` | show the active model + what's installed in Ollama, or switch |
| `/context`, `/ctx` | message count, tool-call count, rough token estimate |
| `/compact [n]` | ask the model to summarize everything but the last `n` messages, to free up context |
| `/tools` | list the tools available to the model |
| `/root` | show the sandbox's project root, timeout, allowlist, dry-run state |
| `/history` | dump the raw conversation history |
| `/save` | force a session save right now |
| `/suggest` | toggle 2-3 numbered follow-up questions after each answer — type a number to ask one instead of writing it out |
| `/ghosttext` | toggle live ghost-text completion while you type (off by default — see below) |

Switching to a model with no Ollama tool-calling support (a plain
completion/FIM base model, say) doesn't fail with a raw HTTP error —
armin shows a clean one-line message naming the model and pointing
back at `/model` to pick a different one.

`/ghosttext` is a real trade: it switches your terminal into raw mode
to show and Tab-accept inline suggestions as you type, which means
you lose the terminal's own arrow-key editing and history for the
duration. It falls back to a normal read with zero side effects
whenever stdin isn't an interactive terminal (piped input, scripts),
so it's always safe to leave configured — it only changes behavior
when you explicitly turn it on.

## Flags

| Flag | Default | Does |
|---|---|---|
| `-root` | `.` | project root; every path argument is jailed to it |
| `-model` | `qwen3:4b` | Ollama model name |
| `-ollama-url` | `http://localhost:11434` | Ollama base URL |
| `-timeout` | `20s` | per-subprocess timeout |
| `-max-output` | `4000` | bytes of tool output before truncation |
| `-dry-run` | off | `run_command` describes what it would execute instead of running it |
| `-mcp` | off | serve the tools over MCP stdio instead of starting the REPL |
| `-lsp` | off | serve hover + an ask command over LSP stdio instead of starting the REPL |
| `-resume` | off | resume the most recent saved session for this root |
| `-no-save` | off | don't persist the session to disk after each turn |
| `-no-color` | off | disable ANSI styling |

Flag defaults can also come from a config file (see below); explicit
flags always win.

## The tools

The model has 34 narrow, structured tools — never a free-form shell
string. The original five:

- **`search_code`** — wraps `rg`, returns matched lines with a couple
  lines of context, not full files.
- **`read_file`** — reads a line range; files over 400 lines require
  `start_line`/`end_line`, forcing a search-then-read pattern instead of
  dumping whole files into context.
- **`list_directory`** — an indented tree, skips `.git`, `node_modules`,
  `vendor`, and build output.
- **`run_command`** — execs an allowlisted, read-only binary directly
  (`exec.Command(binary, args...)`, never a shell) — `git`, `go`, `rg`,
  `grep`, `find`, `cat`, `ls`, `wc` by default.
- **`web_search`** — queries a local [SearXNG](https://docs.searxng.org)
  instance for things not in the codebase (library docs, error
  messages, current facts). `internal/searxng` owns a Docker container
  (`armin-searxng`, `searxng/searxng` image) started lazily on the
  first call and stopped on exit — but only if armin itself started
  it, never a container you already had running. Results are capped
  (5 by default, 10 max) and each snippet is truncated to ~200 chars,
  so one search can't dominate a small local model's context budget.

The other 29, grouped by category — full argument lists are in
`agent.ToolDefinitions()`:

**Repo stats** (`internal/tools/repostat.go`)

| Tool | What it does |
|---|---|
| `count_lines` | Recursive line count, total + per top-level directory, in one call — no shell pipes needed. |
| `list_languages` | File/line counts by extension, ranked. |
| `find_files` | Glob match on filenames (e.g. `*_test.go`) — a name-only complement to `search_code`. |
| `file_stat` | Size/lines/mtime for one file, without reading its content. |
| `find_todo_fixme` | Fixed `search_code` preset for TODO/FIXME/XXX. |
| `env_info` | Go version, OS/arch, whether git/ripgrep/docker are installed. |
| `project_manifest` | Directory tree + first README/manifest found — the same note the REPL auto-loads at session start, re-invocable mid-conversation. |

**Symbol navigation** (`internal/tools/symbols.go`, ctags-backed)

| Tool | What it does |
|---|---|
| `find_definition` | Where a symbol is defined — the same ctags fact `textDocument/definition` uses. |
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
| `commit_stats` | Churn/hotspot ranking — most-frequently-changed files. |

**Build/test** (`internal/tools/build.go`)

| Tool | What it does |
|---|---|
| `run_tests` | `go test`, parsed into pass/fail + summary. |
| `build_check` | `go build` (or `go vet`), parsed into pass/fail + errors. |
| `lint_check` | `golangci-lint` if installed; reports unavailable, not an error, if not. |

**Dependencies** (`internal/tools/deps.go`)

| Tool | What it does |
|---|---|
| `list_dependencies` | Parses whichever manifest exists — go.mod, package.json, requirements.txt, Cargo.toml, pyproject.toml. |
| `module_graph` | Which of this Go module's own packages import which, via `go list -json` (no `golang.org/x/tools` dependency). |

**Documentation** (`internal/tools/doccomment.go`)

| Tool | What it does |
|---|---|
| `read_doc_comment` | Just a symbol's doc comment, without reading the whole file. |

**Call graph** (`internal/tools/callgraph.go`, Go-only, syntactic — not type-checked)

| Tool | What it does |
|---|---|
| `find_callers` | Syntactic call sites of a function/method name. |
| `find_dead_code` | Exported functions/methods with zero syntactic call sites — a signal, not proof (see the tool's own doc comment for a confirmed false-positive source: ctags can't tell a struct field from a method). |
| `impact_analysis` | BFS outward to a bounded depth — what might need re-checking if a function changes. |

**Semantic search** (`internal/tools/semantic.go`)

| Tool | What it does |
|---|---|
| `semantic_search` | Keyword/term-frequency ranking — **not** real embeddings (armin takes on no vector/embedding dependency, same stance as `web_search`'s SearXNG choice). Useful when you don't know the exact string to grep for. |

## Safety model

Enforced in code, not left to model judgment:

- **Path jail** — every path resolves to an absolute, symlink-resolved
  path and must stay under `-root`.
- **Binary allowlist** — `run_command` only execs names explicitly
  allowed; extend it via config, never by letting the model ask.
- **No shell** — arguments are always a literal argv array; there's
  nothing for `;`, `|`, `&&`, `$()` etc. to be interpreted by.
- **Timeout + output cap** — every subprocess call is bounded and
  truncated, with a clear truncation message telling the model how to
  narrow its query.
- **`-dry-run`** — see exactly what `run_command` would execute before
  trusting it to actually run.

## Config file

`~/.config/armin/config.conf` (or `$XDG_CONFIG_HOME/armin/config.conf`):

```
# key = value, "#" comments, nothing fancier
model      = qwen3:4b
ollama_url = http://localhost:11434
timeout    = 30s
max_output = 5000
allow      = curl, docker
```

`allow` appends to the default binary allowlist; it doesn't replace it.

## Sessions

Every turn is saved to
`~/.local/share/armin/sessions/<project>-<hash>.json`, keyed by the
project root. Pick a conversation back up with:

```sh
./armin -root /path/to/your/project -resume
```

Pass `-no-save` to opt out entirely.

## MCP server mode

```sh
./armin -mcp -root /path/to/your/project
```

Exposes all 34 tools (see "The tools" above) as MCP tools over stdio
— point any MCP client at this command to reuse the same sandboxed
tool layer the REPL uses directly.

## LSP server mode

```sh
./armin -lsp -root /path/to/your/project
```

A minimal Language Server, no diagnostics — this runs *alongside*
your real language server, not instead of it:

- **`textDocument/hover`** — explains the code under your cursor,
  through the same agent loop and tools as the REPL (no streaming;
  LSP hover isn't incremental). Grounded in a ctags fact when the
  cursor sits on a known symbol, instead of the model guessing.
- **`textDocument/definition`** / **`workspace/symbol`** — answered
  entirely from a [Universal Ctags](https://ctags.io) index, no LLM
  involved, same as any compiled-index language server. Needs a
  `universal-ctags` binary on `PATH` or at `~/.local/bin/universal-ctags`
  (override with `-ctags-bin`); missing it just means these two
  return nothing, everything else still works.
- **`textDocument/inlineCompletion`** — fast, non-agentic fill-in-
  the-middle completion (no tool calls, no chat formatting) via a
  *separate* model set with `-completion-model` (default
  `qwen2.5-coder:1.5b-base`). Use a real FIM-tuned code model here,
  not your chat model — Ollama will reject a non-FIM model outright
  for this, and the tool degrades gracefully (empty completions, one
  stderr warning) if the configured model can't do it.
- **`workspace/executeCommand`** (`armin.ask`) — a free-form
  question about the project, same agent loop as hover.

For Neovim, `editors/nvim/armin.lua` wraps all of this in commands
(`:ArminLsp`, `:ArminAsk`, `:ArminModel`, `:ArminComplete`,
`:ArminSuggest`, `:ArminStop`) using only `vim.lsp` — no plugin, and nothing starts
or fires until you run a command yourself. See
[`docs/nvim.md`](docs/nvim.md).

## Project layout

```
cmd/armin/main.go         entrypoint: flags, config, wiring
internal/
  agent/       tool-calling round trip (send → tool_calls → execute → repeat)
  llm/         Ollama client (chat, streaming, model listing)
  tools/       the tool implementations (pure functions)
  sandbox/     path jail, allowlist, timeouts, truncation, dry-run
  ctags/       symbol index backing find_definition/list_symbols/etc. and -lsp
  searxng/     web_search's local SearXNG container lifecycle
  mcpserver/   MCP stdio adapter around the same tools
  lspserver/   minimal LSP stdio adapter (hover + armin.ask) around the same tools
  config/      ~/.config/armin/config.conf loader
  session/     save/resume conversation history
  convo/       context-usage stats, /compact, /suggest follow-ups
  ui/          terminal styling, spinner, raw-mode ghost-text line editor
  repl/        the interactive loop and slash-command dispatch
editors/nvim/armin.lua    Neovim integration (see docs/nvim.md)
```

See `plan.md` for the original design rationale and phased build plan.
