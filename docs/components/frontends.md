# Frontends

How armin is shown. Package `internal/display` picks one, main's last
step (input → think → display); each frontend is its own package, and
all of them run turns through the same agent loop.

| mode | package | what it is |
|---|---|---|
| `armin` | `repl` | the terminal chat |
| `armin -web 127.0.0.1:7777` | `web` | the chat in a browser, with file previews |
| `armin -lsp` | `lspserver` | nvim: hover, inline completion, a chat window |
| `armin -mcp` | `mcpserver` | the tools alone, for another MCP client |
| `armin -library-sync` | `display/library.go` | index and catalog the library, then exit |

## One armin per project (`share`)

The first armin started in a project **hosts** it: the models and the
conversation. Every armin started there afterwards (a terminal, nvim's
`-lsp`) **joins** over a unix socket instead of loading models of its
own, and a question from any of them shows in all of them.

- The socket lives in `~/.local/share/armin/attach/`, which only you
  can open, named by a hash of the project path.
- The wire is one JSON object per line: joined armins send `Request`s
  (ask, cancel, clear, model, complete), the host sends `Event`s
  (tokens, tool calls, replies) to everyone.
- A hosting frontend implements `share.Host`: the REPL
  (`repl/host.go`) and nvim's server (`lspserver/host.go`) do.

## The terminal (`repl`)

`Run` reads a line, then either:
- runs a **slash command** (`commands.go`, with `sessions.go`,
  `notes.go`, `models.go`, `library.go`, `effort.go` for the groups),
  or
- runs a **turn** (`turn.go`), which:
  - settles effort and kind of task (auto asks the model);
  - draws the agent loop as it streams (`turnview.go`): thinking
    dimmed, a line per tool call, the answer as Markdown;
  - shows every write and run in full before it happens
    (`proposal.go`) and asks for approval;
  - keeps an input bar open, so what you type reaches the model at its
    next step.

## The browser (`web`)

A local HTTP server (loopback only) serving one embedded page.
`handlers.go` is the API: `chat` streams a turn as server-sent events,
`approve` answers a pending write, `raw` previews project files (never
as HTML). It refuses requests from other sites and DNS-rebinding
hosts.

## nvim (`lspserver`)

A minimal LSP server, so nvim's built-in client is all it needs (see
`docs/nvim.md`):
- **hover**: a read-only agent answer about the code under the cursor,
  cached;
- **completion**: fill-in-the-middle around the cursor, with nearby
  files as context;
- **definition** and **workspace symbols** from ctags, without a
  model;
- the `armin.*` commands behind the chat window.

## MCP (`mcpserver`)

The agent's tool table over MCP's JSON-RPC: `tools/list` and
`tools/call`. No models load. Writes go straight to disk (the MCP
client approves them); everything else runs through
`agent.Loop.RunTool`.
