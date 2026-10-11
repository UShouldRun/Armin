# armin

A terminal coding assistant, backed by a llama.cpp model (local or on
a remote box), that reads, searches and edits your codebase, runs its
build and tests, and grounds every answer in what's actually there —
not generic training data. Named after the *Attack on
Titan* strategist: the one who reads the terrain before anyone moves.

Four ways to use the same sandboxed tool layer:
- **REPL** — a chat loop in your terminal, talking to a local model.
- **Web UI** (`-web 127.0.0.1:7777`) — the same chat in a browser on
  localhost, with previews for PDFs, Markdown and code — see "Web UI".
- **MCP server** (`-mcp`) — the same tools, exposed over stdio
  JSON-RPC, for any MCP client (Claude Desktop, etc.) to reuse.
- **LSP server** (`-lsp`) — hover-to-explain and an ask command, over
  the Language Server Protocol, for editor integration. One Lua file
  gets you this in Neovim with zero plugins — see
  [`docs/nvim.md`](docs/nvim.md).

## Requirements

- Go 1.23+ to build (the binary itself has no runtime dependencies)
- [llama.cpp](https://github.com/ggml-org/llama.cpp)'s `llama-server`,
  either already running (point armin at it with `-llama-url`) or on
  `PATH` if you want armin to spawn/reap it itself (see "Model
  lifecycle" below) — plus a tool-calling-capable `.gguf` model and,
  for -lsp inline completion, a separate FIM-capable one
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
JSON-RPC servers, the llama-server client, ANSI styling) is
hand-rolled against the standard library.

## Install

```sh
./install.sh
```

Builds the binary and installs it to `~/.local/bin` (override with
`--prefix DIR`), then reports on every dependency above — what it
found, what's missing, and exactly what to do about each one. Writes
a starter `config.conf` (skip with `--no-config`) only if you don't
already have one; never overwrites an existing one. Installs the
bundled skills (see "Memories, skills and the scratchpad") the same
way: a skill already in `~/.config/armin/skills/` is left alone,
since you may have edited it — `--update-skills` replaces them,
`--no-skills` skips them. Safe to re-run
any time, e.g. after `git pull`, to rebuild and reinstall.

Prefer to do it by hand instead?

```sh
go build -o armin ./cmd/armin
```

`install.sh` does nothing beyond that build, a copy onto `PATH`, and
the dependency checks above — there's no hidden step you'd be
skipping by not using it.

## Quickstart

```sh
armin .
```

A bare path is shorthand for `-root` (`armin .` and
`armin -root .` are the same thing, and it can go anywhere among your
other flags — `armin . -lsp` and `armin -lsp .` are equivalent too),
mirroring how tools like `code .` or `vim .` take a path. Omit it
entirely and `-root` defaults to `.` anyway.

```
armin — terminal codebase assistant
  root: /path/to/your/project
  model: chat (http://localhost:8080)
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
| `/model [name]` | show the active model + every configured one, or switch |
| `/context`, `/ctx` | message count, tool-call count, rough token estimate |
| `/compact [n]` | ask the model to summarize everything but the last `n` messages, to free up context |
| `/tools` | list the tools available to the model |
| `/root` | show the sandbox's project root, timeout, allowlist, dry-run state |
| `/history` | dump the raw conversation history |
| `/add-dir <dir>` | let armin read a directory outside the project (read-only); with no argument, list them |
| `/sessions [rm <id>]`, `/resume <id>`, `/title <text>` | list, delete, switch or rename sessions — see "Sessions" |
| `/save` | force a session save right now |
| `/suggest` | toggle 2-3 numbered follow-up questions after each answer — type a number to ask one instead of writing it out |
| `/ghosttext` | toggle live ghost-text completion while you type (off by default — see below) |
| `/thinking` | toggle showing the model's thinking (dimmed) as it streams |
| `/approve` | toggle asking before each file write |
| `/memory [show\|rm <name>]` | list, show or delete memories |
| `/remember <text>` | ask the model to save something to memory |
| `/skills` | list skills |
| `/skill <name> [task]`, `/<name> [task]` | run a skill |
| `/scratch [show <name>\|clear]` | list, show or clear the scratchpad |

Switching to a model with no tool-calling support (a plain
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
| `-root` | `.` | project root; every path argument is jailed to it (a bare positional argument, e.g. `armin .`, is shorthand for this — see Quickstart) |
| `-model` | `chat` | name of the model to use for chat — a `models.<name>` entry from `config.conf`, or (with no matching entry) an ad-hoc label for whatever's running at `-llama-url` |
| `-completion-model` | same as `-model` | name of the model for `-lsp` inline completion; should be a fill-in-the-middle code model, not a chat model |
| `-llama-url` | `http://localhost:8080` | llama-server base URL used for `-model` when it has no matching `models.<name>` entry |
| `-completion-llama-url` | (reuses `-llama-url`) | same, for `-completion-model` |
| `-timeout` | `20s` | per-subprocess timeout |
| `-max-output` | `4000` | bytes of tool output before truncation |
| `-max-rounds` | `50` | tool-call rounds per turn before the model must stop and report |
| `-yes` | off | apply file writes without asking |
| `-add-dir` | — | comma-separated directories outside the project armin may read (never write), e.g. `-add-dir ..` |
| `-web` | off | serve the chat in a browser at this localhost address, e.g. `127.0.0.1:7777` |
| `-dry-run` | off | `run_command` describes what it would execute instead of running it |
| `-mcp` | off | serve the tools over MCP stdio instead of starting the REPL |
| `-lsp` | off | serve hover + an ask command over LSP stdio instead of starting the REPL |
| `-resume [id]` | off | resume a session: alone, pick from a menu; with an id (or prefix), that one |
| `-no-save` | off | don't persist the session to disk after each turn |
| `-no-color` | off | disable ANSI styling |

Flag defaults can also come from a config file (see below); explicit
flags always win.

## The tools

The model has 37 narrow, structured tools — never a free-form shell
string — plus the memory, scratchpad and skill tools described in
"Memories, skills and the scratchpad" below. Editing:

- **`edit_file`** — exact-string replacement. `old_string` must
  match exactly once (or set `replace_all`); a miss points at the
  line where the text appears with different whitespace.
- **`write_file`** — create a file, or overwrite one already read.
- **`read_pdf`** — the text of a PDF a page range at a time (10 by
  default), with `--- page N ---` markers, via poppler's `pdftotext`
  (`sudo apt install poppler-utils` if it's missing).

Both refuse to touch a file the model hasn't read this session, or
one that changed on disk since it was read. See "Editing files".

The original five:

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

### Files outside the project

Every path is jailed to the project root. To let armin read
something next to it — slides in the parent folder, a reference
repo — grant it explicitly:

```sh
armin . -add-dir ..          # at startup
/add-dir ~/Documents/papers  # or in the REPL
```

Added directories are **read-only**: read tools (`read_file`,
`read_pdf`, `search_code`, `list_directory`) accept absolute paths
inside them, and every write tool still refuses anything outside
the project. armin tells the model what it can now read and lists
the directory, so "read the slides in the parent folder" works.

### Web search engines

`web_search` takes a `topic`: `general` (Yandex, Bing, Wikipedia),
`code` (those plus Stack Overflow and GitHub) or `science` (arXiv,
Semantic Scholar, OpenAlex, plus general). The engine list is
armin's own, chosen by testing: SearXNG's defaults (DuckDuckGo,
Startpage, Brave, Qwant…) answer a home connection with CAPTCHAs and
bans. Results that share no word with the query — Bing's habit of
answering an unfamiliar query with popular local pages — are
dropped. When engines fail and nothing comes back, the tool says
which engines failed, and the model is told to report it instead of
answering from memory. armin's generated `settings.yml` is versioned
and upgraded automatically (keeping its secret key); delete its
first line to keep your own edits.

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
  trusting it to actually run; file writes are previewed, not
  written.
- **Writes** — jailed like reads, refused inside `.git/`, atomic
  (temp file + rename), only on files read this session and unchanged
  since, and shown as a diff for approval first (unless `-yes`).
  `-lsp` is read-only.

## Config file

`~/.config/armin/config.conf` (or `$XDG_CONFIG_HOME/armin/config.conf`):

```
# key = value, "#" comments, nothing fancier
model      = chat
llama_url  = http://localhost:8080
timeout    = 30s
max_output = 5000
allow      = curl, docker

# Everything above is the zero-config path: -model's name ("chat")
# has no models.<name> block below, so it falls back to whatever's
# already running at llama_url. Add a models.<name> block per name
# you want armin itself to spawn and reap (see "Model lifecycle").
models.coder.binary      = llama-server
models.coder.model_path  = /home/you/models/qwen2.5-coder-7b-q4.gguf
models.coder.port        = 8081
models.coder.idle_timeout = 20m
models.coder.args        = --ctx-size 8192 --n-gpu-layers 999
```

`allow` appends to the default binary allowlist; it doesn't replace it.

Every llama-server armin starts, local or remote, gets lean
defaults that spend memory on context instead of features a
single-user, text-only agent doesn't use. Each applies only if your
`args` don't set that flag themselves:

| Default | Why |
|---|---|
| `--no-mmproj` | never load a vision encoder (~0.9 GB for a 27B Qwen) |
| `-np 1` | one slot; the server's auto default is 4 |
| `-fa on` | flash attention: less scratch memory, needed for a quantized V cache |
| `-ctk q8_0 -ctv q8_0` | KV cache at half the size of f16, negligible quality loss |
| `-cram 1024` | host-RAM prompt cache at 1 GiB instead of 8 |

`models.<name>.vision = true` keeps the vision encoder loadable;
`models.<name>.lean = false` drops all of these. Speculative-decoding
drafts (MTP, EAGLE) are separate files llama.cpp only loads when
asked, and armin never asks.

Agent keys (all optional):

```
max_tool_rounds = 50     # rounds per turn before the model must report
auto_approve    = false  # true = never ask before file writes
context_size    = 65536  # tokens; default asks llama-server's /props
keep_turns      = 2      # recent turns that keep full tool results
read_timeout    = 5m     # give up after this long with no bytes
build_timeout   = 10m    # run_tests / build_check / lint_check
test_cmd        = go test -race ./...
build_cmd       = make
lint_cmd        = golangci-lint run
```

## Model lifecycle

Unlike Ollama (a single daemon juggling many pulled models), a
`llama-server` process serves exactly one already-loaded model — so
`/model <name>` (or `:ArminModel`/`armin.model` over LSP) means
"point at a different URL", not "ask the same daemon for a different
model". Two kinds of `models.<name>` entry:

- **External** (`models.<name>.url = ...`) — something you already
  run yourself, e.g. on a beefy always-on box. armin only ever talks
  to it; it never starts or stops that process.
- **Managed** (`models.<name>.binary`/`.model_path`/`.port`, `.args`
  optional) — armin spawns `llama-server` itself on first use,
  health-polls it before handing back a client, and — mirroring
  `internal/searxng`'s Docker container lifecycle — stops only what
  it started. A managed model left idle (no in-flight request) past
  its `idle_timeout` (default 20m; a negative value disables this) is
  stopped by a background reaper and respawned lazily next time it's
  used, the same tradeoff Ollama's own default `keep_alive` unload
  already makes.

`-model`/`-completion-model` with no matching `models.<name>` entry
fall back to an external, unmanaged spec at `-llama-url`/
`-completion-llama-url` — the zero-config path, for "I already
started a `llama-server`, just talk to it".

### Authenticated / remote models (e.g. Claude)

An external `models.<name>.url` isn't limited to a `llama-server` you
run yourself — any OpenAI-compatible `/v1/chat/completions` endpoint
works, including
[Anthropic's own](https://docs.anthropic.com/en/api/openai-sdk), so
armin can run entirely against Claude instead of a local model. The
only thing a remote endpoint needs that a local `llama-server`
doesn't is auth, sent as `Authorization: Bearer <key>` on every
request when `models.<name>.api_key_env` is set:

```
# ~/.config/armin/.env — "KEY=value", "#" comments, nothing fancier.
# A real exported environment variable always wins over this file.
ANTHROPIC_API_KEY = sk-ant-...
```

```
# ~/.config/armin/config.conf
model = claude
models.claude.url         = https://api.anthropic.com/v1
models.claude.api_key_env = ANTHROPIC_API_KEY
```

`api_key_env` names an environment variable — populated from `.env`
above if it isn't already set some other way — never the key itself.
Keep it out of `config.conf`: that file is plain text people back up,
sync, and paste into issues without thinking twice; `.env` (next to
it, same `~/.config/armin` directory) is where a secret belongs.

Two things armin does automatically so that secret doesn't spread
further than it has to:

- **`.env` must be readable only by you.** `config.LoadEnv` refuses
  to read a group- or other-readable `.env` at all (a wrong umask
  leaving it world-readable is exactly the kind of mistake that
  should fail loud, not quietly work) — `./install.sh` creates it at
  mode 600 from the start and re-tightens an existing one on every
  run, so this shouldn't come up in practice.
- **A key configured for one model never reaches another model's
  process.** Spawning a child process inherits the *entire*
  environment by default — so without this, a managed local
  `llama-server` would see `ANTHROPIC_API_KEY` too, despite having
  nothing to do with Claude. `internal/llamacpp.Manager` strips every
  name any `models.<name>.api_key_env` names from a spawned
  process's environment, regardless of which model that name was
  configured for.

## Editing files

Before every `write_file` / `edit_file`, the REPL prints the change
as a colored diff and asks:

```
  → edit_file(path=internal/ui/ui.go, old_string=…, new_string=…)
  internal/ui/ui.go @ line 98
    const maxValue = 60
  - parts := make([]string, 0)
  + parts := make([]string, 0, len(args))
apply edit_file? [y]es / [n]o / [a]ll this session / or type feedback:
```

Typing anything other than y/n/a declines the change and sends what
you typed to the model as feedback. `a` (or `-yes`, `auto_approve =
true`, `/approve`) stops asking. Ctrl-C stops the current turn
without leaving armin.

## Thinking models

A thinking model's reasoning (llama-server's `reasoning_content`;
`--jinja` and the default `--reasoning-format` produce it) streams
dimmed above the answer; `/thinking` hides it. It's sent back to the
model within a turn, so the chain of thought survives across tool
calls, and dropped when the turn ends — the same thing the chat
template does anyway, so the prompt cache isn't disturbed.

Requests use a read-idle timeout instead of a whole-request one:
a generation that keeps streaming is never cut off, and a server
that goes silent for `read_timeout` (default 5m, long enough for a
large prompt to process on a remote box) is.

## Context management

The window is spent on three things: the prompt, the history, and
room for the model to think and answer. Before every model call,
armin decides (`agent.Decide`, a pure function with its own tests)
what keeps the next request inside the window most cheaply, and
prints a `context:` line in the REPL whenever it does something:

| Context used | What armin does |
|---|---|
| under 50% of the window | nothing; the prompt prefix stays byte-stable, so llama-server's KV cache keeps hitting |
| 50–75% | **trims** tool results older than `keep_turns` turns. Each is **archived to the scratchpad** first, and the stub left in context says exactly where (`scratchpad_read name=archive-1 start_line=… end_line=…`), so the model re-reads instead of re-running a slow or non-deterministic tool. Loaded skills and memories are kept. |
| over 75%, mostly the **current turn's** work | **grows** the window to the streaming tier (below), if the model has one. Summarizing would discard exactly what the model is working with, so armin pays one reload and re-prefill instead. |
| over 75%, mostly **older history** | **saves, then compacts.** The model gets a short turn with only `scratchpad_write`, `scratchpad_read` and `memory_save` to write down its plan, findings and durable facts; then older history is summarized into a brief that points at those files. The latest request is carried over verbatim, and the cut never separates a tool call from its result. |
| after a compaction, in the streaming tier, with usage under half the resident window | **shrinks** back to the fast tier; compaction already invalidated the prefix cache, so the switch costs almost nothing extra |

Thinking from finished turns leaves the context too, but isn't lost:
it's appended to the scratchpad file `thinking`, labeled with the
date and the request, for the model to re-read.

The window size comes from llama-server's `/props`, `context_size`
in config, or the streaming tier's settings. `/context` shows the
server's real token count, how full the window is, and which tier is
active.

### Streaming tier: more context than VRAM holds

A model can have a second, larger tier served by a llama-server
build with adaptive KV streaming
([Raymond Huang's fork](https://github.com/RaymondHuang210129/llama.cpp-adaptive-kv-streaming)).
It keeps a resident pool and a ring buffer in VRAM and the rest of
the KV cache in pinned RAM, prefetching it over PCIe while attention
runs:

```
models.local.args             = --jinja -ngl 999 -c 98304
models.local.stream_binary    = /path/to/kvstream/build/bin/llama-server
models.local.stream_context   = 196608
models.local.stream_arena_mib = 2048
models.local.stream_args      = --fit off -b 256 -ub 256
```

The resident tier's window comes from `-c` in `args` and must be
set. Moving between tiers restarts llama-server on the same port,
re-prefilling the conversation. Decoding slows as more of the cache
has to stream (PCIe bandwidth is the limit), which is why armin only
grows when the current turn needs it, and comes back as soon as a
compaction makes room.

## Memories, skills and the scratchpad

Three kinds of notes, all plain Markdown on disk; only short indexes
and pointers ever sit in the conversation.

**Memories** are durable facts for future sessions: your
preferences, project conventions, decisions and why. The model saves
them with `memory_save` (or ask it with `/remember <text>`), each
with a one-line description. Every session's system prompt lists
names and descriptions; `memory_read` loads one.

- project scope: `~/.local/share/armin/memory/<project>-<hash>/`
- user scope (every project): `~/.local/share/armin/memory/user/`

A checked-in `ARMIN.md`, `AGENTS.md` or `CLAUDE.md` at the project
root (first found) is loaded into the system prompt as project
instructions.

**Skills** are instructions loaded only when a task needs them: a
directory with a `SKILL.md` and any supporting files.

```
~/.config/armin/skills/<name>/SKILL.md     every project
<root>/.armin/skills/<name>/SKILL.md       this project (wins on a name clash)
```

```markdown
---
name: release
description: Cut a release — changelog, version bump, tag
---
1. Read CHANGELOG.md and summarize commits since the last tag.
2. ...
```

Bundled skills, in `skills/` and installed by `./install.sh`:

| Skill | For |
|---|---|
| `go-style` | Go: 2-space, spaced operators/braces, aligned columns, wrapped errors, bitmask flags, allocation-aware code |
| `react-style` | JSX/TSX/HTML: spaced and aligned attribute `=`, padded `{ expr }` |
| `js-style` | JS/TS: aligned `=` / `from` / object values, padded `${ x }`, try/catch layout |
| `sql-style` | lowercase SQL, aligned types, `in_`/`v_` prefixes, lean id-based queries |
| `typst` | Typst CVs, letters and reports, with `cv.typ` / `letter.typ` templates |
| `c-style` | C: `lib/<name>/{include,src}` with `_private.h`, `u8..u64`, `__module_` internals, aligned prototypes, arenas |
| `cpp-style` | C++ kept as close to C as possible: header-only + `.inl`, templates only for compile-time widths, HLS rules |
| `commit-message` | `vX.Y.Z - summary` subjects with semver bumps, no AI attribution |
| `code-review` | bugs first, then security, performance, tests and style; ranked findings with fixes |
| `research-paper` | paper structure, claims backed by numbers, related work via `web_search` |
| `pytorch-experiments` | reproducible runs: seeds, folds, baselines, logged metrics |
| `riscv-asm` | RV32IM encodings, calling convention, custom-opcode space |
| `self-hosting` | Docker Compose, Cloudflare Tunnel, JWT, backups and restores |
| `git` | safe git: inspect first, branches, conflicts, undo by least-destructive step, never force or reset without asking |
| `bash` | strict-mode, quoted, shellcheck-clean scripts; arrays, traps, argument parsing |
| `zsh` | zsh vs bash pitfalls (no word splitting, 1-based arrays, glob errors), `.zshrc` editing |
| `linux` | diagnosing processes, disk, memory, GPU, services, logs, network; asks before system-wide changes |

The system prompt lists each skill's name and description; the
model loads one with `use_skill` (and its files with `use_skill`
+ `file`). `/skill release 1.4.0` or just `/release 1.4.0` runs one
directly, with its instructions inlined.

**The scratchpad** is per-project working memory outside the repo
(`~/.local/share/armin/scratch/<project>-<hash>/`). The model writes
its plan, findings and long reasoning there with `scratchpad_write`,
keeps only the file names in mind, and re-reads with
`scratchpad_read` — so a long task doesn't need everything in
context at once. Compaction summaries point at these files.
`/scratch` lists them, `/scratch clear` empties it.

## A model on a cloud machine armin starts and stops

Give armin cloud credentials and a machine id — an AWS EC2 instance,
a STACKIT server or a Scaleway instance — and it runs the whole
lifecycle: on first use
it starts the machine (if it's off), waits for it to boot, launches
llama-server on it over SSH with a tunnel to your machine, and when
armin exits it stops the machine again. Nothing listens on a public port — llama-server binds to the
instance's loopback and the traffic goes through SSH.

```
# ~/.config/armin/.env (mode 600)
AWS_ACCESS_KEY_ID     = AKIA...
AWS_SECRET_ACCESS_KEY = ...
```

```
# ~/.config/armin/config.conf
model = qwen
models.qwen.aws_instance = i-0123456789abcdef0
models.qwen.aws_region   = eu-west-1
models.qwen.ssh_key      = ~/.ssh/gpu-box.pem
models.qwen.binary       = /home/ubuntu/llama.cpp/build/bin/llama-server
models.qwen.model_path   = /home/ubuntu/models/qwen-27b-q4_k_m.gguf
models.qwen.args         = --jinja -ngl 999 -c 65536
models.qwen.idle_timeout = 30m
```

| Key | Default | |
|---|---|---|
| `aws_instance` | — | the EC2 instance to use; makes the model AWS-hosted |
| `aws_region` | `$AWS_REGION` | |
| `stackit_server` | — | the STACKIT server id; makes the model STACKIT-hosted |
| `stackit_project` | `$STACKIT_PROJECT_ID` | |
| `stackit_region` | `eu01` | |
| `scaleway_server` | — | the Scaleway instance id; makes the model Scaleway-hosted |
| `scaleway_zone` | `$SCW_DEFAULT_ZONE`, else `fr-par-2` | |
| `ssh_user` | `ubuntu` (`root` on Scaleway) | |
| `ssh_key` | ssh's own defaults | private key for the instance |
| `binary`, `model_path`, `args` | `llama-server` | paths on the instance |
| `remote_port` | `8080` | llama-server's port on the instance |
| `port` | `18180` | local end of the tunnel |
| `idle_timeout` | `20m` | stop the instance after this long unused (it starts again on the next request) |
| `stop_on_disconnect` | `true` | see below (`aws_stop_on_disconnect` also works) |

What to know:

- **Stops only what it started.** An instance that was already
  running when armin got to it is used and left running.
- **Dead man's switch.** When armin started the instance, the remote
  side runs llama-server under `trap 'sudo -n shutdown -h now' HUP`:
  if the SSH session drops — armin killed with `-9`, laptop asleep,
  network gone for about a minute — the instance shuts itself down
  instead of billing. armin restarts it on the next request. Needs
  passwordless `sudo` for `ssh_user` (the default on Ubuntu AMIs);
  turn it off with `stop_on_disconnect = false`.
- **IAM.** The keys need only `ec2:DescribeInstances`,
  `ec2:StartInstances` and `ec2:StopInstances`, ideally scoped to
  that one instance. They never reach ssh or llama-server.
- **Host keys** are pinned per machine id (a stopped machine comes
  back with a new public IP) in
  `~/.local/share/armin/remote_known_hosts`.
- **First start is slow.** Boot plus loading a large model from a
  cold EBS volume can take several minutes; progress is shown as it
  goes. The instance needs a public IP (or a VPN to its private one)
  and SSH open to you in its security group.
- Stopping an instance stops compute billing, not storage: the EBS
  volume holding the model is still charged.

### STACKIT

STACKIT is Schwarz Gruppe's cloud, run from German data centers.
armin authenticates with a service account, using the same variables
as the STACKIT CLI and SDK:

```
# ~/.config/armin/.env (mode 600)
STACKIT_SERVICE_ACCOUNT_KEY_PATH = ~/.config/armin/stackit-sa.json
# or, if the key file has no private key inside:
# STACKIT_PRIVATE_KEY_PATH = ~/.config/armin/stackit-private.pem
# or a static token instead of the key flow:
# STACKIT_SERVICE_ACCOUNT_TOKEN = ...
```

```
# ~/.config/armin/config.conf
model = qwen
models.qwen.stackit_server  = 0b8a…   # server id
models.qwen.stackit_project = 9f1c…   # project id
models.qwen.ssh_key         = ~/.ssh/stackit.pem
models.qwen.model_path      = /home/ubuntu/models/qwen-27b-q6_k.gguf
models.qwen.args            = --jinja -ngl 999 -c 262144
```

With the key flow, armin signs an RS512 JWT with the service
account's key, exchanges it for a short-lived access token, and
caches the token until shortly before it expires. The service
account needs permission to read, start and deallocate the server.

armin **deallocates** the server instead of stopping it. A stopped
STACKIT server stays on its hypervisor and is billed at full price.
A deallocated one is billed only for its volume, and starting it
allocates it again. A GPU flavor can be out of capacity at the moment
armin tries to reallocate it; in that case the start fails with
STACKIT's error, and retrying later works.

### Scaleway

```
# ~/.config/armin/.env (mode 600)
SCW_SECRET_KEY = ...
```

```
# ~/.config/armin/config.conf
model = qwen
models.qwen.scaleway_server = 5f2e…        # instance id
models.qwen.scaleway_zone   = fr-par-2     # where the L40S runs
models.qwen.ssh_key         = ~/.ssh/scaleway
models.qwen.model_path      = /root/models/qwen-27b-q8_0.gguf
models.qwen.args            = --jinja -ngl 999 -c 262144 -fa on -ctk q8_0 -ctv q8_0
```

The secret key goes in an `X-Auth-Token` header, and that's the
whole authentication. armin uses `poweroff`, which releases the
instance's slot on its hypervisor and stops compute billing, never
`stop_in_place`, which keeps the slot reserved. Scaleway bills GPU
instances per minute of uptime, though its general billing FAQ also
mentions a 60-minute minimum per resource, so check before relying
on short sessions. Storage and reserved IPs bill while powered off.

## Running a large model on a remote box

For example a ~27B Qwen on a GPU instance, armin on your laptop:

```sh
# on the GPU box: --jinja for native tool calls and reasoning_content
llama-server -m qwen-27b-q4_k_m.gguf --jinja -ngl 999 \
  -c 65536 --host 127.0.0.1 --port 8080 --api-key "$KEY"

# on your laptop: tunnel instead of exposing the port
ssh -N -L 8080:127.0.0.1:8080 gpu-box
```

```
# ~/.config/armin/config.conf
model = qwen
models.qwen.url         = http://127.0.0.1:8080
models.qwen.api_key_env = QWEN_KEY
max_output = 16000
```

`context_size` is picked up from the server. Raise `max_output` from
its default of 4000 bytes: that default suits small local models,
and a 27B model with a 64k window can take bigger tool results. Use
the sampling settings the model card recommends as llama-server
flags (`--temp`, `--top-p`, `--top-k`, `--min-p`).

## Web UI

```sh
armin . -web 127.0.0.1:7777
```

The same agent loop, tools, memories, skills, scratchpad and context
management as the REPL, in a browser, for discussions you'd rather
not have in a terminal:

- **Files** on the left (filter as you type); click one to preview it
  on the right: PDFs in the browser's own viewer, Markdown rendered,
  code with line numbers, images inline.
- **Answers** render as Markdown. A `file:line` in an answer is a
  link: click it and the preview opens that file at that line.
  Web-search answers carry their numbered Sources.
- **Thinking** streams into a collapsed "thinking" block; tool calls
  and `context:` notices appear as they happen.
- **File writes** show their diff with Apply / Decline and a box to
  tell the model what to do instead (`-yes` skips this).
- **Header**: model picker, a context meter (with the tier when the
  model has a streaming one), `compact` and `clear`. "Ask about this
  file" puts the previewed file into your message.

It's one HTML file with inline CSS and plain JavaScript, embedded in
the binary — no build step, no libraries, nothing fetched from the
internet. It only binds to a loopback address, and rejects requests
whose `Host` or `Origin` isn't local, so another site open in your
browser can't drive it. Previewed project files are served as plain
text (never as HTML on armin's origin). The browser session lives in
memory; it isn't saved like REPL sessions are.

## Sessions

Every conversation is saved as its own session: a file named by a
UUID under `~/.local/share/armin/sessions/`, whose first line holds
the project directory, the model, a short title the model writes
after the first turn, and timestamps. Messages are appended as they
happen, never rewritten.

```sh
armin -resume                 # menu: directory, title, id, age, size
armin -resume 999cd437        # by id, or any unique prefix of it
armin -resume=999cd437        # same
```

The menu lists sessions newest first, with this directory's marked
`*`; type a number or an id, or Enter for a new session. Resuming
by id works from anywhere: armin opens in that session's directory.
On exit armin prints `resume with: armin -resume <id>`.

In the REPL:

| Command | Does |
|---|---|
| `/sessions` | list saved sessions (`→` this one, `*` this directory) |
| `/sessions rm <id>` | delete a session and its scratchpad, after asking |
| `/resume <id>` | switch to another session of this directory |
| `/title <text>` | rename this session |

Each session has its **own scratchpad** (`scratch/<project>/<id>/`):
its plan, archived tool results and thinking notes don't mix with
another session's. **Memories** are shared by every session of the
project (and user memories by every project) — durable facts should
carry over; `/memory rm <name>` deletes one. Pass `-no-save` to keep
a session off disk. Sessions saved before ids existed (one file per
project) still list and resume.

## MCP server mode

```sh
./armin -mcp -root /path/to/your/project
```

Exposes all 37 tools (see "The tools" above) as MCP tools over stdio
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
  *separate* `llm.Client`, pointed at whatever `-completion-model`
  resolves to (defaults to `-model` itself, i.e. one llama-server for
  both), talking to llama-server's native `/infill` endpoint. Use a
  real FIM-tuned code model here, not your chat model — the tool
  degrades gracefully (empty completions, one stderr warning) if the
  configured model can't do it.
- **`workspace/executeCommand`** (`armin.ask`) — a free-form
  question about the project, same agent loop as hover.

For Neovim, `editors/nvim/armin.lua` wraps all of this in commands
(`:ArminLsp`, `:ArminAsk`, `:ArminModel`, `:ArminComplete`,
`:ArminSuggest`, `:ArminStop`) using only `vim.lsp` — no plugin, and nothing starts
or fires until you run a command yourself. See
[`docs/nvim.md`](docs/nvim.md).

## Project layout

```
install.sh                  builds, installs to PATH, checks dependencies, writes a starter config
cmd/armin/main.go           entrypoint: flags, config, wiring
internal/
  agent/       tool-calling round trip, approvals, context trimming/compaction
  llm/         llama-server HTTP client (chat + tool calling, streaming, infill completion)
  llamacpp/    spawns/health-polls/idle-reaps managed llama-server processes, hands out llm.Clients
  tools/       the tool implementations (pure functions)
  sandbox/     path jail, allowlist, timeouts, truncation, dry-run
  ctags/       symbol index backing find_definition/list_symbols/etc. and -lsp
  searxng/     web_search's local SearXNG container lifecycle
  mcpserver/   MCP stdio adapter around the same tools
  lspserver/   minimal LSP stdio adapter (hover + armin.ask) around the same tools
  notes/       memories, skills, scratchpad, project instruction file
  config/      ~/.config/armin/config.conf loader
  session/     save/resume conversation history
  convo/       context-usage stats, /compact, /suggest follow-ups
  ui/          terminal styling, spinner, raw-mode ghost-text line editor
  web/         localhost web UI: one embedded HTML page + JSON/SSE API
  repl/        the interactive loop and slash-command dispatch
editors/nvim/armin.lua    Neovim integration (see docs/nvim.md)
skills/                   bundled skills, installed by install.sh
```

See `plan.md` for the original design rationale and phased build plan.
