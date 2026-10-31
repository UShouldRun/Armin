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
