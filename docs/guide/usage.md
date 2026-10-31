# Using armin

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
| `/sessions [rm <id>]`, `/resume <id>`, `/title <text>` | list, delete, switch or rename sessions — see [memory.md](memory.md#sessions) |
| `/save` | force a session save right now |
| `/suggest` | toggle 2-3 numbered follow-up questions after each answer — type a number to ask one instead of writing it out |
| `/ghosttext` | toggle live ghost-text completion while you type (off by default — see "Reading width") |
| `/thinking` | toggle showing the model's thinking (dimmed) as it streams |
| `/approve` | toggle asking before each file write |
| `/memory [show\|rm <name>]` | list, show or delete memories |
| `/remember <text>` | ask the model to save something to memory |
| `/skills` | list skills |
| `/skill <name> [task]`, `/<name> [task]` | run a skill |
| `/scratch [show <name>\|clear]` | list, show or clear the scratchpad |
| `/effort [auto\|off\|low\|medium\|high] [task]` | how hard the model thinks; `auto` lets it judge each request — see [thinking.md](thinking.md#how-hard-it-thinks) |
| `/library [index\|catalog [model]\|merge [model]\|add <path>... [to <folder>]\|topics [topic]]` | the reference library: status, reindex, have a model catalog new books, copy files in, show the topic table — see [library.md](library.md) |

Switching to a model with no tool-calling support (a plain
completion/FIM base model, say) doesn't fail with a raw HTTP error —
armin shows a clean one-line message naming the model and pointing
back at `/model` to pick a different one.

### Reading width

On a wide terminal armin keeps text to 75% of the width; up to 100
columns it uses the whole width (120 columns → 100, 200 → 150).
Answers and thinking word-wrap there as they stream, list items
with a hanging indent. Code blocks, tables, indented code, tool
lines and diffs are never wrapped, since breaking them would change
what they mean. The width is re-read every turn, so resizing the
window takes effect. Piped output is never wrapped.

The input line wraps at the same width, continuation rows lined up
under the text. To do that, armin edits the line itself in raw
mode: typing, Backspace, Ctrl-U (clear), Ctrl-W (delete word),
Ctrl-C, Ctrl-D, and Shift-Enter or Alt-Enter for a new line
(Shift-Enter needs a terminal with the kitty keyboard protocol:
Ghostty, kitty, WezTerm, foot); no arrow-key movement or history. `/ghosttext`
adds dim inline suggestions that Tab accepts. Piped input and
scripts get a plain buffered read.

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
| `-new` | off | start a separate instance even when one runs in this project (by default a later armin joins the first one, terminal or nvim — see [docs/nvim.md](../nvim.md#one-armin-per-project)) |
| `-resume [id]` | off | resume a session: alone, pick from a menu; with an id (or prefix), that one |
| `-no-save` | off | don't persist the session to disk after each turn |
| `-no-color` | off | disable ANSI styling |
| `-aids` | `all` | reasoning aids: `all`, `none`, or a list of `stuck`, `procedure`, `verify` — see [thinking.md](thinking.md#reasoning-aids) |
| `-effort` | `auto` | how hard the model thinks: `auto` (it judges each request), `off`, `low`, `medium` or `high` |
| `-library-sync` | off | index the reference library and have `-model` catalog the books the topic table lacks, then exit |
| `-library-add` | — | comma-separated files or folders to copy into the library (under `-library-to`, default `added`), then index and catalog them, and exit |
| `-install` | — | size a model up against this machine, then download it and add it to config.conf: `org/repo[:QUANT]`, `org/repo/file.gguf` or a local `.gguf` — see [models.md](models.md#installing-a-model) |
| `-install-to` | where your models are | the folder `-install` downloads into (else `~/models`) |
| `-uninstall` | — | remove a model: its `models.<name>` lines and its `.gguf`, unless another model uses it; `-uninstall list` shows every model and its size |

Flag defaults can also come from a config file (see [config.md](config.md)); explicit
flags always win.

## Editing files

Before every `write_file` / `edit_file` / `run_program`, the REPL
shows what's about to happen — a change with line numbers, a new
file's first lines, a command with the script it runs — and asks:

```
● Update(internal/ui/ui.go)
  └ 1 addition, 1 removal
      97    const maxValue = 60
      98  - parts := make([]string, 0)
      98  + parts := make([]string, 0, len(args))
      99    for _, a := range args {
apply edit_file? [y]es / [n]o / [a]ll this session / [v]iew in nvim / or type feedback:
```

`v` opens it read-only in nvim in a new terminal window — a change
as a diff against the file on disk, a run as its script — and asks
again when you come back. The window is config.conf's `terminal`
(e.g. `terminal = ghostty -e`), a tmux window inside tmux,
`$TERMINAL`, or the first of ghostty, kitty, wezterm, alacritty,
foot, konsole, gnome-terminal and xterm installed.

Typing anything other than y/n/a/v declines and sends what you
typed to the model as feedback. `a` (or `-yes`, `auto_approve =
true`, `/approve`) stops asking; changes and runs are still shown.
Ctrl-C stops the current turn without leaving armin.

## Running programs

`run_program` runs a build, a test or a program the project built
(`make`, `mpirun -np 4 ./fox`, `python3 gen.py`), with no shell:
a binary and its arguments, asked about each time. It can feed a
project file as the program's input (`stdin`) and save its output to
one (`stdout`), so "run it on input6 and diff against output6" needs
no shell. For real pipes the model uses `bash -c "..."` — shells,
like `rm`/`cp`/`mv`/`scp`, are asked about every time, even after
`a` (all this session).

When the model re-runs the same command after changing something
and gets the same output, the run still happens and the model is
told the output didn't change; only the same call back to back (a
loop) stops the turn.

## Reading answers

Answers are markdown, shown rendered: headings, **bold**, *italic*,
`code`, bullet and numbered lists, quotes, fenced code blocks and
tables. Each line shows raw while it streams and is redrawn rendered
when it ends.

While a turn runs, the bottom row is an input bar: type a message
and press Enter, and the model reads it at its next step — or, if it
was about to finish, keeps going to answer it. A question (an
approval) pauses the bar; what's left unsent comes back in the next
prompt.

