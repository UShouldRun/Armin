# armin model training

Tunes an open ~8B model (default `Qwen/Qwen3-8B`, set `ARMIN_BASE`
to change it) to work the way armin needs: solve problems **through
armin's tools** instead of recalling facts, think only when a task
needs it, say `CANNOT_SOLVE` instead of making something up, and
state a calibrated `CONFIDENCE`.

Budget target: under $100 of rented GPU time. Expected: ~16 GPU
hours on one H100 80GB, about $25–35 (see [GPU](#gpu)).

## How it works

Every task is a multi-turn episode in `env.py`'s `ArminEnv`, which
exposes 13 of armin's tools under armin's names, argument shapes
and descriptions: `read_file`, `write_file`, `edit_file`,
`list_directory`, `search_code`, `run_tests`, `run_program`,
`web_search`, `memory_read`, `memory_save`, `use_skill`,
`scratchpad_write`, `scratchpad_read`. The system prompt is armin's
own (`internal/think/prompt.go`, copied at prepare time) with the
memory and skill indexes in armin's format, plus `ANSWER_RULES`
from `common.py`.

| Family  | Source (license) | What it trains |
|---------|------------------|----------------|
| code    | KodCode-Light-RL-10K (CC BY-NC 4.0) | write `solution.py`, check with `run_tests`; hidden tests grade it |
| search  | MuSiQue-Ans dev (check its license) | multi-hop questions over an offline BM25 `web_search`; cite `[id]` |
| math    | DeepScaleR (MIT) | numeric answers, computed with `run_program` |
| science | WebInstruct-verified (Apache-2.0) | physics, chemistry, biology, economics... numeric answers |
| memory  | synthetic | read a memory, compute with it, or notice it doesn't exist |
| skill   | synthetic | load a skill and follow its randomized format rules |
| save    | synthetic | `memory_save` a durable preference, not a one-off |

**No fact recall.** Variants make memory useless or wrong:
counterfactual search tasks change the answer everywhere in the
corpus (answering from memory gives the real-world value, and is
penalized as a hallucination); unanswerable ones hide every
paragraph holding the answer; memory and skill values are random
per task. A right answer with nothing behind it (no test run, no
cited search result, no `run_program`, no `memory_read`) loses
reward too.

**Unsolvable tasks** (~10–15% of each split): contradictory tests,
tests importing a module that doesn't exist, unanswerable
questions, missing memories. The right reply is `CANNOT_SOLVE: ...`.

### Reward (`common.reward`)

```
correct - (confidence - correct)^2     calibration, RLCR-style Brier
- 0.5  CANNOT_SOLVE on a solvable task   (else abstaining is a safe 0)
- 0.5  hallucinated                       invented module/name,
                                          answered the unanswerable,
                                          recalled over searched
- 0.3  right but ungrounded               no tool backed it
- 0.3 * easy * thinking / budget          thinking costs on easy tasks
-1     malformed or truncated
```

`easy` is how often the base model solves the task, grounded,
*without* thinking (`label.py`). Thinking is free on hard and
unsolvable tasks and costs on easy ones, so the model learns when
to think (as in AdaptThink, arXiv 2505.13417). The Brier term follows
RLCR (arXiv 2507.16806). AbstentionBench (arXiv 2506.09038) found that
reasoning training hurts abstention, which is why abstention is
rewarded explicitly and measured at every stage.

### Stages (`run.sh`)

1. `prepare.py`: download, build tasks, split eval / sft / rl (CPU).
   `check.py` then replays right and wrong episodes through the
   environment to make sure the grader judges them correctly.
2. `rollout.py`: base-model episodes: eval, then no-think episodes
   for difficulty labels and both modes for self-distillation.
3. `label.py`, `build_sft.py`: difficulty labels; warm-up set of
   the base model's own **grounded** successes (no paid teacher),
   CONFIDENCE rewritten to the measured success rate; scripted
   look-then-abstain episodes where the base never abstained.
4. `sft.py` + `merge.py`: LoRA warm-up on the model's turns only.
5. `rl.py` + `merge.py`: multi-turn GRPO (TRL 1.16, DAPO loss, no KL,
   LoRA, vLLM colocated), reward above.
6. `rollout.py` + `report.py`: eval after each stage, side by side.

## Run it

```sh
# on the GPU box, with the armin repo cloned:
cd armin/train
./setup.sh                 # venv + pinned vllm/trl/transformers
SMOKE=1 ./run.sh           # whole pipeline on Qwen3-0.6B, ~15 min
./run.sh                   # the real run; rerun after any interruption
```

`prepare.py` needs only the standard library and pytest, so it can
run on your own machine first (`python prepare.py && python
check.py`) and `data/` copied over with the repo, to save GPU time.

`report.py` prints, per family: solved %, grounded % of the solved,
hallucination %, right abstentions on unsolvable tasks, false
abstentions on solvable ones, Brier, ECE, thinking length and tool
calls.

## Round 2: multi-hop search and the science workflow

Round 1's eval showed the warm-up had taught "search once": 111 of
115 search episodes made exactly one search, which can't answer a
2–4 hop MuSiQue question. Science had nothing to look up (the corpus
was MuSiQue's Wikipedia paragraphs) and 35% false `CANNOT_SOLVE`.
`round2.sh` builds on round 1's merged RL model:

- **Workflow in the prompt** (`ANSWER_RULES`, "How to work"): science
  and math look up what they need, write a plan to the scratchpad,
  run a script and answer with what it printed; fact questions
  search once per fact, feeding each answer into the next query,
  keep notes, cite.
- **Multi-hop demos** (`build_sft.hop_search`): MuSiQue's own
  decomposition scripts the plan, one search per hop, notes and a
  cited answer; unanswerable tasks search until the missing hop,
  then `CANNOT_SOLVE`. `--search-only` builds just these plus a
  replay of round 1's other families for a short top-up warm-up.
- **Science library** (`science_ref.py`): CODATA constants (NIST),
  the periodic table (PubChem) and a formula sheet, added to the
  `web_search` corpus. Search tasks never see it, so their corpus
  and splits are unchanged (prepare yields the same task ids).
- **Shaping rewards** (`common.reward`): +0.2 x share of the needed
  paragraphs retrieved (R1-Searcher, StepSearch), -0.05 per repeated
  query, +0.15 for following the workflow (`env._process`; for
  math and science the answer must be a number the script printed).
- **More tries per question** (`--gens 12 --prompts 3`, same cost per
  step), search tasks twice in the pool, and a 2 -> 3 -> 4 hop
  curriculum (`--curriculum`).

- **Hints** (`hints.py`, the `ask_user` tool): when stuck, the model
  can ask, up to 3 times per task; each answer tells more (search:
  the plan, then step 1's answer, then everything but the last step;
  code: approach, a hidden test, the solution's first half; math: the
  worked solution's first steps, or the answer's size, then a range;
  science: library entries, the answer's size, a rough value). Hints
  cost 0.15, 0.2, 0.25 and solving after one wins 0.1 back, so solving
  alone > solving with hints > failing. An unsolvable task's first
  hint is its family's usual one, so asking can't reveal that it is
  unsolvable. The report's `solo` column (solved without a hint) is
  the one to compare with round 1. armin has no `ask_user` tool yet;
  it needs one before this model runs there.

- **The bigger model as target and teacher**: Qwen3.8-27B (FP8) is
  evaluated on the same 400 tasks, and its grounded successes on the
  sft split (math, science, search, code; the workflow followed where
  there is one, then the least thinking) join the top-up warm-up,
  which had no honest way to script science or math.

```sh
BUDGET_HOURS=8 AUTO_STOP=1 ./round2.sh   # writes out/REPORT2.md
STEPS=300 ./round2.sh                    # continue round-2 RL later
```

## Using the result in armin

1. Convert: `python llama.cpp/convert_hf_to_gguf.py out/rl-merged
   --outtype bf16`, then `llama-quantize ... Q4_K_M` (~5 GB; fits the
   RTX 4060's 8 GB).
2. Add `ANSWER_RULES` (from `common.py`) to armin's system prompt
   for this model; armin can then use the CONFIDENCE line to decide
   when to escalate to a bigger cloud model.

## GPU

Prices from 2026 comparison pages; marketplace prices move hourly,
so check before renting.

| Provider | GPU | $/h | ~16 h |
|----------|-----|-----|-------|
| Vast.ai (verified hosts) | H100 80GB | 1.50–1.87 | $24–30 |
| RunPod Community Cloud | H100 PCIe | 1.99 | ~$32 |
| Vast.ai | A100 80GB | 0.50–0.80 | ~2x the hours, $16–26 |
| Lambda | H100 | 3.99–4.29 | $64–69 |

Pick an H100 80GB on Vast.ai (verified datacenter host) or RunPod
Community Cloud, with a 150 GB disk. The A100 is cheaper per run
but takes about twice as long. Unverified Vast hosts go lower,
around $0.90/h, but have uneven uptime; `run.sh` resumes, so that
is a fair gamble.

Rough hours: prepare and check 0.5, base rollouts 2.5, SFT 2,
merges 0.5, three evals 1.5, RL (250 steps) 8–9.
