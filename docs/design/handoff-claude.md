# Handoff: armin's knowledge DAG, branch `restructure`

You are continuing a multi-session effort on the `restructure` branch
of armin (~/Code/armin), a terminal coding assistant written in Go
backed by llama.cpp/OpenRouter models. This session (and the two
before it) benchmarked the knowledge DAG, fixed what the benchmark
showed, did a literature pass, and split the design docs. Everything
below is state as of the last commit; verify with `git log` and the
files listed.

## Read these first, in order

1. **docs/design/restructure-tree-economics.md** — the experiment
   log. Rounds 1–5 of the benchmark with numbers and postmortems,
   hypotheses H1–H4, and the research pass (what Mem0/Letta/Zep,
   Agent KB, Voyager, the AGENTS.md evaluation found). This is the
   single most important file: it says what was tried, what went
   wrong, and the agreed next order of experiments.
2. **docs/design/restructure-plan.md** — the settled design (roles,
   the graph, algorithms, advisor/teaching flow, build order) and
   §9's progress table with every commit hash. Sections 5's "Does
   the tree pay?" subsection is a summary pointing to the file
   above.
3. **internal/think/treebench_test.go** — the benchmark itself:
   five question-pairs about armin's own code, two sessions per
   pair, tree vs no-tree, 3 runs per round. `ARMIN_TREEBENCH_DUMP`
   takes an ABSOLUTE path (relative ones land under
   internal/think/); the OpenRouter key loads from
   ~/.config/armin/.env automatically.
4. **internal/agent/tools_know.go** — the three tree tools
   (`know_find` with `ids` for full nodes, `know_write` doing
   add/update/link, `know_trace`) and `recordTurn` (auto-record at
   end of turn: a `file`-kind node under `sessions/` whose claim is
   the answer's own citations, evidence the ranges read).
5. **internal/know/line.go** — the node line format: `at <file
   range>` first-evidence ref and `current` freshness mark on short
   lines. This is the read side the benchmark proved works.
6. **reports/treebench5/** — the latest round's traces (one .md per
   session: answer, tool calls, tree after). Earlier rounds are in
   reports/treebench{,2,3,4}/ and reports/treebench*-run*.log.

## Where we are

- Steps 1–9 of the restructure are built and tested; step 10
  (teaching) and 11 (end-to-end) remain (plan §9).
- The tree benchmark, round 5 (commit 642f3a3): pair costs **+21%
  input tokens** tree vs no-tree (s1 +17%, s2 +27%), accuracy equal,
  but the traces show the intended s2 flow working (find → ids →
  one targeted read → answer) wherever session 1 wrote a real node.
  The read side (at-refs, `current`, find guidance) is done.
- The remaining gap is (a) standing overhead (tool descriptions +
  tree section on every request) and (b) two sessions being too
  short to amortize s1's recording.
- The branch is **unpushed** and the user has **not yet used it
  daily**. A tmp config with the role keys and stt line sits at
  `config.conf.restructure` in the repo root — the user will move
  it to ~/.config/armin/ when they start testing; do not move or
  merge it into config.conf yourself.

## What is next (agreed order)

1. **H1 — cached-token scoring.** Plumb
   `prompt_tokens_details.cached_tokens` from the OpenRouter
   response through `llm.Usage` into the bench's usage hook, and
   re-score by estimated dollars (OpenRouter discounts cached
   prefix ~10x). First step: read internal/llm's stream decode to
   see if usage parsing is one path (~15–25 min) or split (~1h).
   The standing overhead sits in the system prompt, i.e. mostly
   cache — the +21% may be nearly free in dollars. Old logs can't
   be re-scored; one fresh benchmark round needed.
2. **Sleep-time consolidation (new, from the literature pass).**
   A background pass over `sessions/` pointer nodes — on session
   start or delegated to the local minion GPU — that merges
   duplicates, promotes oft-read pointers to proper claims with
   evidence, prunes what nothing cited. Fixes recording quality
   with zero chat tokens (Letta's sleep-time compute; Mem0's
   pipeline). In-turn recording stays dumb pointers.
3. **H3 — three-session pairs.** Add a "week later" third session
   per benchmark pair; the standing cost is per-session, the tree
   compounds. If s3 doesn't win either, the honest conclusion is
   the tree doesn't pay for chat and its job is the
   advisor/teaching flow.
4. **H2 — description slimming** (move tool how-to into the cached
   tree section) only if H1 says the gap is real dollars.
5. **A static repo-map baseline round** (aider-style, tree-sitter
   or ctags map frozen in the prompt) to bound how much of the
   tree's value is just "any map present".
6. Separately: the user wants to **run the branch themselves for a
   day** before step 10 (teaching) is built on it.

## Conventions that matter here

- The user's style: 80-column Go, no if-init declarations, 2-space
  indent, plain error wrapping like the existing code. Commits are
  lowercase prose subjects, no AI attribution; this repo does not
  use version numbers in subjects (follow `git log`).
- The bench costs ~$0.30–0.50 per full round (30 sessions on
  OpenRouter, Qwen 3.8 27B). Ask before spending more than one
  round.
- Golden tool definitions live in
  internal/agent/testdata/definitions*.json and are regenerated
  with `go test ./internal/agent -run TestDefinitions -update`;
  they must stay byte-stable per role.
- `run_program` with bash is for necessary commands only (env
  setup, git if needed); prefer the direct tools otherwise. Git is
  not in run_program's allowlist; bash `git` commands work.
- Benchmark traces: use absolute ARMIN_TREEBENCH_DUMP paths.

## Quick numbers table (round 5, 3 runs, input tokens summed)

| | no tree | tree | delta |
|---|---|---|---|
| session 1 | 1351k | 1585k | +17% |
| session 2 | 842k | 1068k | +27% |
| pair | 2193k | 2653k | +21.0% |

Earlier rounds and their postmortems: restructure-tree-economics.md.
