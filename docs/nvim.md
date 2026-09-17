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
| `:ArminAsk [question]` | ask armin something about the project. With no argument, opens a floating prompt window (not the command line) — `<CR>` submits, `<Esc>` cancels, `<C-f>` mentions a file. Answer shows in a floating window, hard-wrapped to ~78 columns (`q` to close). |
| `:ArminModel [name]` | show or switch the chat model used by `:ArminAsk`/hover. With no argument, opens a floating picker of installed models (size-annotated, smallest/largest coding-focused model tagged "fastest"/"best") — `j`/`k` or arrows to move, `<CR>` to pick, `<Esc>`/`q` to cancel. |
| `:ArminComplete` | toggle live ghost-text completion for the current buffer — fires automatically on a short pause after you type, once turned on. |
| `:ArminSuggest` | request one completion at the cursor right now, regardless of whether `:ArminComplete` is on. |
| `:ArminStop` | detach, turn off completion everywhere it was on, and kill the armin process. |

`:ArminModel` switches the *chat* model — the one `:ArminAsk` and
hover use, which requires Ollama tool-calling support. It's a
different thing from `-completion-model` (inline completion): plain
completion/FIM base models don't support tools at all. Switching to
one here doesn't fail silently: `:ArminAsk`/hover show a clean
one-line notification naming the model and pointing back at
`:ArminModel`, instead of Ollama's raw HTTP error.

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

`:ArminComplete` shows a dim inline suggestion as you type (like
fish/zsh autosuggestions), computed by a separate, ideally
fill-in-the-middle-tuned model — see `-completion-model` in the main
README, since a general chat model gives noticeably worse
completions here. Accept it with a keymap you add yourself:

```lua
vim.keymap.set("i", "<S-Tab>", function()
  if require("armin").has_suggestion() then
    require("armin").accept()
  end
end, { silent = true })
```

Not an expr-mapping on purpose: `accept()` edits the buffer directly
(`nvim_buf_set_text`), and Neovim textlocks against buffer edits from
inside an expr-mapping's evaluation (`E565`). Shift-Tab has no
default insert-mode behavior, so there's nothing to fall back to when
no suggestion is showing.

Or pass `require("armin").setup({ tab_accept = true })` to have
`setup()` install that same mapping (`<S-Tab>`, chosen so it doesn't
collide with a real completion plugin's `<Tab>`) for you.

## Optional keymaps

Nothing else is bound for you. If you want one, add it yourself,
e.g.:

```lua
vim.keymap.set("n", "<leader>aa", "<cmd>ArminAsk<CR>", { desc = "Ask armin" })
```

## How it stays "minimal"

- One Lua file, stdlib-only (`vim.lsp`, `vim.fs`, `vim.notify`,
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
- Every path argument the model can act on is still jailed to the
  project root by the same sandbox the REPL uses — see the main
  [README](../README.md#safety-model).
