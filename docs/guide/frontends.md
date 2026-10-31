# Web UI, MCP and LSP

## Web UI

```sh
armin . -web 127.0.0.1:7777
```

The same agent loop, tools, memories, skills, scratchpad and context
management as the REPL, in a browser, for discussions you'd rather
not have in a terminal:

- **Files** on the left (filter as you type); click one to preview it
  on the right: PDFs in the browser's own viewer, Markdown rendered,
  code with line numbers and syntax highlighting (Go, JS/TS, Python,
  C/C++, Rust, Java, Lua, shell, SQL, Typst, JSON — a small built-in
  highlighter, no library), images inline. The viewer opens with a
  file and closes with `×` or the **viewer** button; the browser
  remembers which.
- **Answers** render as Markdown. A `file:line` in an answer is a
  link: click it and the preview opens that file at that line.
  Web-search answers carry their numbered Sources.
- **Thinking** streams into a collapsed "thinking" block; tool calls
  and `context:` notices appear as they happen.
- **Status bar** while a turn runs: the phase (waiting for the
  model, reading the prompt with its progress, thinking, writing,
  running a command, waiting for your approval), how long it's
  taken, tokens so far, a warning when the model has said nothing
  for 20 s, and "the connection may be lost" when armin's 5 s
  heartbeat stops. The REPL's spinner shows the same.
- **Approvals**: a file write opens in the viewer as the whole file
  with the change in place (removed lines red, new ones green,
  scrolled to it), with Apply in the chat; `run_program`
  the command (Run), reads outside the project the folder (Grant),
  each with Decline and a box to tell the model what to do instead
  (`-yes` skips writes and runs, never access).
- **The file list** reloads after the model writes a file or runs a
  program, at the end of each turn, and when you return to the tab;
  an open preview of a changed file reloads too.
- **Header**: model picker, a context meter (with the tier when the
  model has a streaming one), `compact` and `clear`. "Ask about this
  file" puts the previewed file into your message.

It's one HTML file with inline CSS and plain JavaScript, embedded in
the binary — no build step, no libraries, nothing fetched from the
internet. It only binds to a loopback address, and rejects requests
whose `Host` or `Origin` isn't local, so another site open in your
browser can't drive it. Previewed project files are served as plain
text (never as HTML on armin's origin).

The browser conversation is a session like a REPL one: saved after
every turn, named by the model, listed by `armin -resume`, and
resumable in either front end. `armin -resume <id> -web …` opens it
in the browser with the earlier messages shown; `clear` starts a new
session. `-add-dir` works here too.

## MCP server mode

```sh
./armin -mcp -root /path/to/your/project
```

Exposes all 37 tools (see [tools.md](tools.md)) as MCP tools over stdio
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
[`docs/nvim.md`](../nvim.md).

