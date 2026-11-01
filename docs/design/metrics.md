# Metrics: how much armin's deterministic systems help the models

armin does a lot of work without a model: it searches the knowledge
tree, holds duplicate writes back, checks cited code for changes, runs
acceptance checks, guards against repeated calls. `internal/metrics`
counts what each of those does, so it can be seen which ones pay.

## Builds

| build | what it keeps |
|---|---|
| release (`go build ./cmd/armin`) | counts and measured values in memory, merged into `~/.local/share/armin/metrics/metrics.tsv` at shutdown: one line per metric, name, count, sum, min, max |
| debug (`go build -tags debug ./cmd/armin`) | the same, plus every event as it happens, appended to `metrics/events-<date>.tsv`: time, name, key=value pairs |

`armin -metrics` prints the cumulative file grouped by system, then the
derived rates below. Nothing leaves the machine.

## The systems and their metrics

### Tree recall (`internal/agent/recall.go`)

armin searches the tree with each turn's message and attaches the best
matches above it (on by default; `recall = off` in config.conf turns it
off).

| metric | meaning |
|---|---|
| `recall.turns` | turns recall ran on |
| `recall.attached` | turns it attached at least one node |
| `recall.empty` | turns nothing matched well enough |
| `recall.nodes` | nodes attached per turn (avg, min, max) |
| `recall.cited` | turns the model cited an attached node: its `[nN]` in the answer, or a `know_write` naming it (id, from, to, needs, derived_from) |
| `recall.cited.nodes` | attached nodes cited per such turn |
| `recall.read` | turns it cited none but read a recalled node's file: a weak sign, since it often reads that file anyway |
| event `recall.attach`, `recall.settle` | debug: per turn, which nodes, how many cited, whether read |

Rates: attached of turns (does the tree cover what's asked?), **cited
of attached** (does the model trust what armin hands it?), read
without a citation of attached.

### Tree writes (`internal/agent/tools_know.go`)

| metric | meaning |
|---|---|
| `know.write.add`, `.update`, `.link` | writes by kind |
| `know.dedup.held` | adds held back because a similar node existed |
| `know.dedup.forced` | adds the model forced through anyway |
| `know.minion.added`, `know.minion.capped` | minion writes, and those whose confidence was capped |
| `know.verified` | minion claims a copilot or advisor verified |
| `know.cycle.opened`, `.resolved` | requires loops a write closed, and later resolved |
| `know.record.turns`, `know.record.refs` | turns armin recorded itself, and the file references each carried |
| event `know.dedup.held` | debug: the path, the similar node, the score |

Rates: dedup held of adds (is the model repeating itself?), dedup
overridden of held (is the 0.6 threshold too eager?), auto-recorded of
turns.

### Consolidation (`internal/agent/consolidate.go`)

In the background at session start, on the minion: a few recorded
pointers turned into claims a later session can answer from.

| metric | meaning |
|---|---|
| `know.consolidate.pending` | pointers waiting when a pass started |
| `know.consolidate.pointers` | pointers turned into at least one claim |
| `know.consolidate.claims` | claims written |
| `know.consolidate.linked` | claims a similar existing node already made, linked instead |
| `know.consolidate.empty` | replies with no usable claim |
| `know.consolidate.failed` | pointers whose model call failed |
| `know.consolidate.in`, `.out`, `.cost` | the pass's own tokens and dollars |
| `know.consolidate.ms` | time per pass |
| event `know.consolidate` | debug: the pointer and how many claims |

Rates: consolidated of pending (does the minion manage it?), linked of
claims (is it repeating what's known?).

### Staleness (`internal/think/stale.go`)

| metric | meaning |
|---|---|
| `stale.refresh.ms` | time of the start-of-session check |
| `stale.files.cited`, `.skipped` | cited files, and those the stamp spared from re-reading |
| `stale.changed` | references whose lines changed or moved |
| `stale.moved` | of those, followed to their new place |
| `stale.stale`, `stale.suspect` | nodes marked stale, and dependents marked suspect |

Rates: skipped by stamp of cited (is the stamp worth having?), moved of
changed (how much false staleness re-anchoring prevents).

### Delegation (`internal/agent/tools_delegate.go`)

| metric | meaning |
|---|---|
| `delegate.calls` | tasks handed to the minion |
| `delegate.check.run`, `.passed` | acceptance checks run, and passed |
| `delegate.nodes.raised` | minion claims raised to checked by a passing check |
| event `delegate` | debug: id, check kind, outcome |

Rate: checks passed of run (does the minion get handed tasks it can do?).

### Every tool (`internal/agent/turn.go`)

| metric | meaning |
|---|---|
| `tool.calls`, `tool.errors` | all calls, and those that returned an error |
| `tool.<name>.calls`, `.errors` | per tool |
| `tool.<name>.ms` | time per call |
| `tool.<name>.bytes` | size of the result put in context, per call |
| `turn.count` | turns |
| `guard.blocked` | repeated failed calls the repeat guard refused |
| event `tool`, `guard.blocked` | debug: per call |

Per-tool error rates and result sizes show which tools models misuse
and which flood the context.

### Startup (`internal/think/startup.go`)

The chat model, when armin runs it on this machine, loads in the
background: the prompt shows at once, a first message waits for the
model only if sent before it's ready, and armin says
"model ready: local loaded in 4.5s" when it is. The library scan runs
in the background too. `ARMIN_STARTUP=1` prints every step.

| metric | meaning |
|---|---|
| `startup.<step>.ms` | each step: `senses.symbols`, `senses.library`, `models.gpu-check`, `models.chat-ready` (a hosted or already-running model), `models.catalog`, `loop.context-size`, `loop.notes`, `loop.learner`, `loop.know-open`, `loop.know-refresh` |
| `startup.models.chat-load.ms` | loading a local chat model onto the GPU, in the background |
| `startup.library-scan.ms` | the library's change scan, in the background |

Measured on this machine: the prompt went from 3.1–5.0 s to under 5 ms;
the 9B loads in 2.7–4.6 s behind it, and the library scan took 0.42 s
of every start.

### Speech (`internal/stt/stt.go`)

| metric | meaning |
|---|---|
| `stt.recordings`, `stt.flat` | recordings, and those refused as a flat signal |
| `stt.ms` | transcription time |

## Not measured yet

- the learner tree's probe (questions per concept settled) and fading:
  they have no caller until step 10 (teaching)
- the library's cataloging; library_search is covered as a tool
- wall time per turn: the benchmark has it, armin doesn't log it
