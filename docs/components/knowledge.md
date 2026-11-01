# The knowledge DAG and the repo map

What armin knows about a project, kept between sessions, and the
static map it starts every session with. Packages `internal/know`,
`internal/repomap`; wired in by `think` (`know.go`, `stale.go`) and
the agent (`tools_know.go`, `recall.go`, `consolidate.go`).

## The repo map (`repomap`)

Built at start, in the background, from the ctags index: each file
ranked by how often the rest of the project names what it defines,
its top definitions with their lines, about 2k tokens. It goes in
the system prompt and stays fixed for the session, so the prompt
stays cacheable. On by default (`repo_map = off`). In the benchmark
it cut tool calls by a quarter from the first session.

## The knowledge DAG (`know`)

A graph of nodes (topics, facts, decisions, concepts, file pointers)
with edges between them (needs, derived from, contradicts), each
node with its evidence (a file range with a hash, a commit, a URL)
and who wrote it (armin, the minion, the copilot, the advisor) at
what confidence.

- **Storage**: a snapshot read through mmap plus an append-only log,
  compacted when a changed DAG closes. `-know dump` and `-know edit`
  show it as text.
- **Search**: BM25 over names and claims, plus path prefixes
  (`find.go`).
- **Staleness** (`think/stale.go`): at start, every cited file whose
  stamp changed is re-hashed; a range whose lines moved follows them,
  one whose lines changed marks its node stale and the nodes resting
  on it suspect.

## How a session uses it

1. The system prompt carries the DAG's outline and the rules for
   writing to it (`think/prompt.go`).
2. **Recall** (`agent/recall.go`): armin searches the DAG with each
   user message and puts the best matches above it. The model cites
   them as `[nN]`.
3. **Writing**: the model adds what it established (`know_write`),
   held back when a similar node exists.
4. **Recording** (`tools_know.go`, `recordTurn`): after a turn that
   read files, armin records a pointer under `sessions/`: the files
   and lines the answer rests on, and the user's question.
5. **Consolidation** (`agent/consolidate.go`): at the next start, in
   the background on the minion, pointers become claims a later
   session can answer from.

`internal/metrics` counts what each of these does (`armin -metrics`;
docs/design/metrics.md).

## What it's worth

The benchmark (docs/design/restructure-tree-economics.md): with the
repo map in place, the DAG breaks even for answering questions about
code. Its job is what a map can't hold: decisions and why, what was
tried, the advisor's plans, and the learner's state.

## Connections

- built by: `think` (opened, refreshed, closed)
- used by: `agent` (the know tools, recall, recording, consolidation),
  `display` (`-know`), `learner` (the user's own graph)
- uses: `tokens` (the search terms), `ctags` (the repo map)
