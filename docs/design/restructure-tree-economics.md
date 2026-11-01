# Tree economics: what went wrong and what we're trying

Companion to docs/design/restructure-plan.md, which holds the
settled design. This file is the experiment log for one question:
**does the knowledge DAG pay for its keep in chat?** Written as we
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
| 9 (5c05413) | consolidation between sessions, charged to tree | **+10.2% $** | +13% $ | −28% $, s3 −20% $ |

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

**Claims compound, pointers don't** (found by the armin-test copilot,
Qwen 3.8 27B, reading run 1's s3 traces; one run, to be checked across
all three). The s3 saving tracks what session 1 left in the tree:
where the model wrote a claim (`decision`, `fact`), s3 answered from it
in 1–3 tool calls (golden-tools $0.0007 vs $0.0025, staleness $0.0026
vs $0.0054); where session 1 left only armin's auto-recorded pointer (a
`file` node listing file:line refs), s3 re-explored with 7–10 tools and
saved little (project-dirs ~10%). The round 5 lesson, that the model
reuses what it wrote itself, extended to the third session. It points
the next experiment at recording quality: consolidation that promotes
pointers to claims, not recall's knobs. The node kind already tells
the two apart, so the split can be measured per round.

**Checked across all 15 sessions** (the armin-test copilot, from the
traces): session 1 left a model-written claim in 4 of 15 and only an
auto-recorded pointer in 11. Session 3 saved −56% where s1 left a
claim, −30% where it left a pointer (range −83% to +65%). The direction
holds, with two qualifiers, the second found checking its reading:
- s3's saving tracks how much the no-tree s3 explores; where that's
  already cheap (≤ $0.0032) there's little to save either way.
- **project-dirs' third question is off-area.** Sessions 1 and 2 are
  about chat sessions (internal/session); the third asks where the
  knowledge DAG's files live (internal/know). The s1 pointer covers
  internal/session correctly and has nothing on internal/know, so that
  pair doesn't test compounding; its +65% run is noise on a question
  the tree couldn't cover. The question should move to the same area
  before the next round (e.g. "what is the session file's first line
  and what does it hold").

## Round 9: consolidation between sessions (2026-10-31)

The round 8 pointer: promote s1's auto-recorded pointers (a `file`
node whose claim is a list of file:line refs) into model-written
claims, off the turn on the minion (internal/agent/consolidate.go,
`Consolidate`/`consolidateOne`, commits 5c05413 + 23c1adb). In the
bench it runs between a pair's sessions with the bench's model as the
minion, its cost charged to the tree (treebench_test.go:194-197,
benchConsolidate, ARMIN_TREEBENCH_CONSOLIDATE set).

| | no tree | tree | delta |
|---|---|---|---|
| s1 | $0.1141 | $0.1290 | +13% |
| s2 | $0.1013 | $0.0726 | −28% |
| s3 | $0.1040 | $0.0827 | −20% |
| all three | $0.2855 | $0.3146 | **+10.2%** |

Per run: +4.4%, −3.6%, +32.6%. Traces reports/treebench9/, logs
reports/treebench9-run*.log. **Round 8 was −17.7%; consolidation made
it worse.** Accuracy tree 45/45, no tree 43/45.

Where the money went, per run (pair costs already carry the
consolidation charge, $0.0065 ± 0.001/run, ~41 calls over three runs
at ~$0.0005 each — the pass itself is not the problem):

- **run 3 is the whole regression.** No runaway session this time
  (largest tree turn $0.0164, in 118958, out 1758); instead the
  plain side got lucky: its three cheapest sessions of the round are
  all run 3 (golden-tools s1 $0.0040 in 22944, roles s3 $0.0110,
  repeat-guard s2 $0.0065), while tree s1 ran hot everywhere
  (staleness $0.0164, roles $0.0159, project-dirs $0.0159).
  No-tree s1 totals: run 3 $0.0320 vs run 1 $0.0394, run 2 $0.0394.
- **s1 got 13% more expensive** (matches round 8's +9% plus
  consolidation's charge), and s3's −35% from round 8 fell to −20%.

Did consolidation work mechanically? Yes, on its own terms: ~41
pointers consolidated into 51 claims + 4 links across the three
runs (13-14 calls each, 1-2 empty), the promoted claims cite ranges
that were actually shown, and run 3's staleness s3 answered largely
from the tree's new `fact` nodes (8 tools vs plain's 6 — but see the
next section). But it did not move the two numbers that matter:
**s3 tools tree 76 vs plain 78** (round 8: 76 vs 99), and s3 dollars.

Two readings, and the data does not separate them yet:

1. **Round 9 hit the "cheap plain s3" ceiling from round 8's second
   qualifier.** The pairs where plain s3 is already cheap (golden-tools,
   repeat-guard: $0.0022-0.0037 in every run) leave nothing to save;
   in round 8 the saving lived in the pairs where plain s3 explored
   (staleness 12-15 tools, roles 8-14), and in round 9 those same
   pairs' plain sides got cheap too (staleness plain s3: 6, 6, 11;
   roles: 8, 8, 9 vs round 8's 14, 8, 8). The variance is the confound.
2. **Consolidation's claims arrived too late to help s3 in the pairs
   that needed them** — the pass runs between s1 and s2 with at most 10
   pointers per call and 3 claims per pointer, and the promoted claims
   answer *s1's question and its follow-up* (consolidatePrompt:
   "the follow-up most likely after it"), not s3's week-later question.

What round 9 does settle: consolidation as built does not pay for
itself in the bench, and the round 8 write-up's "refinements now"
verdict holds for it. The next measurement should fix the round 9
confound before judging the idea: same round again, but the
*consolidation pass is what s3 sees* (claims written between s2 and
s3, from s2's pointers too), and the project-dirs third question
moved on-area as round 8 flagged. If s3 tools don't drop there either,
the pass stays as hygiene (dedupe, at-refs) and the money moves to
what round 8 actually showed paying: recall attach rate, which held
at 10/15 turns and 5-10 used in round 9.


## The static repo-map baseline (built 2026-11-01)

The round 8 open question: how much of the tree's value is just "a
map in the prompt"? `internal/repomap` builds aider's repo map without
tree-sitter or a graph: the definitions Universal Ctags finds
(functions, types, constants; no variables or fields, no tests), each
file ranked by how often the rest of the project names what it
defines (an unexported Go name counted only inside its package, a
name defined in k files split k ways), the top 4 per file, cut to
8000 bytes (~2k tokens). On armin itself: 84 of 269 files, built in
0.25 s, byte-stable across sessions.

The bench gained a third arm, `map`: no tree, the map in the system
prompt (`mapSection`, think/prompt.go). `ARMIN_TREEBENCH_ARMS` picks
arms (default `plain,tree,map`); PAIR COST prints each arm against
no tree. One run of plain + map costs about $0.20 on Qwen 3.8 27B;
all three about $0.30 (round 9: ~$0.10 per arm per run).

## Round 10: the map against no tree (2026-11-01, one run)

`ARMIN_TREEBENCH_ARMS=plain,map`, Qwen 3.8 27B, one run, $0.21.
Traces reports/treebench10/, log reports/treebench10-run1.log.

| | no tree | map | delta |
|---|---|---|---|
| s1 tools / $ fixed-price | 48 / $0.0244 | 33 / $0.0220 | −31% / −10% |
| s2 | 32 / $0.0166 | 24 / $0.0158 | −25% / −4% |
| s3 | 35 / $0.0169 | 28 / $0.0145 | −20% / −14% |
| all three | 115 tools, $0.0579 | 85 tools, $0.0524 | **−26% tools, −9.5% $** |

Billed: $0.1056 against $0.1037, −1.8% (OpenRouter's cache hits swing
the billed figure per request; fixed-price reprices every run at
$0.04/M input, $0.02/M cached, $2.30/M output). Input −10%, output
−10%, rounds 94 → 73. Accuracy 15/15 against 14/15 by regex, but the
miss (project-dirs map s2) is a scoring artifact: the answer is right
and says "first 8 hex chars of the SHA-256", the check wants `hash`.

What it says, with one run's caveat:

- **The map saves from the first session**, where the tree costs:
  s1 tools −31% here, against the tree's +23% in round 9 (150 vs
  122). The map has nothing to learn; the tree pays its recording up
  front.
- **The map's saving holds flat across sessions** (−31%, −25%,
  −20% tools), as a static map should. The tree's grows by session
  (round 8: s2 −29%, s3 −35% $) but starts negative.
- So the two answer different things, and they combine: the map is
  the cheap floor on every session, the tree the cross-session delta
  on top. The next measurement is the tree *with* the map against the
  map alone, which isolates exactly what the tree adds.
- Caveats: one run (round 9's runs spread −3.6% to +32.6%); the
  question text changed from "knowledge tree" to "knowledge DAG"
  since round 9; and the map's ~2k tokens ride on every request,
  which is why input falls only 10% while tools fall 26%.

## Round 11: the tree with the map, against the map alone (2026-11-01, one run)

`ARMIN_TREEBENCH_ARMS=map,treemap`, recall on, consolidation between
sessions (the bench's hosted model as the minion, charged to the
tree), Qwen 3.8 27B, one run, $0.22. Traces reports/treebench11/, log
reports/treebench11-run1.log. Same map as round 10 (7917 bytes, 84
files).

| | map | tree+map | delta |
|---|---|---|---|
| s1 tools / $ fixed-price | 34 / $0.0248 | 43 / $0.0269 | +26% / +8% |
| s2 | 28 / $0.0148 | 23 / $0.0144 | −18% / −2% |
| s3 | 20 / $0.0130 | 24 / $0.0159 | +20% / +22% |
| all three | 82 tools, $0.0527 | 90 tools, $0.0573 | **+10% tools, +8.8% $** |

Billed: map $0.1093, tree+map $0.1147, **+4.9%**, of which
consolidation is $0.0117 (9 calls, 27 claims, none empty or failed
now that the off-by-one is fixed). Sessions alone billed −5.8%; in
armin consolidation runs on the local minion and costs nothing, so
that is the shipped figure, but fixed-price says +8.8%.

Accuracy: map 15/15, tree+map 14/15, and **the miss is real**:
staleness s3 ("where does armin keep the stamps of the files the
tree cites") was answered about evidence range hashes, citing the
recalled [n5] and [n10], instead of the stamps file. The recalled
nodes were about the neighbouring mechanism and steered the answer
there. Recall: attached 10 of 15 turns, cited 5, read only 4.

What it says, with one run's caveat:

- **With the map present, the tree adds nothing measurable.** The
  map already takes the first-session saving (round 10: −31% tools)
  that the tree can't; the tree's s2 saving shrinks to −2% at fixed
  prices, and s3 went the wrong way (roles s3 explored 12 tools with
  the tree, 7 without).
- **Recall can mislead**: a close-but-wrong node is an answer the
  model takes, where without it the model would have looked.
- Taken with rounds 8–10: the map is the cheap, reliable win and a
  candidate for on by default; the tree's case for chat is now
  unproven on top of it. Its remaining job is the one the handoff
  named: the advisor and teaching flow, and what no static map
  holds (decisions, why, what was tried), not where code is.
- Before more rounds: one run per arm has swung ±30% before (round
  9), so a second run of this pair is what would make it a finding
  rather than a lead.

### Round 11, run 2, and both runs together

Run 2 (log reports/treebench11-run2.log): billed map $0.1115,
tree+map $0.1125, +0.9% with consolidation's $0.0148 (sessions alone
−12.4%); fixed-price +1.6%; tools 101 → 82; accuracy 15/15 both.
Run 1 had been +4.9% billed, +8.8% fixed, tools 82 → 90.

Both runs summed, map against tree+map:

| | tools | fixed-price $ | billed $ | right |
|---|---|---|---|---|
| s1 | 79 → 86 | +8% | +11% | 10/10, 10/10 |
| s2 | 55 → 47 | +1% | −22% | 10/10, 10/10 |
| s3 | 49 → 39 | +3% | −29% | 10/10, 9/10 |
| all | 183 → 172 (−6%) | **+5.0%** | sessions −9%, with consolidation ≈ +3% | 30/30, 29/30 |

So two runs agree on the shape, and it is break-even, not a win:

- The tree still costs in s1 (+8%) and still spares tool calls
  later (s2 −15%, s3 −20%), but with the map in place the saved calls
  no longer pay for the tree's own text: fixed-price s2 and s3 are
  +1% and +3%. The billed figure says −22% and −29%, but billed swings
  with OpenRouter's caching per request; the fixed-price reprice is
  the one that compares like with like.
- Charged nothing for consolidation (it runs on the local minion in
  armin), the tree is at best a few percent ahead in billed dollars
  and a few percent behind at fixed prices: within one run's noise
  either way.
- One wrong answer in 30, steered by recalled nodes (run 1, above).

**Verdict for chat:** the static map is the default-worthy part
(round 10, −26% tools, −9.5% fixed-price, from session 1). The tree
on top of it doesn't pay for itself in this benchmark; keep it for
what the map can't hold (decisions, why, what was tried) and for the
advisor and teaching flow, and don't sell it as an exploration
saver.
