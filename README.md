<p align="center"><img src="assets/banner.svg" alt="armin — wings of freedom mark" width="100%"></p>

# armin
A terminal coding assistant, backed by a llama.cpp model (local, on a
remote box, or hosted) that reads, searches and edits your codebase,
runs its build and tests, and grounds every answer in what's actually
there — not generic training data. Named after the *Attack on Titan*
strategist: the one who reads the terrain before anyone moves.

The wings mark is the project's logo (assets/, in light and dark
variants; the dark one is the default). The README banner above is the
same mark the web UI's header draws, on a transparent background;
the startup header draws a small PNG of it inline where the terminal
can show images (built into the binary from internal/ui/logo.png),
and the web UI's pages carry the mark as their icon.

One sandboxed tool layer, four front ends:

- **REPL**: a chat loop in your terminal.
- **Web UI** (`-web 127.0.0.1:7777`): the same chat in a browser,
  with previews for PDFs, Markdown and code.
- **MCP server** (`-mcp`): the tools over stdio JSON-RPC, for any MCP
  client.
- **LSP server** (`-lsp`): hover-to-explain, ask, and inline
  completion in your editor; one Lua file for Neovim.

## Requirements

- Go 1.23+ to build; the binary has no runtime dependencies
- llama.cpp's `llama-server` (running already, or on `PATH` for armin
  to start), or a hosted OpenAI-compatible endpoint such as OpenRouter
- `rg` (ripgrep) on `PATH`
- Optional: Docker (`web_search`), universal-ctags (`-lsp` symbols),
  poppler-utils (`read_pdf`)

## Install

```sh
./install.sh       # build, install to ~/.local/bin, check dependencies
go build -o armin ./cmd/armin   # or just build it
```

## Quickstart

```sh
armin .                        # chat about the project here
armin . -web 127.0.0.1:7777    # the same, in a browser
armin -resume                  # pick up an earlier session
armin -install qwen3.5-9b      # size up, download and configure a model
```

Inside, `/help` lists commands, `/model` switches models, `/effort`
sets how hard it thinks, and `/quit` leaves. File writes and program
runs are shown and asked about first.

## Documentation

| Read | For |
|---|---|
| [install](docs/guide/install.md) | requirements and what `install.sh` does |
| [usage](docs/guide/usage.md) | slash commands, flags, approvals, running programs |
| [tools](docs/guide/tools.md) | every tool, files outside the project, the safety model |
| [config](docs/guide/config.md) | `config.conf` keys and llama-server defaults |
| [models](docs/guide/models.md) | managed and external models, `-install`, OpenRouter, cloud GPUs |
| [thinking](docs/guide/thinking.md) | effort, reasoning aids, context management |
| [memory](docs/guide/memory.md) | memories, skills, the scratchpad, sessions |
| [library](docs/guide/library.md) | the reference library and `library_search` |
| [frontends](docs/guide/frontends.md) | the web UI, MCP and LSP modes |
| [nvim](docs/nvim.md) | the Neovim integration |
| [architecture](docs/architecture.md) | how the code fits together, one doc per component |

`man armin` covers the flags once installed.

## Principles

- **No dependencies**: the standard library and `golang.org/x/sys`.
- **Boundaries in code**: path jail, allowlists, approvals and
  timeouts don't rely on the model behaving.
- **Small, stable requests**: a byte-stable prompt prefix so the
  server's cache keeps hitting; compact tool results.

## License

Apache 2.0, see [LICENSE](LICENSE).
