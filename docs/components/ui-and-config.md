# The terminal UI and config

## UI (`internal/ui`)

A hand-rolled terminal layer: ANSI codes, no TUI framework.

- **Styles and lines** (`ui.go`): colors, the prompt, the line a tool
  call gets.
- **Wrapping** (`wrap.go`) at the terminal's width, never splitting a
  word or an escape sequence.
- **Markdown** (`markdown.go`) rendered as it streams: headings,
  lists, code blocks, tables kept whole with wrapped cells.
- **The spinner and status line** (`spinner.go`): what's running, for
  how long.
- **The line editor** (`lineinput.go`, `keys.go`): raw mode, ghost-text
  suggestions accepted with Tab, large pastes folded into a label.
- **The composer** (`composer.go`): the input bar under a running turn.
- **Errors** (`trace.go`): the message first, the `... in Func` trace
  folded away until you expand it.

## Config (`internal/config`)

`~/.config/armin/config.conf`: `key = value` lines and `#` comments,
nothing fancier. Every flag's default can come from it; the flag wins.
Main groups:

- `model`, `completion_model`: which `models.<name>` to start on
- `models.<name>.*`: a local llama-server (`binary`, `model_path`,
  `port`, `args`), a hosted API (`url`, `api_key_env`, `id`,
  `reasoning`, `region`) or a cloud machine (`aws_instance`,
  `stackit_server`, `scaleway_server`, ...)
- `effort`, `aids`: how hard models think, the reasoning aids
- `library`: the reference library's folder
- agent and sandbox limits: `max_tool_rounds`, `context_size`,
  `keep_turns`, `timeout`, `build_timeout`, `allow`, `allow_exec`,
  `test_cmd`, `build_cmd`, `lint_cmd`

API keys never go in config.conf: `api_key_env` names a variable,
found in the environment or `~/.config/armin/.env`. armin refuses to
read that file if anyone but you can.
