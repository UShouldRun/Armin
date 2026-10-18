"""Sets each task's "easy" to how often the model solved it, grounded,
without thinking. Easy tasks make thinking cost reward; hard ones
don't. Unsolvable tasks get 0: spotting the problem is worth
thinking about.

  python label.py --split rl --rollouts base-rl-nt --out rl.labeled

Writes only the labeled tasks, so --limit on the rollouts bounds
the RL task pool. --extra adds a split no rollout has seen (round 2's
code tasks) with easy = 0: new and harder, so thinking stays free.

  python label.py --split rl --rollouts base-rl-nt --extra rlx --out rl2.labeled
"""

import argparse
import os

from common import DATA, OUT, read_jsonl, write_jsonl


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--split",    required = True)
  ap.add_argument("--rollouts", required = True)
  ap.add_argument("--out",      required = True)
  ap.add_argument("--extra")
  args = ap.parse_args()

  hits, seen = {}, {}
  for r in read_jsonl(os.path.join(OUT, "rollouts", args.rollouts + ".jsonl")):
    g = r["grade"]
    seen[r["id"]] = seen.get(r["id"], 0) + 1
    hits[r["id"]] = hits.get(r["id"], 0) + (g["correct"] and g["grounded"])

  tasks = [
    t for t in read_jsonl(os.path.join(DATA, args.split + ".jsonl"))
    if t["id"] in seen
  ]
  for t in tasks:
    t["easy"] = hits[t["id"]] / seen[t["id"]] if t["solvable"] else 0.0

  if args.extra:
    for t in read_jsonl(os.path.join(DATA, args.extra + ".jsonl")):
      t["easy"] = 0.0
      tasks.append(t)

  write_jsonl(os.path.join(DATA, args.out + ".jsonl"), tasks)
  easy = sum(t["easy"] >= 0.75 for t in tasks)
  print(f"{args.out}: {len(tasks)} labeled, {easy} easy")


if __name__ == "__main__":
  main()
