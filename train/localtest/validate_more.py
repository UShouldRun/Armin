"""Proves tasks_more.py's tasks sound: each task checked by running
code fails as given and passes with its reference solution; each
answer pattern accepts the right sample and rejects the wrong one.

  python3 validate_more.py [id ...]
"""

import os, shutil, sys, tempfile

from run import setup, grade
from tasks_more import TASKS_MORE, SOLUTIONS, SAMPLES

# checks that hold on the untouched files: the task is not to break them
PASS_UNTOUCHED = { "no-secret" }


def trial(task, extra):
  root = tempfile.mkdtemp(prefix = f"val-{task['id']}-")
  try:
    setup(task, root)
    for path, body in extra.items():
      p = os.path.join(root, path)
      os.makedirs(os.path.dirname(p), exist_ok = True)
      open(p, "w").write(body)

    t = dict(task)
    t.pop("answer", None)
    return grade(t, root, { "answer": "", "text": "" })
  finally:
    shutil.rmtree(root, ignore_errors = True)


def main():
  ids = set(sys.argv[1:])
  bad = 0
  for task in TASKS_MORE:
    i = task["id"]
    if ids and i not in ids:
      continue

    problems = []
    if "check" in task:
      before = trial(task, {})
      if before != (i in PASS_UNTOUCHED):
        problems.append(f"as given the check {'passes' if before else 'fails'}")

      if i not in SOLUTIONS:
        problems.append("no reference solution")
      elif not trial(task, SOLUTIONS[i]):
        problems.append("the reference solution fails the check")

    if "answer" in task:
      import re
      good, wrong = SAMPLES.get(i, (None, None))
      if good is None:
        problems.append("no answer samples")
      else:
        if not all(re.search(p, good) for p in task["answer"]):
          problems.append("the right answer is rejected")
        if all(re.search(p, wrong) for p in task["answer"]):
          problems.append("the wrong answer is accepted")

    bad += bool(problems)
    print(f"{'BAD ' if problems else 'ok  '} {i:16} {'; '.join(problems)}", flush = True)

  print(f"{len(TASKS_MORE) - bad}/{len(TASKS_MORE)} sound" if not ids else "")
  sys.exit(1 if bad else 0)


if __name__ == "__main__":
  main()
