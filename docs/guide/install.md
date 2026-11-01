# Installing armin

## Requirements

- Go 1.23+ to build (the binary itself has no runtime dependencies)
- [llama.cpp](https://github.com/ggml-org/llama.cpp)'s `llama-server`,
  either already running (point armin at it with `-llama-url`) or on
  `PATH` if you want armin to spawn/reap it itself (see [models.md](models.md))
  - plus a tool-calling-capable `.gguf` model and,
  for -lsp inline completion, a separate FIM-capable one
- [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) on `PATH` -
  `search_code` shells out to it
- Optional: [Docker](https://docker.com), for the `web_search` tool -
  it runs a local [SearXNG](https://docs.searxng.org) instance,
  started on first search and stopped on exit (see [tools.md](tools.md)).
  Missing docker just means `web_search` reports a clean
  "unavailable" error; everything else works as normal.
- Optional: [Universal Ctags](https://ctags.io) (`universal-ctags`) for
  `-lsp`'s go-to-definition and workspace-symbol search - not the
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
behind `/ghosttext` - everything else (the REPL, the LSP/MCP
JSON-RPC servers, the llama-server client, ANSI styling) is
hand-rolled against the standard library.

## Install

```sh
./install.sh
```

Builds the binary and installs it to `~/.local/bin` (override with
`--prefix DIR`), and its manual page to `~/.local/share/man/man1`
(`man armin`; `man ./docs/armin.1` reads it in place), then reports on every dependency above - what it
found, what's missing, and exactly what to do about each one. Writes
a starter `config.conf` (skip with `--no-config`) only if you don't
already have one; never overwrites an existing one. Installs the
bundled skills (see [memory.md](memory.md)) the same
way: a skill already in `~/.config/armin/skills/` is left alone,
since you may have edited it - `--update-skills` replaces them,
`--no-skills` skips them. Safe to re-run
any time, e.g. after `git pull`, to rebuild and reinstall.

It also checks every command armin runs, in groups: armin's own
tools (`rg`, `git`, `pdftotext`/`pdfinfo`, `tree`, `file`, the
coreutils `run_command` allows), C and C++ builds (`make`, `gcc`,
`g++`, `cmake`, `gdb`, `valgrind`), file transfer (`rsync`, `scp`)
and MPI (`mpicc`, `mpirun`). For a group with something missing it
shows the exact install command for your package manager (apt, dnf,
pacman or brew) and asks first; if you say no, it lists what armin
can't do without each one. `--install-deps` installs without
asking, `--no-deps` only reports. Other toolchains `run_program` can
use (Go, Rust, Python, Node, Java, …) are reported, never installed.

Prefer to do it by hand instead?

```sh
go build -o armin ./cmd/armin
```

`install.sh` does nothing beyond that build, a copy onto `PATH`, the
dependency checks above and, only when you say yes, installing what's
missing - there's no hidden step you'd be skipping by not using it.


## Trying a branch beside the installed armin

```sh
./install-test.sh            # builds this checkout as ~/.local/bin/armin-test
armin-test .
```

`armin-test` keeps everything apart from the installed armin: its own
config (`~/.config/armin-test`), sessions, memories, knowledge DAGs and
metrics (`~/.local/share/armin-test`), and its own join socket, so it
never joins or hosts the installed armin's sessions. The first run
seeds its config from `config.conf.restructure` (or a copy of the
installed config.conf), copies the API keys and links the skills; later
runs only rebuild the binary. `--name` picks another name. It shares
the configured llama-server ports, so with both running they use the
same model server.
