# Restructure plan: roles and knowledge trees

Status: **steps 1-9 built** on branch `restructure`; step 10 (teaching)
and 11 (end-to-end check) remain, plus the open tree-economics question
in "Does the tree pay?" (section 5). Section 9 says exactly where
things stand and what's left.

Inspired by LearnHarness (amosblomqvist/learnharness): a persistent
knowledge graph of what the learner knows, built from teaching
sessions. armin takes the idea further: the model keeps a tree of
what *it* knows about the project, and the advisor keeps one of what
*you* know.

Constraints for everything below: data structures as compact as
possible, the most efficient algorithms that fit, no dependencies
beyond `golang.org/x/sys`, and no JSON for the trees.

## 1. Roles — settled

| role | runs on | for | tools |
|---|---|---|---|
| minion | local GPU | searching, small edits, nvim completion; you talk to it directly too | read/search, small edits, FIM |
| copilot | hosted (OpenRouter) | large searches, reasoning, large changes | the full table + `delegate` |
| advisor | hosted (OpenRouter) | key decisions, learning a topic, a codebase, a new project's concepts | read-only code, web, library, teaching tools + `delegate` |

- **Model per role is yours** in `config.conf`; nothing hardcoded.
- **Copilot and advisor work like `models.planner` today**: switched
  mid-conversation (`/copilot`, `/advisor`, back with `/minion` or
  `/model`).
- **A role is a tool permission mask**, enforced in armin's tool
  layer for every frontend (REPL, web, LSP, `-mcp`). A switch also
  appends a short role-reminder message instead of rewriting the
  system prompt, so the cached prefix stays valid up to it.
- `Role` is a `uint8` bitmask; each tool spec gets a `roles` mask;
  definitions are computed once per role and cached (byte-stable per
  role, one golden file per role).

### Delegation

`delegate(task, check)` runs a minion turn on the local GPU.

- Before delegating, the copilot/advisor must rule out that a script
  or a deterministic tool does it better.
- The delegator gives an **acceptance check**: a test, a command, or
  a property the result must satisfy. The task should be one a
  minion does correctly almost every time.
- Once handed over, the minion is on its own (read tools; edits only
  when the delegator allows them).
- The result comes back with the check's outcome and a confidence
  level.
- What a minion contributes to a tree is **authored by the minion**.
  Only a copilot or advisor can set its `verified` flag, and the
  authorship stays the minion's, so a wrong conclusion can be traced
  back to it.

armin's small internal helpers (effort classification, titles,
library cataloging) become minion jobs while chat runs on a hosted
model (free, and the GPU is idle). Those that read the whole
conversation (compaction summaries, verify, suggestions) stay on the
chat model: there it is already in the provider's prompt cache, and a
9B would read 50k+ tokens cold and summarize a bigger model's work
worse. (Changed while building step 6.)

## 2. The graph — settled

Two stores, one type:

- **project tree**: what armin knows about this codebase and its
  topic, `~/.local/share/armin/know/<project>-<hash>/`
- **learner tree**: what you know, one per user, organized by
  topic and shared across projects, `~/.local/share/armin/learner/`

**Shape**: a DAG where every node also has one primary parent. The
parent links form a tree for outlines and paths (`net/tcp/handshake`);
the extra edges carry dependencies.

**Node kinds** (3 bits): topic, concept, fact, decision, file, symbol,
question.

**Confidence** (2 bits), levels not numbers: guess, inferred,
checked, tested.

**Edge kinds** (3 bits): requires, implements, explains, contradicts,
derived-from.

**Authors** are interned once per (role, model, session, delegation)
and referenced by a `uint16`.

## 3. Layout and storage — settled

IDs are array indexes. Strings live in one arena (uvarint length +
bytes); records hold 4-byte offsets.

```go
type Node struct {        // 20 bytes
  Parent      NodeID      // uint32
  Name, Claim StrRef      // uint32 arena offsets
  Born, Seen  uint16      // days since 2026-01-01
  Author      AuthorID    // uint16
  KindConf    uint8       // kind 3 bits, confidence 2 bits
  Flags       NodeFlags   // stale, suspect, pinned, user-edited,
}                         // verified, deleted

type Evidence struct {    // 16 bytes, sorted by Node
  Node NodeID
  Ref  StrRef             // "internal/agent/turn.go:120-160"
  Hash uint32             // rolling hash of the referenced lines
  Kind RefKind            // file range, commit, library page, url,
}                         // session message, check result

type Event struct {       // 12 bytes, learner tree only
  When uint32             // unix minutes
  Node NodeID
  Bits uint8              // kind 2, result 2, confidence 2
}
```

- **Edges**: CSR, one `[]uint32` of offsets and one of targets, the
  kind in a target's top 3 bits (29-bit IDs): 4 bytes per edge. The
  reverse CSR is built at load. In-session edges go to a delta list,
  rebuilt into CSR past ~1k.
- **Path lookup**: open-addressing table on `hash(parent, name)`,
  8-byte slots at ≤ 50% load, ~16 bytes per node.
- **Footprint** at 10k nodes, 20k edges, 15k evidence: ~1.8 MB.

On disk, per tree:

```
graph.snap  header (magic "ARMK", version, counts, section offsets)
            + the node, edge, evidence and author arrays and the
            string arena, exactly as in memory, little-endian
graph.log   "ARML" + generation, then append-only ops:
            [op u8][len uvarint][payload][crc32]
lock        flock'd by the armin that writes; others open read-only
stamps      size, inode, mtime and ctime of each cited file at the last
            staleness check
events      (learner) raw 12-byte Event records, append-only
```

- **Load**: mmap `graph.snap` (`x/sys/unix`) and use the arrays in
  place, then replay `graph.log` into the delta. Replay stops at the
  first bad CRC (a torn write).
- **Write**: one log record per op, O(change).
- **Compaction**: at session end or when the log passes half the
  snapshot; written to a temp file and renamed.
- **`armin -know dump [path]`** prints a subtree as text;
  **`-know edit`** loads an edited dump back.

The 32-bit hash is only ever compared with the same evidence's
earlier value. Its limits: about 1 in 300,000 sessions misses a
change (a one-byte edit is always caught); FNV can be collided on
purpose by a hostile repo; and it must never be used as an identity
or dedup key.

## 4. Algorithms — settled

**a) `know_find(query, under?)`**: BM25 over name + claim with the
library's tokenizer. Postings in CSR with the term frequency in 3 top
bits; node length a `uint8`. Term-at-a-time with a sparse accumulator
(O(postings)), top-k by a fixed heap (k ≤ 20). Subtree filter by
Euler-tour intervals (two `uint32` per node, one DFS at load); nodes
added mid-session walk up their parents. Partial words match by
prefix: the term dictionary is kept sorted and a query term expands
to its prefix range by binary search (~4 B per distinct term). No
trigram index (decided: the model reuses names it has seen, and
trigrams wouldn't help with synonyms anyway).

**b) Dedup on write**: candidates are the siblings plus the top 5
BM25 hits; Jaccard over sorted term-ID sets (merge, O(a + b)). Above
0.6 the tool returns `similar: 42 tcp.handshake "…" (0.71) → link,
update, or add anyway?` and the model decides. No extra LLM call,
and no automatic merge at any score (decided: word overlap can't
tell "accepts" from "rejects", and a merge would drop an author and
its evidence).

Writing rules, stated in the `know_write` tool description and the
procedure aid, and enforced in code where they can be:
- **Search before adding.** Check that no node already holds the
  same information; prefer updating or linking it.
- **Every concept has a definition.** A `concept` node's claim is its
  definition; `know_write` refuses a concept with an empty claim.
- **Reasoning goes into edges.** When a conclusion follows from other
  nodes, say so with `requires` / `derived-from` / `explains` edges
  instead of leaving the link in prose or the scratchpad, so the tree
  carries the logic and not only the facts.

**c) Staleness at session start**:
1. changed files: each cited file's stamp (size, inode, mtime, ctime)
   against the last check's, so no version control is needed (git,
   Mercurial, Jujutsu, Subversion, Fossil or none); ctime can't be
   set by tools, so a replaced file with the old mtime is still caught
2. a path → evidence index finds the evidence touching them
3. each changed file is read once and hashed per line (FNV); a
   range's hash is a polynomial rolling hash over its line hashes
4. on a mismatch the same-length window is rolled across the file in
   O(lines): found elsewhere → the reference is moved (a log op),
   else the node is marked **stale**
5. BFS over reverse `requires` / `derived-from` marks dependents
   **suspect** (visited as a bitset)

Cost: O(changed evidence + affected subgraph).

Re-anchoring (step 4) searches the **same file only** (decided). Code
moved to another file goes stale and the model fixes the reference.

**Later, with Poneglyph** (`~/Code/codegraph`): once it builds a
real code graph, evidence about code anchors to a Poneglyph function
or type node instead of a line range, and cross-file moves become
cheap:
- **Moves**: Poneglyph keeps a content hash per function. A moved,
  unchanged function keeps its hash, so a hash → symbol lookup
  finds it in any file in O(1). No rolling scan over changed files.
- **Semantic staleness**: Poneglyph tracks semantic dependencies
  (a callee's change can change a caller's meaning, even when the
  caller's text didn't change). Its invalidation set marks nodes
  suspect more precisely than the textual BFS over `derived-from`.
- **Facts as evidence**: a Poneglyph finding (rule, witness path,
  premises, revision) can be a node's evidence. Its precision tiers
  map onto confidence: exact → checked, inferred → inferred,
  unknown → guess.
- **Revisions**: Poneglyph stamps results with a graph revision;
  evidence would store that instead of a commit, matching its rule
  that stale evidence never appears current.

To keep that door open now: `RefKind` reserves a `symbol` kind, and
staleness sits behind one function (changed evidence in → stale and
moved references out), so the line-range method can be swapped.
Poneglyph's planned binding for armin is purego, a dependency;
reading its IR file or calling its CLI keeps armin dependency-free.
Decide when Poneglyph exists.

**d) `know_trace(id)`**: BFS back along `derived-from` to a depth
limit; listed with unverified minion nodes first, then lowest
confidence, then stale.

**e) Cycles in `requires`** (decided: accept, then force a fix):
- A write that closes a cycle is **accepted**, and the cycle is
  recorded as open (a `question` node linking its members, marked
  as a cycle).
- A **cycle pass** finds every loop: Tarjan's strongly connected
  components over `requires`, O(V + E), run after each batch of
  writes in a turn (only when a `requires` edge was added) and at
  session start. Every component with more than one node is a loop.
- **Every loop must be resolved by the model**, in one of two ways:
  - **make it atomic**: merge the members into one node (their
    evidence, edges and authorship carried over with
    `derived-from` back to the old nodes)
  - **reframe it**: flip the wrong edge, or turn a mutual
    dependency into `explains`
- **A graph with an open cycle isn't accepted**: `know_plan`
  refuses while one exists in the subgraph it needs, the turn-end
  reminder lists every open cycle, and the session doesn't end
  cleanly (compaction keeps them flagged) until each is resolved.

**f) Learning plan**: the `requires` closure of the goal, stopping
at known nodes; Kahn's sort on that subgraph (in-degrees in a scratch
`[]uint16`), O(V + E) of the subgraph. The frontier (all
prerequisites known) is taught next.

**g) Learner status** (decided: fading, read-only): a Leitner box
(0–7) and the last answer's day, 4 bytes per node. Right moves up a
box, wrong resets to 0; fading when `now − last > 1 day << box`,
checked lazily. A faded concept counts as "not sure" when a plan is
built and gets a quick check question; no review schedule, no
reminders.

**Crystallized** knowledge never fades (counting, arithmetic,
English): a flag bit in the same 4 bytes. Proposed ways in:
- **declared**: you say so, or the advisor's probe establishes it
  as a foundation ("basic algebra: yes")
- **earned**: a concept in box 7 answered right after its full
  128-day gap
- **use refreshes prerequisites**: a right answer on a concept also
  refreshes the last-seen day of what it `requires` (doing
  multiplication exercises addition), without moving their boxes

**Accuracy score** on crystallized concepts, 0–50 (2% steps, 6 bits
of the same 4 bytes: box 3 + crystal 1 + day 16 + score 6 = 26):

```
target = 50 if right else 0
d      = (target − s) / 8, rounded away from zero  // |d| ≥ 1 unless s == target
s'     = s + d
```

An answer weighs 1/8 and the score always moves (a plain
`floor((100·a + v) / 101)` would get stuck, since one answer moves it
by less than a step). Starts at 50 when declared, from the concept's
history when earned. 50 → 43 → 37 → 32 → 28 takes four wrong answers
in a row.

Below **30** (60%) the concept stays crystallized but counts as "not
sure" in plans, and the advisor recommends a refresher on it (the
one proactive prompt in the learner tree). The refresher's check
answers move the score back up.

Open questions:
1. ~~BM25 only, or a trigram index?~~ BM25 + prefix lookup.
2. ~~Also auto-merge above 0.9?~~ No: 0.6, the model decides, plus
   the writing rules above.
3. ~~Re-anchoring moved code?~~ Same file only; cross-file via
   Poneglyph later.
4. ~~A `requires` cycle: refuse or accept?~~ Accept, find with a
   cycle pass, and the model must resolve each (atomic node or
   reframe) before the graph is accepted.
5. ~~Fading at all?~~ Yes, read-only, with crystallized knowledge
   that never fades.

## 5. Using the trees in context — settled

**Outline (decided)**: the system prompt gets a depth-2 outline of
the project tree with per-branch node counts, frozen at session
start and capped at ~300 tokens, so the prefix cache holds; deeper
levels through `know_find` / `armin -know dump`, new nodes through tool
results. The advisor's slot also carries a depth-2 outline of the
learner tree for the current topic (known / not sure).

**Node lines in tool results (decided)**: one line per node, fixed
field order, plain words instead of one-letter codes or arrows (words
tokenize as cheaply, need no legend, and small models misread arrow
direction), unset fields left out:

```
n42 tcp/handshake fact checked minion-unverified needs n17 n23 used-by n8 "three segments: SYN, SYN-ACK, ACK; the server…" STALE
```

IDs in the same `n42` form the model cites. Claims cut at ~100
characters; `know_find` with `ids` returns the full node.

Measured (internal/know/format_eval_test.go, 190-node fixture) against
a coded format (`142 path F:t m? ←103,91 →199`):

| model | set | plain | coded |
|---|---|---|---|
| Qwen 3.8 27B | 20 basic | 20/20 | 20/20 |
| Qwen 3.8 27B | 28 moderate | 28/28 | 25/28 |
| Qwen 3.8 27B | 3 misses × 3 reruns | 9/9 | 6/9 |
| Qwen3.5-9B | 20 basic | 20/20 | 19/20 |

Coded saves 5-7% of input but cost the 9B 10% more thinking. Its
misses, read from the replies: the model's own reading of an arrow
("A →B": A points to B) beat the legend (B depends on A); directions
mixed mid-traversal (down a ← edge, then up a → one); and a list
bound to the adjacent line with a near-identical bare number (125's
→146 read as 126's). Plain words keep their meaning on every line
and `n42` plus a word before each list keeps lists on their line.
Plain stays.

**References (decided)**: in internal text (scratchpad plans,
compaction summaries, `delegate` tasks) the model writes `[n42]`
instead of restating a node; compaction keeps findings by reference,
so it loses nothing and shrinks further. In answers to you it writes
a brief plain statement plus the reference ("the handshake has three
segments [n42]"). The front ends expand references: the REPL shows
`[n42]` dimmed with its path, the web UI shows the node on hover and
opens it in the tree on click, nvim shows it on hover.

**Where notes go (decided)**, by what the note is about:
- how you want things done → **memory** (no evidence needed)
- how to do a task → **skill**
- what is true about the project or topic → **tree** (the only
  store checked for staleness)
- what's being done right now → **scratchpad**, pointing into the
  tree with `[n42]` instead of copying

The procedure aid opens with "check the tree before exploring:
`know_find` first, read code only for what it doesn't cover or marks
stale". `memory_save` refuses notes that cite a file, line or symbol
and points to `know_write`. Existing memories that are code facts move
once with `armin -know import-memories`: each is shown with a
proposed node, you accept or skip, accepted ones leave memory.

**The minion and the tree (decided)**:

| use | outline | read (`know_find`, ids) | write (`know_write`) |
|---|---|---|---|
| you talk to the minion | yes | yes | yes, always unverified |
| `delegate` | no, the task carries the referenced nodes | yes | only if the delegator allows it |
| nvim completion | no | no | no |

Minion writes: confidence capped at `inferred` (`checked` only when a
delegation's acceptance check passed, with the result attached as
evidence); no `decision` nodes; no verifying; no resolving cycles
(merging or reframing concepts is left to the copilot or advisor).

The main saving is input tokens and tool rounds (no re-reading what's
understood); output shrinks mostly through references.

### Does the tree pay? (measured 2026-10-31)

`internal/think/treebench_test.go` measures it: five pairs of real
questions about armin's own code (repeat-guard, golden-tools,
staleness, roles, project-dirs), two fresh sessions per pair, run
concurrently per condition. Session 1 asks about an area; session 2
follows up on it, where the tree should spare the model from
exploring again. Answers checked by regex; tokens from the usage
hook. Qwen 3.8 27B via OpenRouter, effort medium, read-only, no
memories or skills. `ARMIN_TREEBENCH_DUMP` writes per-session traces
(paths are package-relative: pass an absolute path). The key loads
from `~/.config/armin/.env` (0471735).

Four rounds, three runs each, input tokens summed over runs:

| round | s1 no tree | s1 tree | s2 no tree | s2 tree | pair |
|---|---|---|---|---|---|
| 6 tools, no auto-record (0471735) | 293k* | 463k (+58%) | 325k* | 259k (−20%) | 722k vs 619k |
| 3 tools, no auto-record (28d53c5) | 1061k | 1383k (+30%) | 900k | 969k (+8%) | +19.9% |
| 3 tools, auto-record never fired† | 1005k | 1562k (+55%) | 900k | 1150k (+28%) | +42.4% |
| 3 tools, auto-record live (6af79a5) | 1202k | 1790k (+49%) | 869k | 1134k (+31%) | +41.2% |
| trust design (642f3a3, round 5 below) | 1351k | 1585k (+17%) | 842k | 1068k (+27%) | +21.0% |

\* single runs, no repetition. † recordTurn read a nonexistent
argument key (`file`; read_file's is `path`), so this round silently
re-measured the 3-tool state; kept as a lesson: a feature whose
effect is "nothing changed" must be proven on before its cost is
believed. All tree-condition accuracy was 3–5/5 per session-1 and
4–5/5 per session-2, scattered, never systematically worse.

**Findings, from the traces (reports/treebench*/)**:

1. **The tree pays exactly where the model wrote a real claim.**
   golden-tools tree s2 with a recorded node: 4 rounds, 35k input,
   answered from the tree. Without a recorded node the model calls
   `know_find`, gets nothing useful, and explores anyway.
2. **Three tools didn't lower the s1 cost** (recording + description
   overhead sits on every request; +30% with descriptions alone).
   But the merged `know_write` roughly doubled how often the model
   records (about one write per session-1 now, 2 calls in 5 runs
   before) — the easier surface is real, just not cheap enough.
3. **Auto-recording Q/A pairs does not pay.** A cut question plus a
   cut answer (`Q: … A: …`) under `sessions/` filled the tree (all
   15 session-1s recorded), s2 searched and found them (1–4
   `know_find` calls), and still explored the code: +31% s2, the
   worst yet. The model doesn't trust a blob it can't weigh; a
   claim it wrote itself, with evidence it chose, it reuses.
4. **Run-to-run noise is large** (no-tree s1 ranged 317k–497k in
   one round); three runs is the minimum, and only pair deltas mean
   anything.

**Where this leaves the tree (open, not settled)**: recording is
the model's job (`know_write` with a real claim), and armin's
auto-record should at most note *where to look* (a `file`-kind node
with the ranges read, no Q/A text), so s2's first read is cheap and
targeted instead of a search. The next measurement: strip the Q/A
claim to the ranges alone, keep everything else, and see whether s2
drops below no-tree. If it does, the tree's value is navigation, not
prose; if it doesn't, the tree only pays in long chains (three-plus
sessions) and the benchmark should grow a third session to show it.
Costs so far: about $1.50 in OpenRouter across 12 clean runs.

**Round 5 (642f3a3), the trust design** — four changes aimed at the
misses above, all in `line.go`, `tools_know.go`, `prompt.go`:

- **Pointers, not prose**: `recordTurn` records a `file`-kind node
  whose claim is the answer's own citations (regex over `x.go:12-34`
  patterns), falling back to the ranges read. No Q/A text.
- **`at`-ref on the line**: every short node line carries its first
  file evidence (`at tcp/conn.go:10-20`), so find → read is one
  round; a node's evidence no longer costs a second call to see.
- **`current` mark**: a node with file evidence that is neither
  STALE nor SUSPECT says `current` — it provably passed the
  staleness check. The one thing a re-read can't beat.
- **The rule at the point of decision**: `knowRules` states the
  current/STALE contract and the at-ref's meaning, and every
  `know_find` with hits ends with "answer from these where none is
  STALE or SUSPECT (current = passed today's file check); read code
  only for what they don't cover, starting at their at-lines".

Results (3 runs, tokens summed):

| round | s1 no tree | s1 tree | s2 no tree | s2 tree | pair |
|---|---|---|---|---|---|
| 5: trust design | 1351k | 1585k (+17%) | 842k | 1068k (+27%) | +21.0% |

The best round yet — s1's overhead halved (+49% → +17%), accuracy
held (two s2 misses in one run, both borderline regex checks on
correct-looking answers, not tree-induced), and the traces show the
intended flow working: s2 finds the s1 node, reads its `at`-ref
range, and answers in four calls (staleness-tree-2:
find → find ids → one 106-line read → done). But s2 still spends
more than no-tree: the pointer nodes under `sessions/` mostly
weren't what s2 used — the model's own s1 nodes carried the saving;
where s1 wrote nothing, s2 explored as before. And the +27% s2 gap
says the description overhead still rides on every s2 request
whether the tree helps or not.

**What round 5 settles**: the read side is done — `at`-refs,
`current`, find-with-hits guidance all work and cost almost
nothing. The remaining gap is standing overhead (descriptions +
outline on every request) and s1's recording. The levers left:
slim the tool descriptions and the tree section (move the how-to
into the cached prefix), measure with the cached/uncached token
split (OpenRouter bills cached prefix at a discount; the +17% may
be nearly free in dollars), and try three-session pairs — the
standing cost is per session, the tree compounds across them.

## 6. The advisor flow — settled

**Probe (decided)**, in two stages:
1. **self-report** per top-level area: never heard / heard of it /
   used it / could explain it. The two ends settle the area at once.
2. **quiz the middle** by bisecting `requires` chains (~log₂ n
   questions per chain). A right answer marks the concept known and
   its prerequisites **known by implication** (trusted less; a quick
   confirming question if a plan comes to depend on them). Each
   answer records how sure you said you were (the event's confidence
   bits).

Free answers for concepts, graded by the advisor against the
concept's definition; multiple choice only for plain facts.

**Your thought process (decided)**: besides answering questions you
can explain how you think about something, typed or spoken, and the
advisor reads more from it than from a quiz answer:
- **concepts used correctly** → learner events of kind
  `explained`, with the transcript excerpt as evidence
- **misconceptions** → a wrong event plus a `question` node naming
  what was misunderstood, so teaching targets it
- **gaps**: concepts the explanation needed but never touched →
  marked "not sure" for the plan

Event kinds (2 bits): quiz answer, self-report, explanation,
refresher. Speech goes through a configurable external command
(`stt = whisper-cli …`, like `rg` today, so armin stays
dependency-free): the REPL records with a key and the web UI with
the browser's recorder, both handing a WAV to that command. The
transcript is shown for correction before the advisor reads it.

**Map (decided)**: goal-driven and lazy. Work backward from the goal
through `requires` until reaching what you know, and map only one or
two levels ahead of what is being taught; unneeded branches are never
built, and the map stays in the tree for reuse. Sources: the library
first, then the web. Every concept has a definition and at least one
citation. Confidence from sources: one web source → `inferred`; a
library source or two agreeing independent web sources → `checked`;
disagreeing sources → both claims kept with a `contradicts` edge,
and the advisor points it out instead of picking one.

**Plan (decided)**: a sequence of milestones, theory and building
interleaved. Each milestone has 2–4 frontier concepts (taught with
check questions), a piece of the project that uses them, and a check
that proves it works (a test or a visible command output). The
copilot scaffolds, you write the core, the advisor reviews it against
the concepts; your code counts as `explained` events. Next concept:
the frontier one with the most still-needed dependents, ties broken
by fewest new prerequisites. You can reorder, skip (recorded as
self-report) or add milestones; only the changed part is re-planned.

**Teach (decided)**, one concept at a time:
1. a **prediction question** first ("segments 1, 2 and 4 arrived;
   what does the receiver send?"), answered typed or spoken
2. a short **explanation** (~200–400 words) built on your answer,
   citing nodes and library pages, using the project's own code as
   the example when there is some
3. a **check question**, a free answer graded against the definition
4. on a wrong answer: name the misconception (a `question` node) and
   re-explain from another angle; **at most two attempts**, then
   "not sure" and the plan moves on, returning to it in the
   milestone review or before something that requires it

You set the pace: `next`, `deeper`, `skip` (self-report), and "why?"
to branch into a missing prerequisite and come back.

**Decide (decided)**: for choices costly to reverse (core data
structures, dependencies, public interfaces, storage formats) the
advisor presents 2–4 options with tradeoffs for your goal and says
which it would pick; you decide, and your reasoning is recorded
(also as learner evidence). Stored as a `decision` node whose
options are `decision`-kind children with a `chosen` flag (the 7th
node flag), rejected ones kept with their reasons, and `requires`
edges to the assumptions it rests on. When an assumption goes stale
or suspect the decision goes suspect through the same pass, and the
advisor asks whether to revisit it.

**Hand off (decided)**: a short structured note in the scratchpad,
shown before switching to the copilot:

```
milestone 3: parse incoming segments
concepts  [n42] [n57] [n61]     decisions [n80]
copilot   segment struct, raw socket setup, test harness
yours     parseHeader(), checksum()          ← stubs with TODO [n57]
check     go test ./seg -run TestParse passes
```

The copilot builds the scaffolding and leaves your parts as stubs
whose TODO cites the concept they practice, then runs the check (it
should fail only on your stubs) and hands back. Soft rule plus
review, no hard lock: the role reminder tells the copilot not to fill
your stubs, and the advisor's review checks from the edit history
that you wrote them; a part the copilot filled is flagged and
doesn't count as learner evidence. Once your check passes, the
review (correct? shows the concepts?) records learner events.

**Learning an existing codebase (decided)**: `implements` edges link
project nodes (`agent.Loop`) to general learner concepts ("agent
loop"); general concepts you know are skipped, new ones are taught
attached to real code.
1. **survey**, if the project tree is thin: the copilot builds the
   outline (packages, roles, entry points); per-file summaries are
   delegated to minions (check: every exported symbol mentioned),
   unverified until a tour uses them and the copilot verifies them
2. **architecture first**: a short overview from the outline: the
   layers, the main packages and how they connect
3. **path-following tours**: one real flow at a time ("REPL message
   → model → tool call → answer"), each stop a symbol node taught
   like a concept; the first tour is the path most others branch off
   from (the "unlocks the most" rule)
4. **exercises**: small real changes along the toured path, with the
   milestone and stub structure of new projects

**Front ends (decided)**: one structured message format for teaching
(`kind: choice`, options, answer type), rendered per front end, with
nothing front-end specific in the agent layer.

| | REPL | web | nvim | MCP |
|---|---|---|---|---|
| self-report / multiple choice | numbered options, one key | buttons | no | no |
| free answer | input, a key records speech | text box + mic button | no | no |
| plan / milestones | `/plan`; `/plan edit` in `$EDITOR` | side panel, drag to reorder | no | no |
| `[n42]` | dimmed path after it | hover card, click opens the node | hover | plain text |
| tree browser | `-know dump` | tree view of project and learner trees | no | no |

Teaching runs in the REPL and the web; the REPL stays fully usable
without a browser.

**Web UI redesign**: a better-looking design, still plain HTML + CSS +
JS with no build step or framework, embedded with `embed.FS`. Today
it's one 1,116-line `internal/web/static/index.html` with inline CSS
and JS; it splits so each piece is easy to find and change:

```
static/index.html          markup only
static/css/tokens.css      colors (light and dark) and syntax colors:
                           the one place to restyle
static/css/layout.css      the page grid, header, panels, small screens
static/css/chat.css, viewer.css, md.css   one file per part
static/js/main.js          wiring only (native ES modules, no bundler)
static/js/api.js           $, esc, fetch to the Go side
static/js/state.js         what the modules share: files, current, busy
static/js/header.js        model picker, balance, context gauge
static/js/chat.js          messages, approvals, status, the event stream
static/js/viewer.js        file list, previews, proposed changes
static/js/markdown.js, highlight.js   rendering
later: tree.js, learn.js (questions, plan panel), record.js (mic)
```

## 7. Package layout — settled

```
internal/know/     NEW  the graph engine for both trees; imports only x/sys
                        graph (Node, Evidence, CSR edges, arena), store (snap
                        mmap + log), find (BM25 + prefix), dedup, stale,
                        trace, cycle (Tarjan), plan (frontier, ordering),
                        dump/edit text format
internal/learner/  NEW  learner-only parts on top of know: events, Leitner
                        status, crystallized score, probe bisection
internal/stt/      NEW  runs the configured speech-to-text command on a WAV
internal/agent/         role.go (Role mask), a roles mask per tool spec, one
                        golden definitions file per role, tools_know.go,
                        tools_teach.go, tools_delegate.go; teaching
                        questions, plans and [n42] refs as display events
internal/think/         Mind gains Know, Learner and one client per role;
                        Switch(role) swaps the client and appends the
                        reminder; delegate runs a short-lived minion Loop
internal/config/        minion = …, copilot = …, advisor = …, stt = …
internal/repl/          /minion /copilot /advisor /plan, question rendering,
                        record key
internal/web/           the static split (section 6), tree view, plan panel,
                        mic
cmd/armin/              -know dump | edit | import-memories
```

- **One `Loop` whose role switches** (one history, a swapped client
  and a reminder); only `delegate` creates a second, temporary Loop.
- **`know` and `learner` are separate**: the engine stays small and
  imports nothing from armin, so it tests alone and can sit next to
  Poneglyph later.
- Front ends render teaching through display events and never import
  teaching logic.

## 8. Build order

Each step is tested on its own before the next starts, and lands as
its own commit (one per package where it spans several).

1. **`know` storage**: Node, Evidence, arena, CSR edges, path table;
   `graph.snap` (mmap) + `graph.log` (CRC, torn-write stop),
   compaction by rename; dump/edit text format. Tests: round trip
   through snap, log and dump; a log truncated mid-record; compaction
   while the log is non-empty.
2. **`know` algorithms**: find (BM25 + prefix), dedup (Jaccard),
   cycle pass (Tarjan), trace, plan (frontier, "unlocks the most"
   order). Tests on small hand-built graphs; benchmarks at 10k nodes /
   20k edges with allocation counts.
3. **Roles**: `Role` mask, roles per tool spec, golden definitions
   per role, `Switch` + reminder, config keys, `/minion` `/copilot`
   `/advisor`. Works with no tree at all; the golden files prove each
   role's tool list is byte-stable.
4. **Trees in context**: `tools_know.go` (find, get, add, link,
   edit; since merged into find, write, trace — see section 5) with
   the node line format and writing rules, the frozen outline in the
   system prompt, the procedure aid line, `memory_save` refusing
   code facts, `-know dump | edit | import-memories`. Then the
   **format measurement** (~200-node fixture, ~20 questions, minion
   and copilot models), and the **benchmark** ("Does the tree pay?",
   section 5).
5. **Staleness**: evidence hashing, git diff + status at session
   start, same-file re-anchoring, suspect pass; the cycle pass and
   the turn-end reminder for open cycles. Tests replay real edits
   from armin's history.
6. **Delegation**: `tools_delegate.go`, the temporary minion Loop,
   acceptance check, minion authorship, confidence cap, verifying.
   Then move armin's internal helpers (effort, titles, compaction
   summaries, cataloging) to minion jobs.
7. **`learner`**: events, Leitner status with fading, crystallized
   flag and score, refresher below 30, probe (self-report + bisection).
   Pure logic, tested with recorded answer sequences.
8. **Speech**: `internal/stt`, the REPL record key; thought-process
   answers into `explained` events, misconceptions, gaps.
9. **Web UI split**: the static files into the section 6 layout with
   no behavior change first, then the new design. Independent of
   steps 1–8; can move earlier.
10. **Teaching**: `tools_teach.go` and display events; map, plan with
    milestones, teach, decide, hand off; REPL rendering, then the web
    (questions, plan panel, tree view, mic). Codebase tours last
    (survey, architecture overview, path tours, exercises).
11. **Check it works**: one full run learning a real topic and one
    tour of an unfamiliar codebase; fix what they show before calling
    the restructure done.

## 9. Progress (revised 2026-10-31, after the tree benchmark)

Branch `restructure`, not pushed, not merged. Every step's tests
pass (`go test ./...`, the know and learner packages also under
`-race`).

| step | state | commits |
|---|---|---|
| 1 know storage | done | 9d3d73a |
| 2 know algorithms | done (tokenizer moved to internal/tokens) | eabbf3b, 9ed27f6 |
| 3 roles | done in the REPL | 9cf1c0c |
| 4 trees in context | done, format measured | 28b0bec, 2a89273, 3b1f5c5, 24edf79 |
| 5 staleness | done, by file stamps (no VCS needed) | cf60ab9, 1c77720 |
| 6 delegation | done | 89b3663 |
| 7 learner | done (logic only, used by step 10) | 4cd4d36 |
| 8 speech | done in the REPL (/voice, learn_explained) | 1adc4fe |
| 9 web UI split + redesign | done | c38a8cf, e47a868 |
| style pass (80 columns, no if-init) | done | 2faca9c |
| tree benchmark + answer rule | done, results in section 5 | 0471735 |
| six know tools into three | done (find reads full, write does all writes) | 28d53c5 |
| auto-record turn into tree | built; Q/A claims measured unhelpful, rethink pending (section 5) | 6af79a5 |
| benchmark loads .env key | done | e4a7178 |

### Left to do

**Tree economics** (the open question in section 5, before more is
built on the tree): strip auto-record to a `file`-kind node with the
ranges read (no Q/A text), measure; if s2 still doesn't beat no-tree,
add a third session to the pairs and measure long chains.

**Step 10, teaching** (the big one; section 6 has every decision):
- `tools_teach.go` beyond `learn_explained`: the probe (self-report on
  areas with `learner.AreaReport`, then `Probe.Next/Answer` with check
  questions), map (goal-driven, lazy; concepts with definitions and
  citations, confidence by source, `contradicts` for disagreements),
  `know_plan` (know.Plan with learner.State as known; milestones of
  2-4 concepts + a project piece + a check; refuses through an open
  cycle), teach (prediction question, explanation, check, two
  attempts), decide (`decision` nodes, options as children with
  FlagChosen, requires to assumptions), hand off (the structured note,
  stubs with TODO [nN], soft rule + review from the edit history).
- Refreshers: surface `learner.Refreshers()` to the advisor (the only
  unasked prompt).
- Teaching as display events in `agent/display.go` (`kind: choice`,
  options, answer type), rendered by the REPL (numbered options, one
  key) and the web (buttons).
- Codebase tours: survey (minion-delegated file summaries), the
  architecture overview, path-following tours, exercises.
- Web: role switching (/minion /copilot /advisor reach only the REPL
  and joined frontends don't get them yet), the plan panel, the tree
  view (js/tree.js), `[n42]` hover cards and click-to-open (needs a
  `/api/know` endpoint), the mic button (js/record.js: MediaRecorder →
  POST a WAV → internal/stt → text into the composer).
- REPL: `[n42]` dimmed with its path after it; `/plan`, `/plan edit`.

**Step 11**: learn one real topic and tour one unfamiliar codebase end
to end; fix what that shows.

**Smaller loose ends**
- `stt` isn't in the live config.conf yet; a branch-run config with
  the role keys and stt sits at `config.conf.restructure` in the
  repo root (the main-branch build ignores the role keys). The
  whisper.cpp build is at
  /media/henrique/partition/llm/src/whisper.cpp (whisper-cli under
  build/bin), the model at
  models/whisper/ggml-large-v3-turbo-q5_0.bin; add `-ng` if the
  local minion holds the GPU. /voice is tested only with fake scripts.
- The treebench dump path is package-relative (resolves under
  internal/think); pass an absolute path, or make the bench
  filepath.Abs it.
- Version control adapters (hg, jj, svn, fossil) aren't written; only
  git was installed to test, and staleness no longer needs any.
- Poneglyph integration (section 4c): later, when it exists.
- The format harness (internal/know/format_eval_test.go) can rerun any
  model: ARMIN_FORMAT_EVAL_URL/_MODEL/_KEY, _SET=hard|harder, _ONLY,
  _REPEAT, _DUMP. Results so far: plain beat coded on every model
  (Qwen 3.8 27B 28/28 vs 25/28 moderate; Qwen3.5-9B 20/20 vs 19/20;
  GLM 5.3 Flash 19/20 vs 13/20 harder, label slip counted).
- Web verification: a stub server and a WebDriver BiDi driver for
  headless Firefox were used to compare old and new pages pixel for
  pixel; they live only in the session scratchpad, worth adding under
  internal/web/testdata if the web keeps growing.

