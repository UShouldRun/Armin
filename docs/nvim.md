# Neovim integration

armin can run as a minimal [LSP](https://microsoft.github.io/language-server-protocol/)
server (`-lsp`) instead of the REPL: hover to explain code, jump to
a definition, search symbols by name, get inline completions, and
ask free-form questions — all riding Neovim's built-in `vim.lsp`
client, so there's no plugin to install and nothing new to learn.

This is opt-in and stays that way. Loading `editors/nvim/armin.lua`
registers commands and starts nothing. armin only runs when you call
`:ArminLsp` yourself, and stops when you call `:ArminStop`. Inline
completion only ever fires automatically once you've explicitly
turned it on with `:ArminComplete`. There's no autocmd anywhere in
the file that runs before you ask for one.

Requires Neovim ≥ 0.10.

## Setup

Copy the file into your runtimepath and require it once:

```sh
cp editors/nvim/armin.lua ~/.config/nvim/lua/armin.lua
```

```lua
-- init.lua
require("armin").setup()
```

That's the entire installation — one file, no plugin manager entry
required (though a `dir = "~/Code/armin/editors/nvim"`-style local
spec works too, if you'd rather manage it that way).

Definition and workspace-symbol search need Universal Ctags on
`PATH` as `universal-ctags` (see the main
[README](../README.md#lsp-server-mode) for why the name matters and
how to build it) — without it, those two just return nothing rather
than failing.

## Commands

| Command | Does |
|---|---|
| `:ArminLsp` | attach armin to the current buffer. Root is the nearest ancestor with `.git`, `go.mod`, `package.json`, `pyproject.toml`, or `Cargo.toml`, falling back to cwd. |
| `:ArminAsk [question]` | open (or focus) the chat window, sending `question` right away if given. |
| `:ArminClear` | start the conversation over (same as `/clear` in the chat). |
| `:ArminCancel` | stop the answer in flight (same as `<C-c>` in the chat). |
| `:ArminModel [name]` | with no name, a picker of every `models.<name>` in `config.conf` (current one marked `●`, with its llama-server state); with one, switch to it. `<Tab>` completes names. |
| `:ArminComplete` | toggle ghost-text completion for the current buffer. |
| `:ArminSuggest` | request one completion at the cursor right now. |
| `:ArminStop` | detach, turn off completion everywhere, and stop the armin process (and the models it started). |

`:ArminModel` switches the chat model, the one `:ArminAsk` and hover
use. Without a `completion_model` of its own, inline completion
follows it between local models, so the GPU holds one model at a
time. Switching chat to a cloud model leaves completion on the last
local one: ghost text never bills per keystroke, and hosted APIs
rarely serve `/infill`. Switching back to a local model brings
completion along again.

## Using the chat window

`:ArminAsk` works like the terminal chat. It's one conversation per
armin process, and every question builds on the previous ones.
`<Esc>` (or `q`) only hides the window. Run `:ArminAsk` again and
everything is still there, until `/clear`. The conversation lives in
the armin process, so even a buffer you wiped comes back with its
history.

At the `❯` prompt:

| Key / command | Does |
|---|---|
| `<CR>` | send the question |
| `<C-c>`, `/stop` | stop the answer, whoever asked |
| `<C-f>` | insert an `@path` file mention |
| `/clear` | start the conversation over |
| `/model [name]` | list or switch models |
| `/tips` | list every tip |
| `/help` | list these |
| `<CR>` (normal mode) on a `[[file:line]]` | open that spot in a split |

While armin works, a status line says what it's doing: `◐ Thinking…`,
`◐ Reading lines.go…`, then `✓ … — 34 lines` once a tool returns. A
tip about the commands rotates beside the status line. The window's
bottom border shows how full the context window is.

`setup({ layout = "right" })` docks the chat as a split on the right,
like a terminal pane, instead of a centered float.

### One armin per project

The first armin started in a project is its *instance*: it loads the
model and keeps the conversation. Every armin started there later
joins it over a per-project socket instead of loading models of its
own, in either order:

- **Terminal first, then `:ArminLsp`.** nvim's armin joins the
  terminal's instance. Completion, hover, `:ArminAsk` and
  `:ArminModel` all go to it, so one llama-server serves both.
  Questions from nvim run in the terminal's conversation, show there
  (`nvim:2 asked:`), and any approval they need is asked in the
  terminal.
- **`:ArminLsp` first, then `armin`.** The terminal joins nvim's
  instance and shares the `:ArminAsk` chat. That chat is read-only,
  like `:ArminAsk`.

Both sides say which instance they are: the terminal's banner and
the chat window's title read `Armin local · instance 1`. The same
number means the same conversation. Questions from one side stream
into the other, even while you're typing; the half-typed line comes
back afterwards. `armin -new` starts a separate instance that loads
its own model. If the instance quits, a joined nvim says so; run
`:ArminStop` and `:ArminLsp` to start one there.

In `-lsp` mode armin has the same memories, skills and scratchpad
as the REPL, but no write tools.

When armin flags something worth your attention (a risk, a doubt, a
surprising behavior), it cites a `[[file:line]]` (or
`[[file:start-end]]`). Citations are underlined, and `<CR>` on one
opens or updates a preview split jumped to that line.

## Using hover, definition, and workspace symbols

These are Neovim's own built-ins — once attached, armin just answers
them like any other language server:

```lua
vim.lsp.buf.hover()           -- explain the code under the cursor
vim.lsp.buf.definition()      -- jump to where a symbol is defined
vim.lsp.buf.workspace_symbol() -- fuzzy-search symbols by name
```

If your config already binds `K` to `vim.lsp.buf.hover()` (most do),
it'll work against armin the moment you `:ArminLsp` — no separate
keybinding needed. The first hover on a given line takes a few
seconds (local model + it may search/read files to ground the
answer); repeat hovers on unchanged code are cached and return
instantly. Definition and workspace-symbol are instant either way —
they're a ctags index lookup, no model involved.

## Using inline completion

`:ArminComplete` shows a dim inline suggestion as you type. With
`setup({ tab_accept = true })`:

| Key (insert mode) | Does |
|---|---|
| `<S-Tab>` | accept the whole suggestion |
| `<C-Right>` | accept the next word |
| `<C-Down>` | accept the first line |

Or map `require("armin").accept`, `.accept_word` and `.accept_line`
yourself. They aren't expr-mappings, because they edit the buffer and
textlock forbids that in an expr (E565).

What keeps it fast:

- **Typing through.** While what you type matches the suggestion,
  it keeps showing what's left with no new request. An answer that
  arrives after you typed a few more matching characters still
  shows.
- **Short waits.** A request goes out after a 120 ms pause
  (`setup({ debounce = ms })`). A newer request cancels the older
  one inside armin and in llama-server.
- **Short answers.** armin sends llama-server's `/infill` the cursor
  line's indentation (`n_indent`) and a time budget, so generation
  stops at the end of the block instead of running to 128 tokens.
  The old request took 1.1–2.3 s on the 9B model; this one takes
  ~0.26 s through nvim.
- **Context.** Up to 64 lines above the cursor and 24 below, plus the
  file's first 32 lines when they're out of view. Also included: the
  last-edited spot in up to three other open files of the same kind.
  The window moves in 16-line steps, so the prompt's start stays the
  same while you work.

llama-server reuses its prompt cache when only the end of the prompt
changed. That doesn't work for every model:

- Qwen3.5 and Qwen3.6 are hybrid (recurrent) models and reprocess the
  whole prompt on every request, about 0.5 s for ~1100 tokens on an
  RTX 4060.
- Sliding-window models like Mellum need `--swa-full` in their args
  to reuse the cache. With it, a follow-up request processed 175
  tokens instead of 873.

A completion error (a model without fill-in-the-middle tokens, say)
is shown once, as a notification.

## How it stays "minimal"

- One Lua file, built-ins only (`vim.lsp`, `vim.fs`, `vim.notify`,
  `vim.api`, Neovim's built-in prompt-buffer) — no dependency on
  nvim-lspconfig, telescope, plenary, or any UI plugin.
  `:ArminModel`'s picker and file-mention (`<C-f>` in `:ArminAsk`)
  are a small hand-rolled floating window (`_select_float`), not
  `vim.ui.select` — that avoids depending on whatever (if anything)
  has that hook installed, which matters because Neovim's own
  default `vim.ui.select` renders through the command-line/message
  area and can be invisible with a shrunk `cmdheight`, exactly the
  kind of setting a minimal config tends to have.
- No diagnostics — that's a job for your actual language server
  (gopls, pyright, etc.), which armin runs alongside, not in place
  of.
- Hover answers are capped and cached in the armin process itself
  (see `internal/lspserver`), so a slow local model doesn't turn
  into a runaway request storm from repeated hovers.
- Definition and workspace-symbol never touch the model at all —
  they're answered from a Universal Ctags index, the same
  deterministic approach any compiled-index language server uses.
- Completion is debounced and self-cancels on every keystroke, so
  typing quickly never queues up stale requests.
- `install.sh` links `~/.config/nvim/lua/armin.lua` to this repo's
  copy, so the plugin and the binary never drift apart.
- Every path argument the model can act on is still jailed to the
  project root by the same sandbox the REPL uses — see the main
  [README](../README.md#safety-model).
