---
name: pytorch-experiments
description: Run ML experiments in PyTorch with reproducible setups — fixed seeds, proper splits / k-fold, baselines, logged metrics, configs in files — and report results honestly. Load when training or evaluating models.
---
## Setup
- One config (dict / dataclass / YAML) per experiment: model,
  data, optimizer, schedule, seed. Results record the config they
  came from.
- Seed everything (`random`, `numpy`, `torch`, CUDA) and note that
  GPU nondeterminism remains unless deterministic algorithms are on.
- Splits fixed and saved; the test set touched once, at the end.
  Use the dataset's official folds when it has them (e.g.
  UrbanSound8K's 10 folds — never mix folds).
- Class imbalance: report per-class metrics and macro averages, not
  only accuracy.

## Code
- `Dataset` / `DataLoader` with the preprocessing in one place;
  features cached when extraction is slow (e.g. librosa).
- `model.train()` / `model.eval()` and `torch.no_grad()` in the
  right places; `.to(device)` once; no Python loops over samples in
  the hot path.
- Checkpoint the best model by validation metric, plus the last one.

## Running
- Start with a tiny subset to check the loop end to end (loss goes
  down, shapes right) before a full run.
- Log per epoch: train / val loss and metrics, learning rate, time.
  Write them to CSV/JSON so they can be plotted, and keep a running
  summary in the scratchpad.
- Long runs: run in the background with output to a log file and
  check it, rather than holding the session.

## Reporting
- Always against a baseline (a simple model, the previous method,
  chance level).
- Mean ± std over seeds or folds; say how many.
- Tables with the exact config; plots with labeled axes.
- State what didn't work too — "PPO 60% vs. DQN 0% on unseen tracks"
  is a result.
