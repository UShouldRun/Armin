# Tree economics: what went wrong and what we're trying

Companion to docs/design/restructure-plan.md, which holds the
settled design. This file is the experiment log for one question:
**does the knowledge tree pay for its keep in chat?** Written as we
go; rounds are appended, never rewritten, so the reasoning that
looked wrong stays visible.

The benchmark (internal/think/treebench_test.go): five pairs of real
questions about armin's own code (repeat-guard, golden-tools,
staleness, roles, project-dirs), two fresh sessions per pair.
Session 1 asks about an area; session 2 follows up, where the tree
should spare the model from exploring again. Answers checked by
regex; tokens from the usage hook. Qwen 3.8 27B via OpenRouter,
effort medium, read-only, no memories or skills. Three runs per
round, both conditions; only pair deltas mean anything (single-run
noise is ±30%). Traces: `ARMIN_TREEBENCH_DUMP=<absolute path>`.
The key loads from `~/.config/armin/.env`.

## The question

The tree costs standing tokens (three tool descriptions plus the
tree section on every request, and session-1 recording) and should
pay them back by making session 2 (and every later session in the
area) skip exploration. After five rounds the pair still costs +21%.
Everything below is why, and what the next tries are.

## Round summary

| round | change | pair | s1 | s2 |
|---|---|---|---|---|
| 1 (0471735) | 6 tools, no auto-record, single runs | +17% (722k vs 619k) | +58% | −20% |
| 2 (28d53c5) | 6 tools → 3 | +19.9% | +30% | +8% |
| 3 † | "auto-record" that never fired | +42.4% | +55% | +28% |
| 4 (6af79a5) | auto-record of Q/A pairs | +41.2% | +49% | +31% |
| 5 (642f3a3) | pointers, at-refs, current, find guidance | +21.0% | +17% | +27% |
| 6 (f751386) | none: round 5 scored in dollars (H1) | +5.3% tok, **−0.4% $** | +25% $ | −19% $ |
| 7 (d8cf528) | recall: armin searches, model only writes | +15% $ ‡ | +53% $ ‡ | −17% $ |
| 8 (1491052) | H3: three sessions, recall on | **−17.7% $** | +9% $ | −29% $, s3 −35% $ |

Input tokens, 3 runs summed, tree vs no-tree. Accuracy held
throughout (scattered 3–5/5, never systematically worse; the two
round-5 misses were borderline regex checks on correct-looking
answers).

† round 3 measured nothing new — see the postmortem.

## What went wrong, in order

1. **The first "win" was noise.** Round 1's −20% s2 came from one
   recorded node in one pair (golden-tools) plus single-run noise;
   the 6-tool cost (+58% s1) made the pair a loss regardless. Lesson
   one: three runs minimum, both conditions, pair deltas only.

2. **Merging six tools into three didn't pay.** The descriptions
   ride on every request whether used or not, and merging them cut
   s1's description cost only modestly (+58% → +30%). But it did
   roughly double how often the model records (2 know_add calls in
   5 runs → about one know_write per session-1): the easier surface
   is real, just not cheap.

3. **A whole round measured a feature that was off.** recordTurn
   read a nonexistent argument key (`file`; read_file's argument is
   `path`), so round 3 silently re-measured round 2 — and we read
   its +42% as "auto-record makes it worse" for a while. Lesson two:
   a feature whose effect is "nothing changed" must be proven on
   before its cost is believed. (The test was wrong the same way as
   the code, so it passed.)

4. **Auto-recording Q/A pairs made s2 worse** (+31%, the worst).
   The tree filled with cut question + cut answer blobs under
   `sessions/`; s2 found them and explored the code anyway. A blob
   the model didn't write, with no evidence it chose, it doesn't
   trust. What it *does* reuse: a claim it wrote itself (round 1's
   golden-tools s2: 4 rounds, 35k input, answered from the tree).

5. **Round 5 halved s1 but s2 didn't follow.** Pointer claims
   (the answer's own citations, no prose), the at-ref on every node
   line, the `current` freshness mark, and the trust rule printed
   on find results. The traces show the intended flow working
   (find → find ids → one 106-line read → answer), but only where
   s1's model had written a real node; the auto-recorded pointers
   under `sessions/` mostly weren't what s2 used. s1 dropped to
   +17% — mostly because pointer recording is cheap — but s2's
   +27% says the standing overhead rides whether the tree helps
   or not.

## Where the gap actually is now

- **Standing overhead** (three tool descriptions + tree section +
  outline, every request, every session). Unknown how much of the
  +17%/+21% is this.
- **s1 recording** (model-written claims): pays back only if a
  later session lands in the same area — two sessions may be too
  short to amortize it.
- **s2's usage is fine.** Where the tree had a real node, s2 was
  cheap and fast. The read side (at-refs, current, find guidance)
  is done and costs almost nothing.

## Hypotheses and next experiments

**H1 — the gap is mostly cache-discounted tokens, not dollars.**
OpenRouter serves the cached prefix at a steep discount; the
standing overhead sits in the system prompt (cached), while any s2
savings land on fresh tokens. If true, the tree is already cheaper
than the token table says and chat-side work is done.
*Test:* plumb `prompt_tokens_details.cached_tokens` into the usage
hook (one field through llm.Usage; 15–25 min if the stream decode
has one path, an hour if it's split), rerun one round, re-score by
estimated cost. The old logs can't be re-scored — the field wasn't
captured.

**H2 — the descriptions can move into the cached prefix.** The
how-to (writing rules, edge semantics, dedup protocol) can live in
the tree section of the system prompt instead of the tool
descriptions, leaving one-liners. *Test:* measure the definitions'
byte drop (goldens), one benchmark round.

**H3 — two sessions is too short to amortize.** The standing cost
is per session; the tree compounds. *Test:* add a third session per
pair ("week later" style) and see whether s3 tree beats s3 no-tree.
If it does, the pair framing was the artifact. If it doesn't, the
honest conclusion: the tree doesn't pay for chat and its job is the
advisor/teaching flow, where the plan already gives it structure.

**H4 — the wrong currency.** Tokens are a proxy; what we actually
care about is dollars and wall time. After H1, re-score every round
by cost, not tokens.

Order: H1 first (cheapest, possibly decisive — if the standing
overhead is nearly free in dollars, H2 is moot and H3 is the only
remaining question). Then H3. H2 only if H1 says the gap is real
dollars.

Separately from all of this: the branch is unpushed and untested in
daily use. One real day of the roles plus the tree, by the user,
would tell more than another benchmark round — and anything broken
surfaces before step 10 (teaching) builds on it.

## Costs so far

About $2 across 15 clean runs (12 through round 4, 3 in round 5),
plus one early 401 round and one silent no-op round, both free.

## What the literature and other builders found (2026-10-31)

A research pass over what others doing similar things have learned.
Sources are web results; where a claim matters to a decision, the
paper's own numbers are cited below.

### The memory-systems line (Mem0, Letta/MemGPT, Zep)

The closest neighbors to our tree: persistent memory layers for
agents, evaluated on long-conversation benchmarks (LOCOMO, DMR,
LongMemEval) instead of token cost.

- **Mem0** (arXiv 2504.19413): extract salient facts from
  conversations, consolidate, retrieve on demand. Their LOCOMO
  numbers: ~26% better answer accuracy than OpenAI's memory, with
  ~91% lower p95 latency than stuffing full conversation history
  into context, and large token savings — the selective-retrieval
  trade on LOCOMO was 66.9% accuracy at 1.44s with ~90% fewer
  tokens, vs 72.9% at 17.12s for full context (atlan's summary of
  the paper). The graph variant (Mem0g) beat the flat variant by
  ~2-4% on multi-hop questions — structure helps, a little.
- **Zep / Graphiti** (arXiv 2501.13956): memory as a temporally
  aware knowledge graph; beats MemGPT on the Deep Memory Retrieval
  benchmark and on latency. Their argument matches our design:
  episodes (what happened) plus entity-relation graphs (what's
  true now), with edges invalidated by time rather than deleted.
  Our staleness-by-hash is the code-native version of their
  temporal invalidation.
- **Letta / MemGPT sleep-time compute**: consolidation happens
  *asynchronously, while the user is away* — memory formation is
  incremental and gets messy, so a background agent reorganizes it
  off the critical path. This is the literature's version of our
  "armin records at end of turn, model writes when it chooses":
  move the cost out of the turn. Their reported gain: async
  consolidation improves next-session accuracy without touching
  in-turn latency or tokens.

**What this line says for us**: nobody evaluates memory by
*per-session input-token delta on a two-session pair* — our
benchmark is unusually harsh. The standard metrics are accuracy on
multi-session questions (which our tree condition mostly matched)
plus latency plus total token cost over a whole workload. Also:
the systems that work all do *extraction and consolidation outside
the turn* (Mem0's pipeline, Letta's sleep-time, Zep's async graph
updates). Our round-4 mistake — Q/A blobs recorded in-turn — is
what happens without a consolidation step; Mem0's answer is an
extract-then-consolidate pipeline, not better in-turn recording.

### The coding-agent line (what actually got measured on code)

- **AGENTS.md evaluation** (arXiv 2602.11988): a controlled study of
  repository-level context files — the productized cousin of our
  frozen outline. Finding: context files *encourage broader
  exploration* (more files traversed, more thorough testing) and
  mixed-to-positive accuracy effects; they do not reliably reduce
  tokens, and can increase them, because a guided agent explores
  *more confidently*. A companion ablation (realm-1.5 workshop)
  found effectiveness "contradictory" across setups. This is
  precisely our s2 result: give the model a map and it still walks
  the terrain — sometimes more of it.
- **SWE-Explore** (arXiv 2606.07297): a benchmark isolating *how*
  agents explore repos (navigation, localization), independent of
  patching. It exists because exploration is the unmeasured cost —
  the same gap our benchmark targets. Their framing supports our
  benchmark design: exploration efficiency is a capability to
  measure in isolation, and binary resolved/not-resolved hides it.
- **aider's repo map** (tree-sitter): the practical proof that a
  *compact structural map* (like our outline, but static and
  syntax-derived) is the sweet spot for coding agents — aider
  ships a map computed with graph ranking, not accumulated from
  sessions. Static, recomputed cheaply, no trust question.
- **Agent KB** (arXiv 2507.06229) and **XferAgent**: cross-task,
  cross-agent *experience reuse* — capture execution traces,
  abstract them into reusable strategies, retrieve at planning
  time. Agent KB's key result: retrieving *abstracted lessons*
  (named strategies) at plan time works; retrieving raw traces
  doesn't. This is the strongest external support for our round-5
  finding — pointers/lessons the model can weigh beat blobs of
  transcript.

### The skill/experience line

- **Voyager** (arXiv 2305.16291): the classic result — a *skill
  library* (verified, reusable, executable code) is the memory that
  compounds. Skills are validated by execution before being stored,
  which is why the agent trusts them. Our `tested` confidence level
  and delegation's acceptance-check evidence are the same idea:
  verified-at-write beats trusted-at-read.
- **Case-based reasoning, old and new**: the CBR cycle
  (retrieve-reuse-revise-retain) is the classical formulation of
  what we're building; 2025-26 work re-brands it with LLMs. The
  CBR literature's settled lesson applies directly: **the retrieval
  step is where value lives or dies**, and cases need an index as
  good as the cases themselves. Our BM25+outline is the index;
  round 4 was a case-store with cases nobody wanted.

### Synthesis: five things they found that we can use

1. **Measure like the memory line does** (supports H4, reframes the
   whole table): accuracy over multi-session workloads + latency +
   total-cost-per-workload, not per-pair input tokens. Our benchmark
   shows the tree matching accuracy at +21% pair tokens; Mem0's
   LOCOMO table would call that "competitive on accuracy, cost TBD
   until amortized" — their own wins are amortized over hundreds of
   interactions.
2. **Consolidate outside the turn** (Letta's sleep-time, Mem0's
   pipeline): a background pass — on session start, on the local
   GPU (our minion!), or on close — that distills the sessions/
   pointers into the model-written tree: merge duplicates, promote
   oft-read pointers to proper claims with evidence, delete what
   nothing cited. This replaces "hope the model records well" with
   a scheduled digestion step, and it costs no chat tokens.
3. **Store what's verified, not what's said** (Voyager, our `tested`
   level): pointer nodes with hash-backed `current` are the right
   primitive — the at-ref staleness design is stronger than most of
   the literature, which timestamps facts but doesn't verify them
   against the source. Keep leaning on it.
4. **Abstracted lessons, not traces** (Agent KB): when the
   consolidation pass promotes a pointer to a claim, the claim
   should be a named strategy ("the repeat guard refuses identical
   failed calls in t.run"), not a transcript. Agent KB measured
   exactly this difference.
5. **Static maps are the competition** (aider, AGENTS.md): before
   claiming the tree beats no-tree, compare against the cheap
   alternative — a recomputed repo map in the system prompt, no
   session state at all. If a static map gets most of s2's value,
   the tree's remaining job is the *cross-session* delta, which is
   H3's three-session measurement.

And one cautionary thread: **memory poisoning** (arXiv 2606.24322
and the GraphRAG extraction-attack line) — persistent memory
inherits wrong or hostile content across sessions. Our authorship
bits and minion-verification caps are already the right shape; the
learner tree (user's own knowledge) is the sensitive one.

### What this changes in our plan

- H4 is promoted: the field's currency is accuracy-then-amortized-
  cost, and our two-session pair may be measuring the wrong thing
  *by the field's own standards*. H1 (cached-token scoring) plus H3
  (three sessions) together are the honest measurement.
- New experiment, from Letta: **sleep-time consolidation** — at
  session start (or on the minion between sessions), a cheap pass
  over `sessions/` that merges, promotes and prunes. This is
  plausibly where round 4's failure gets fixed without touching
  the in-turn path: the in-turn recording stays dumb (pointers),
  and the digestion happens off the critical path, on the free
  local GPU.
- The static repo-map baseline (aider-style) is worth one round:
  it bounds how much of the tree's value is just "map present in
  the prompt".

## Round 6: H1, the same design scored in dollars (2026-10-31)

No change to the tree; usage now carries OpenRouter's own per-request
cost and cached tokens (llm.Usage.Cost, PromptDetails.CachedTokens,
asked for with `usage: {include: true}`, OpenRouter only). Three runs,
traces in reports/treebench6/, logs reports/treebench6-run*.log.

| | right | input | cached | output | cost |
|---|---|---|---|---|---|
| no tree s1 | 15/15 | 1155k | 79% | 18.8k | $0.093 |
| tree s1 | 10/15 † | 1432k | 85% | 23.5k | $0.116 (+25%) |
| no tree s2 | 15/15 | 950k | 80% | 13.2k | $0.123 |
| tree s2 | 14/15 | 783k | 82% | 11.6k | $0.099 (−19%) |
| pair | | +5.3% | | | $0.2154 vs $0.2146 (−0.4%) |

**H1, partly wrong in its premise, right in its conclusion.** Qwen
3.8 27B's cached input is $0.02/M against $0.04/M: a 2× discount, not
10×. Input and output cost about the same per session here (input at
$0.04/M, output at $2.30/M). Still, in dollars the pair breaks even:
session 2's savings pay session 1's recording exactly. Session 2 is
19% cheaper with the tree, in dollars and in tokens.

† **The accuracy drop is a scoring artifact.** In all five misses the
model wrote its answer, then called know_write, then closed the turn
with "Recorded as [n3]" or "covered above". The REPL streams every
message, so the user saw the answer; the bench scored only the final
message. It now scores everything streamed in the turn (hooks.Token).
Round 5's scattered misses were likely the same; their full text
wasn't kept, so they can't be re-scored.

**What this changes.** The pair is no longer a loss; the remaining
question is whether the tree *wins*, which needs either a longer
horizon (H3) or a cheaper read side. Proposed next, ahead of
consolidation: **automatic retrieval** (Mem0's shape) — armin runs
know_find on the user's message itself (BM25, no model call) and
puts the top node lines at the head of that message, so reading the
tree costs no tool round and no read-side tool descriptions; the
model keeps only the write tool. Nothing is added when nothing
matches, and the system prompt stays byte-stable.

## Round 7: recall (2026-10-31)

armin searches the tree with each turn's message and attaches up to
three node lines above it (score ≥ 40% of the best); the model loses
know_find and know_trace and keeps know_write (`recall = on`,
ARMIN_TREEBENCH_RECALL=1). Traces reports/treebench7/, logs
reports/treebench7-run*.log.

| | right | cost |
|---|---|---|
| no tree s1 | 15/15 | $0.078 |
| tree s1 | 15/15 † | $0.461, $0.120 without the runaway ‡ |
| no tree s2 | 14/15 | $0.094 |
| tree s2 | 15/15 † | $0.078 (−17%) |
| pair | | +213% as run; **+15% without the runaway** |

Recall (from metrics): attached on 14 of 30 tree turns, which is every
session 2 and no session 1 (the tree is empty there); the model used
what was attached (cited [nN] or read the node's file) in 7 of 14.

‡ **One runaway generation.** project-dirs tree s1 in one run wrote
132k output tokens ($0.35) inside a single know_write call whose
arguments never parsed (`know_write map[]`). Nothing capped a reply.
Each turn now caps a reply at 32768 tokens unless the client sets its
own (agent.maxReplyTokens); it's a gap any tool call could hit, not the
tree's.

† **The scoring bug wasn't fixed in round 6.** The bench recorded the
whole streamed answer but still matched the final message. Fixed now
(regex on run.answer); round 7 re-scored from its saved answers: the
tree is 15/15 in both sessions. Every tree "miss" in rounds 6 and 7 was
this bug. Round 6's saved answers were final messages only, so its
accuracy can't be recovered.

**Reading.** Session 2 lands where round 6 did (−17% against −19%), so
recall reads the tree about as well as know_find did, with fewer
tools; it didn't make session 2 cheaper. Session 1 swings with the
no-tree baseline ($0.078 here, $0.093 in round 6): two sessions on
five pairs is still noise-sized for s1. The honest next measurement
is H3, three sessions per pair, with recall on.

## Round 8: H3, three sessions per pair (2026-10-31)

Recall on, a third "week later" question per pair (the `third` field),
the tree kept across a pair's three sessions. Traces reports/treebench8/,
logs reports/treebench8-run*.log. About $0.58 for 90 sessions.

| | no tree | tree | delta | rounds |
|---|---|---|---|---|
| s1 | $0.114 | $0.124 | +9% | 100 vs 104 |
| s2 | $0.101 | $0.072 | **−29%** | 77 vs 65 |
| s3 | $0.105 | $0.068 | **−35%** | 99 vs 76 |
| all three | $0.320 | $0.263 | **−17.7%** | 276 vs 245 |

Accuracy: tree 44/45, no tree 45/45. Each run on its own: −15.0%,
−23.7%, −13.3% — the first round where every run agrees on the sign.
Recall attached on 27 of 45 tree turns (every s2 and s3, no s1: the
tree is empty there) and was used in 16 of 27.

**H3 confirmed: the tree compounds.** Session 1's recording (+9%) is
paid back in session 2, and session 3 saves more than session 2 did:
fewer rounds (76 vs 99), not cheaper rounds. Two-session pairs were too
short to show it; the "+21% pair" of rounds 2-5 was measuring the
cost of a tree before it had anything to give back.

What this settles and what it doesn't:
- **Settled:** with recall, the tree pays for itself in chat from the
  second session in an area, and pays more the more sessions land
  there. Recall stays on as the design.
- **Not settled:** a static repo map (aider-style) as the baseline, to
  bound how much of this is "any map in the prompt"; and consolidation
  between sessions, which should lift s1's recording quality and the
  27-of-45 attach rate. Both are refinements now, not the question of
  whether the tree pays.

