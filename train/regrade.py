"""Re-judges saved episodes with env's matchers as they are now
(_matches for search and memory facts, _close for numbers), so runs
graded before a matcher fix compare fairly with later ones. Only
"correct" and "hallucinated" change: they depend on the final answer
alone. Grounding and process needed the live episode and stay as they
were.

  python regrade.py --split eval base-eval sft-eval rl-eval-179
"""

import argparse
import os

import env as environment
from common import OUT, final_parse, read_jsonl, reward, tasks_by_id, write_jsonl


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--split", required = True, help = "split(s) the runs used, comma-separated")
  ap.add_argument("runs", nargs = "+")
  args = ap.parse_args()

  tasks = tasks_by_id(*args.split.split(","))
  for name in args.runs:
    path = os.path.join(OUT, "rollouts", name + ".jsonl")
    rows = read_jsonl(path)
    changed = 0
    for r in rows:
      t = tasks.get(r["id"])
      g = r["grade"]
      if t is None or not r.get("final") or not g["format"]:
        continue

      if t["kind"] not in ("search", "math", "science", "memory"):
        continue

      if g["abstained"] or not t["solvable"]:
        continue

      f = final_parse(r["final"])
      if f["answer"] is None:
        continue

      if "number" in t:
        correct = environment._close(f["answer"], t)

      else:
        correct = environment._matches(f["answer"], t["answers"])

      real = t.get("real_answers")
      halluc = bool(real) and not correct and environment._matches(f["answer"], real)
      if (correct, halluc) != (g["correct"], g["hallucinated"]):
        changed += 1
        g["correct"], g["hallucinated"] = correct, halluc
        r["reward"] = reward(t, g, r["think_chars"])

    write_jsonl(path, rows)
    print(f"{name}: {changed} episodes re-judged")


if __name__ == "__main__":
  main()
