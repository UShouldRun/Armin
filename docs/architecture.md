# How armin is built

One page to read first. Each component has its own short doc in
[components/](components/).

## The shape

`cmd/armin/main.go` is three steps, one package each:

```
input  ──Options──▶  think  ──Mind──▶  display
what you asked       the mind           how it's shown
```

1. **[input](components/input.md)**: `.env`, `config.conf` and the
   flags become one `Options`.
2. **[think](components/think.md)**: builds the `Mind`: the sandbox,
   the models, the agent loop, the notes, the library.
3. **[display](components/frontends.md)**: runs a frontend on it: the
   terminal, the browser, nvim, or MCP.

If an armin already hosts the project, a new one joins it instead
(`share`), so the models load once per project.

## A question, end to end

```
you type ─▶ repl (or web, or nvim)
              │  effort and kind of task (agent.PlanTurn)
              ▼
            agent.Loop.Turn ◀──────────────────────────┐
              │  context kept in the window             │
              ▼                                         │
            llm.Client ─▶ llama-server / hosted API     │
              │  answer?  ─▶ checked, shown, saved      │
              │  tool calls?                            │
              ▼                                         │
            tool table (agent/tools.go)                 │
              │  repeat guard, approvals for writes     │
              ▼                                         │
            tools.* inside the sandbox ──── results ───┘
```

- The **frontend** draws the turn as it streams and asks you before
  writes and runs ([frontends](components/frontends.md)).
- The **agent** loops model call → tools → model call until there's
  an answer, keeps the context inside the window, and steers with the
  reasoning aids ([the agent](components/agent.md)).
- **llm** sends each request; **llamacpp** starts and stops the
  servers behind it, on this machine or a cloud GPU
  ([models](components/models.md)).
- **tools** do the work; the **sandbox** keeps paths, commands and
  time within bounds ([tools and the sandbox](components/tools-and-sandbox.md)).
- The **library** answers technical questions with cited passages
  ([the library](components/library.md)); **notes** and **session**
  keep what lasts ([memory and sessions](components/memory-and-sessions.md)).

## Where things live

| package | job |
|---|---|
| `input`, `config` | what armin is told |
| `think` | builds the Mind |
| `display` | picks the frontend |
| `repl`, `web`, `lspserver`, `mcpserver` | the frontends |
| `share` | one armin per project: host and joiners |
| `agent` | turns, the tool table, context management, reasoning aids |
| `llm`, `llamacpp`, `cloud`, `catalog` | models: client, servers, cloud machines, OpenRouter's list |
| `install` | `-install`: sizes a model up against the machine, downloads and configures it |
| `tools`, `sandbox` | what tools do, and their boundaries |
| `ctags`, `searxng` | the symbol index, the local web search |
| `library` | the reference library |
| `notes`, `session`, `convo` | memories, skills, scratchpad; saved sessions; context stats |
| `ui` | the terminal: styles, Markdown, the line editor |

## Principles you'll see everywhere

- **No dependencies**: the standard library and `golang.org/x/sys`.
  Protocols (LSP, MCP, SSE, AWS signing) are hand-rolled.
- **Boundaries in code**: the path jail, the allowlists, approvals
  and timeouts don't rely on the model behaving.
- **Small, stable requests**: the prompt prefix stays byte-identical
  across turns so the server's cache keeps hitting; tool results are
  compact.
- **Memory-conscious**: nothing large is held without a reason. The
  library index stays on disk, reads from the network are bounded,
  and sessions are append-only logs.
- **Every file says what it holds**: a header at the top of each file,
  a map in each package's doc comment.
