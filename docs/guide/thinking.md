# Thinking and context

## Thinking models

A thinking model's reasoning (llama-server's `reasoning_content`;
`--jinja` and the default `--reasoning-format` produce it) streams
dimmed above the answer; `/thinking` hides it. It's sent back to the
model within a turn, so the chain of thought survives across tool
calls, and dropped when the turn ends - the same thing the chat
template does anyway, so the prompt cache isn't disturbed.

Requests use a read-idle timeout instead of a whole-request one:
a generation that keeps streaming is never cut off, and a server
that goes silent for `read_timeout` (default 5m, long enough for a
large prompt to process on a remote box) is.

### How hard it thinks

`/effort [auto|off|low|medium|high] [task]` sets how much the model
thinks; `effort =` in config.conf and `-effort` set the default,
`auto`. With a task (`/effort high why does this deadlock?`) the
level is for that task alone. `auto` lets the model judge: before
each turn it answers, with its thinking off, whether the request
needs low, medium or high, and armin shows the choice. The web chat
takes an `effort` field per message.

A hosted model gets OpenRouter's `reasoning.effort`. A local
llama-server gets thinking on or off (`enable_thinking`, for templates
that switch it) and a cap on thinking tokens, which works whatever the
template: off 0, low 1024, medium 4096, high unbounded. Not the
template's own `reasoning_effort`: K2 Horizon's opens a different
thinking tag per level, which llama.cpp doesn't recognize.

High and low effort also put the line Qwen3.8's chat template uses
for its xhigh and low efforts at the top of the system prompt ("Reasoning
effort is set to xhigh. Please think carefully..."); Qwen3.8 is trained
on those words, and to other models they're plain instructions. The
prompt only changes when the effort does, so the server's cache holds.

### Reasoning aids

Three small aids steer how the model works, not what it knows (`aids =`
in config.conf, `-aids`: `all` by default, `none`, or a list):

- **stuck**: after three misses in a row (tool errors, failing tests or
  builds, programs exiting non-zero, with any passing check resetting
  the count) armin asks the model to step back: what it knows for sure,
  two or three hypotheses, the cheapest one to test. At most twice a
  turn.
- **procedure**: auto's question also names the kind of task (fix,
  build, debug, explain, shell), and the request gets that kind's
  steps, each opening with a feasibility check: say so when the
  request contradicts the code or can't be done. Files in
  `~/.config/armin/procedures/<kind>.md` replace the built-in steps.
- **verify**: before the final answer of a turn that changed files or
  ran code, one quick no-thinking check of the request, the actions and
  their outcome, and the answer. An issue (an edit never tested, a part
  of the request not done) sends the model back for one more round.

## Context management

The window is spent on three things: the prompt, the history, and
room for the model to think and answer. Before every model call,
armin decides (`agent.Decide`, a pure function with its own tests)
what keeps the next request inside the window most cheaply, and
prints a `context:` line in the REPL whenever it does something:

| Context used | What armin does |
|---|---|
| under 50% of the window | nothing; the prompt prefix stays byte-stable, so llama-server's KV cache keeps hitting |
| 50–75% | **trims** tool results older than `keep_turns` turns. Each is **archived to the scratchpad** first, and the stub left in context says exactly where (`scratchpad_read name=archive-1 start_line=… end_line=…`), so the model re-reads instead of re-running a slow or non-deterministic tool. Loaded skills and memories are kept. |
| over 75%, mostly the **current turn's** work | **grows** the window to the streaming tier (below), if the model has one. Summarizing would discard exactly what the model is working with, so armin pays one reload and re-prefill instead. |
| over 75%, mostly **older history** | **saves, then compacts.** The model gets a short turn with only `scratchpad_write`, `scratchpad_read` and `memory_save` to write down its plan, findings and durable facts; then older history is summarized into a brief that points at those files. The latest request is carried over verbatim, and the cut never separates a tool call from its result. |
| after a compaction, in the streaming tier, with usage under half the resident window | **shrinks** back to the fast tier; compaction already invalidated the prefix cache, so the switch costs almost nothing extra |

Thinking from finished turns leaves the context too, but isn't lost:
it's appended to the scratchpad file `thinking`, labeled with the
date and the request, for the model to re-read.

The window size comes from llama-server's `/props`, `context_size`
in config, or the streaming tier's settings. `/context` shows the
server's real token count, how full the window is, and which tier is
active.

### Streaming tier: more context than VRAM holds

A model can have a second, larger tier served by a llama-server
build with adaptive KV streaming
([Raymond Huang's fork](https://github.com/RaymondHuang210129/llama.cpp-adaptive-kv-streaming)).
It keeps a resident pool and a ring buffer in VRAM and the rest of
the KV cache in pinned RAM, prefetching it over PCIe while attention
runs:

```
models.local.args             = --jinja -ngl 999 -c 98304
models.local.stream_binary    = /path/to/kvstream/build/bin/llama-server
models.local.stream_context   = 196608
models.local.stream_arena_mib = 2048
models.local.stream_args      = --fit off -b 256 -ub 256
```

The resident tier's window comes from `-c` in `args` and must be
set. Moving between tiers restarts llama-server on the same port,
re-prefilling the conversation. Decoding slows as more of the cache
has to stream (PCIe bandwidth is the limit), which is why armin only
grows when the current turn needs it, and comes back as soon as a
compaction makes room.

