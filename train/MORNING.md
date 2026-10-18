# Overnight results

## Ranking on the practical test (through armin, 28 tasks)

1. **qwen3.5-9b** (reference): 23/28 passed, 27 s per task
2. **base** (reference): 11/28 passed, 63 s per task
3. **r1-179**: 9/28 passed, 67 s per task (try it: `armin` then `/model ft-r1-179`)
4. **r2-sft**: 5/28 passed, 247 s per task (try it: `armin` then `/model ft-r2-sft`)

Details per category and task: localtest/out/REPORT.md, answers and tool
calls per task in localtest/out/<model>.jsonl.

## Trying them

The candidates are in ~/.config/armin/config.conf as models.ft-*
(backup of the old config: config.conf.pre-ft). In armin: `/model
ft-r2-65` (for example); `/model local` goes back to Qwen3.5-9B. GGUFs:
train/pod/gguf/.

## Pod eval (400 tasks, regraded with the current grader)

# armin model report

Runs compared: `base-eval`, `sft-eval`, `rl-eval-179` (400 eval tasks each). First = base model, last = trained.

## Verdict, base → trained

| Metric | Base | Trained | Change | big-eval |
|---|---|---|---|---|
| Solved (solvable tasks) | 22.7% | 54.5% | +31.8 pts (better) | 34.0% |
| Solved without asking for a hint | 22.7% | 54.5% | +31.8 pts (better) | 34.0% |
| Grounded (share of solved backed by a tool) | 22.4% | 98.5% | +76.2 pts (better) | 97.6% |
| Hallucinated | 1.2% | 2.8% | +1.5 pts (worse) | 0.2% |
| Right CANNOT_SOLVE on unsolvable | 0.0% | 50.0% | +50.0 pts (better) | 7.7% |
| False CANNOT_SOLVE on solvable | 6.1% | 0.8% | -5.3 pts (better) | 0.3% |
| Answer format followed | 42.8% | 96.2% | +53.5 pts (better) | 38.0% |
| Brier (calibration, lower is better) | 0.575 | 0.291 | -0.284 (better) | 0.363 |
| ECE (calibration, lower is better) | 0.607 | 0.219 | -0.388 (better) | 0.360 |
| Thinking (chars per episode) | 6205 | 356 | |
| Tool calls per episode | 0.6 | 2.0 | |
| Searches per episode | - | - | |
| Workflow followed | - | - | |
| Hints asked per episode | 0.00 | 0.00 | |

## Solved per family

| Family | n | base-eval | sft-eval | rl-eval-179 | big-eval |
|---|---|---|---|---|---|
| code | 98 | 39.1 | 78.3 | 90.2 | 40.2 |
| math | 53 | 26.4 | 11.3 | 34.0 | 39.6 |
| memory | 35 | 46.2 | 100.0 | 100.0 | 11.5 |
| save | 23 | 43.5 | 100.0 | 100.0 | 65.2 |
| science | 37 | 27.0 | 21.6 | 21.6 | 16.2 |
| search | 126 | 1.7 | 15.7 | 15.7 | 14.8 |
| skill | 28 | 3.6 | 96.4 | 100.0 | 100.0 |


