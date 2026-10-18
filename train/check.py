"""Oracle check of the environment and grader, no GPU needed: for
sampled tasks of every family it plays a scripted right episode and
a wrong one through ArminEnv's tools, and checks the grader scores
them as it should. Also reports how often a plain web_search of the
question surfaces the answer (multi-hop questions need more than
one search, so this is a floor).

  python check.py [--split eval] [--per-kind 20]
"""

import argparse
import json
import os
import random
import sys

import env as environment
from common import DATA, read_jsonl


def episode(task):
  e = environment.ArminEnv()
  e.reset(task_id = task["id"])
  return e


def right_and_wrong(task):
  """Plays the task correctly, then wrongly. Returns two grades."""
  kind = task["kind"]
  out = []
  for right in (True, False):
    e = episode(task)
    if not task["solvable"]:
      if kind == "code":
        e.run_tests()

      if kind == "search":
        e.web_search(task["user"])

      final = (
        f"CANNOT_SOLVE: {task['why']}.\nCONFIDENCE: 0.9" if right
        else "ANSWER: 42\nCONFIDENCE: 0.9"
      )

    elif kind == "code":
      code = task["solution"] if right else "def nothing():\n    pass\n"
      e.write_file("solution.py", code)
      e.run_tests()
      final = "Done.\nCONFIDENCE: 0.9"

    elif kind == "search":
      res = json.loads(e.web_search(task["user"], count = 10))["results"]
      gold = task["answers"][0]
      cite = next(
        (r["id"] for r in res if gold.lower() in r["snippet"].lower()), None,
      )
      if cite is None:
        e.web_search(gold + " " + task["user"], count = 10)
        cite = next(
          (i for i, s in e._snippets.items() if gold.lower() in s.lower()), 1,
        )

      answer = gold if right else (task.get("real_answers") or ["Zanzibar"])[0]
      final = f"ANSWER: {answer} [{cite}]\nCONFIDENCE: 0.8"

    elif kind in ("math", "science") or (kind == "memory" and "number" in task):
      if kind == "memory":
        e.memory_read(task["memory"])

      v = task["number"] if right else task["number"] * 3 + 7
      e.run_program("python3", ["-c", f"print({v!r})"])
      final = f"ANSWER: {v}\nCONFIDENCE: 0.8"

    elif kind == "memory":
      e.memory_read(task["memory"])
      answer = task["answers"][0] if right else "nobody"
      final = f"ANSWER: {answer}\nCONFIDENCE: 0.8"

    elif kind == "skill":
      e.use_skill(task["skill"])
      final = _notes(task) if right else "Here are the notes.\n- stuff"
      final += "\nCONFIDENCE: 0.8"

    elif kind == "save":
      if right == task["should_save"]:
        e.memory_save("formatter", "Python formatter", f"Use {task['key']}.")

      final = "ANSWER: done\nCONFIDENCE: 0.8"

    out.append(environment.grade(e, final))
    e._cleanup()

  return out


def _notes(task):
  rules = { r["type"]: r for r in task["rules"] }
  first = rules["first_line"]["regex"].replace("\\", "")
  bullet = rules["bullets"]["prefix"]
  return first + "\n" + bullet + "Fixed things.\n" + bullet + "Added things."


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--split",    default = "eval")
  ap.add_argument("--per-kind", type = int, default = 20)
  args = ap.parse_args()

  environment.load(args.split)
  tasks = read_jsonl(os.path.join(DATA, args.split + ".jsonl"))
  rng = random.Random(0)
  groups = {}
  for t in tasks:
    groups.setdefault((t["kind"], t["variant"]), []).append(t)

  failed = 0
  for key in sorted(groups):
    sample = rng.sample(groups[key], min(args.per_kind, len(groups[key])))
    bad = []
    grounded = 0
    for t in sample:
      good, wrong = right_and_wrong(t)
      grounded += good["grounded"]
      if not good["correct"] or wrong["correct"]:
        bad.append(t["id"])

    failed += len(bad)
    note = f"  grounded {grounded}/{len(sample)}" if key[0] == "search" else ""
    print(f"{key[0] + '/' + key[1]:26} {len(sample) - len(bad):3}/{len(sample)} ok{note}")
    for tid in bad[:3]:
      print("    wrong verdict:", tid)

  sys.exit(1 if failed else 0)


if __name__ == "__main__":
  main()
