# Input

Everything armin is told before it starts. Package `internal/input`;
the first of main's three steps (input → think → display).

## What it does

`input.Parse` reads, in this order:

1. **`~/.config/armin/.env`** — API keys (refused if group- or
   world-readable). A variable already exported in the shell wins.
2. **`~/.config/armin/config.conf`** — `key = value` defaults
   (package `internal/config`).
3. **The command line** — every flag, its default taken from
   config.conf; a bare path (`armin .`) is the project root.
4. **`-resume`** — alone, a menu of saved sessions; with an id, that
   session (which may move the project root).

The result is one `Options` struct: the root, the model names, the
mode (REPL, web, LSP, MCP, library sync), on/off flags packed in one
byte, effort and reasoning aids, and the raw config for whatever has
no flag. `think` and `display` read only this.

`WatchSignals` routes signals while armin runs: Ctrl-C first to the
REPL (to stop the current turn), and SIGTERM/SIGHUP to a cleanup that
stops the models and cloud machines armin started, so a killed
terminal never leaves a GPU billing.

## Files

| file | what's in it |
|---|---|
| `input.go` | `Parse`, the `Mode` and `Flag` values |
| `flags.go` | flag declarations, the bare-path handling |
| `session.go` | the `-resume` menu |
| `signals.go` | the signal router |

## Connections

- reads: `config` (config.conf, .env), `session` (saved sessions),
  `agent` (effort and aid names)
- read by: `cmd/armin/main.go`, then `think` and `display` via
  `Options`
