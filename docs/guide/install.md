# Installing armin

## Requirements

`./install.sh` makes sure of these, installing what's missing after
asking (yes by default; `--install-deps` doesn't ask, `--no-deps`
only reports):

- **Go** 1.21 or later, to build armin (Go then fetches the exact
  toolchain go.mod asks for). Missing or older, the latest release
  goes into `~/.local/go`.
- **llama.cpp's `llama-server`**, to run local models. Found on `PATH`
  or named by a `models.<name>.binary` in config.conf, it's left
  alone; otherwise it's built from source into
  `~/.local/share/armin/llama.cpp` and installed beside armin, with
  CUDA when the CUDA toolkit is installed, Metal on a Mac, the CPU
  otherwise. Without one, a hosted model or a `llama_url` pointing at
  a server elsewhere still works ([models.md](models.md)).
- **[ripgrep](https://github.com/BurntSushi/ripgrep)** (`rg`):
  `search_code` runs it.
- **A version control system armin supports**: git, for now. It's
  checked for, never installed: without one, the history, diff and
  blame tools don't work, and install.sh recommends installing git.

Offered, with what armin loses without each:

- [Universal Ctags](https://ctags.io), for the repo map in the prompt
  and `-lsp`'s go-to-definition. Not the Emacs `ctags` some distros
  ship as `/usr/bin/ctags`, a different tool despite the name; armin
  finds it as `universal-ctags`, `ctags-universal`, or a `ctags`
  that is Universal Ctags.
- [Docker](https://docker.com), for `web_search`: it runs a local
  [SearXNG](https://docs.searxng.org), started on the first search and
  stopped on exit ([tools.md](tools.md)). Without it, `web_search`
  reports "unavailable" and everything else works.
- The commands `run_command` and `run_program` use, in groups below.

You also need a tool-calling `.gguf` model and, for `-lsp` inline
completion, a fill-in-the-middle one ([models.md](models.md)).

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
tools (`pdftotext`/`pdfinfo`, `tree`, `file`, the coreutils
`run_command` allows), C and C++ builds (`make`, `gcc`,
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
dependency checks above and, when you say yes, installing what's
missing (Go, llama-server, packages) - there's no hidden step you'd
be skipping by not using it.
