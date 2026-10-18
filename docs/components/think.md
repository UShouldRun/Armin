# Think

armin's mind: the sandbox its tools act in, the models it reasons
with, the agent loop tying them together, and what it remembers.
Package `internal/think`; main's second step (input → think →
display).

## What it does

`think.New(opts)` builds a `Mind` in two stages:

1. **Senses** (`NewSenses`) — no models yet, so `-mcp` stops here:
   - the **sandbox**: the project root as a jail, the command
     allowlists, timeouts (`internal/sandbox`)
   - **web search**, if docker exists (`internal/searxng`)
   - the **symbol index** (`internal/ctags`)
   - the **reference library**, if configured, whose folder becomes
     readable (`internal/library`)
2. **Wake**:
   - `startModels`: the model manager (`internal/llamacpp`) from
     config.conf's `models.*` entries; the chat model starts now (a
     local one loads on the GPU), inline completion loads on first
     use
   - `buildLoop`: the agent loop (`internal/agent`) on the chat
     model, with the sandbox's services, the notes (memories, skills,
     scratchpad), the reasoning aids and the library

`Mind.SystemPrompt` builds the system prompt: the base instructions,
then the project's instruction file, the memory and skill indexes,
and the library's "where to look" (`prompt.go`).

`Mind.Shutdown` stops only what armin started: llama-server
processes, cloud machines, the SearXNG container. It also runs on
SIGTERM, so a killed armin never leaves a GPU billing.

## Files

| file | what's in it |
|---|---|
| `think.go` | `New`, `NewSenses`, `wake` (`startModels`, `buildLoop`), `Shutdown` |
| `models.go` | `models.<name>` config entries → model specs |
| `prompt.go` | the system prompts and what's appended to them |

## Connections

- reads: `input.Options`
- builds: `sandbox`, `searxng`, `ctags`, `library`, `llamacpp`,
  `agent`, `notes`
- read by: `display`, which runs the frontends on the `Mind`
