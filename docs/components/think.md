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
     scratchpad), the reasoning aids, the library, the roles' models,
     the knowledge DAG (opened, checked for stale references, then
     consolidated in the background) and the learner

Nothing slow holds up the prompt: a local chat model loads in the
background and says when it's ready, the repo map and the library's
change scan run beside the rest (`startup.go`), and their notices
reach the frontend through `Mind.Notices`.

`Mind.SystemPrompt` builds the system prompt: the base instructions,
then the project's instruction file, the memory and skill indexes,
the library's "where to look", the knowledge DAG's outline and rules,
and the repo map (`prompt.go`).

`Mind.Shutdown` stops only what armin started: llama-server
processes, cloud machines, the SearXNG container. It also runs on
SIGTERM, so a killed armin never leaves a GPU billing.

## Files

| file | what's in it |
|---|---|
| `think.go` | `New`, `NewSenses`, `wake` (`startModels`, `buildLoop`), `Shutdown` |
| `models.go` | `models.<name>` config entries → model specs |
| `prompt.go` | the system prompts and what's appended to them |
| `know.go` | opening and closing the knowledge DAG and the learner, consolidation in the background |
| `stale.go` | the start-of-session check of cited files |
| `startup.go` | timing each step, the background model load, notices |

## Connections

- reads: `input.Options`
- builds: `sandbox`, `searxng`, `ctags`, `repomap`, `library`,
  `llamacpp`, `agent`, `notes`, `know`, `learner`
- read by: `display`, which runs the frontends on the `Mind`
