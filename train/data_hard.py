"""Round 2's code families, from KodCode tasks round 1 never used
(so nothing here overlaps its splits):

  hard      KodCode's medium and hard problems (GPT-4o pass rates),
            hard first; same setup as the code family
  debug     solution.py is the reference with one or two planted
            bugs (a flipped comparison, an off-by-one, a wrong
            operator...); the visible tests fail; fix it
  report    one planted bug the visible tests miss; the user pastes
            the failing test from a bug report; reproduce, then fix

A mutant is kept only if the hidden tests catch it (and, for debug,
the visible ones too), so every planted bug is a real one. Each bug
keeps its line, the buggy text and the fix, for hints and demos.
Standard library + pytest."""

import ast
import os
import random
import re
import shutil
import tempfile
from concurrent.futures import ThreadPoolExecutor

from sandbox import run_pytest

# eval, sft, rl tasks per family
SIZES = {
  "hard":   (60, 200, 250),
  "debug":  (50, 200, 200),
  "report": (30, 100, 150),
}
TRIES = 6   # mutants tried per task
# Candidate tasks per needed one: a bug the visible tests miss but a
# hidden test catches is rare (about 1 in 8 tasks), a visible one isn't.
POOL = { "debug": 3, "report": 10 }
# Parallel test runs. A container's os.cpu_count() is the host's (160
# on a 20-vCPU pod): that many at once made the tests time out and
# threw away most bugs.
WORKERS = min(os.cpu_count() or 4, 16)

DEBUG_USER = """{question}

solution.py implements this, but it has a bug: some tests in \
test_solution.py fail. Find the bug and fix it in solution.py \
without changing the tests. More hidden tests will run against \
solution.py. Only the Python standard library is available."""

REPORT_USER = """{question}

solution.py implements this and its tests pass, but a user reports \
a bug. This test, from their report, fails:

```python
{test}
```

Find the bug and fix it in solution.py. More hidden tests will run \
against solution.py. Only the Python standard library is available."""

_SWAP = {
  ast.Lt: ast.LtE, ast.LtE: ast.Lt, ast.Gt: ast.GtE, ast.GtE: ast.Gt,
  ast.Eq: ast.NotEq, ast.NotEq: ast.Eq,
  ast.Add: ast.Sub, ast.Sub: ast.Add, ast.Mult: ast.FloorDiv,
  ast.FloorDiv: ast.Mult, ast.Mod: ast.FloorDiv,
  ast.And: ast.Or, ast.Or: ast.And,
}
_failed_re = re.compile(r"^FAILED \S+::(\w+)", re.M)


def _sites(tree):
  """Every place a bug can go: (node, kind)."""
  out = []
  for node in ast.walk(tree):
    if isinstance(node, ast.Compare) and type(node.ops[0]) in _SWAP:
      out.append((node, "compare"))

    elif isinstance(node, (ast.BinOp, ast.AugAssign)) and type(node.op) in _SWAP:
      out.append((node, "operator"))

    elif isinstance(node, ast.BoolOp) and type(node.op) in _SWAP:
      out.append((node, "boolean"))

    elif (
      isinstance(node, ast.Constant) and type(node.value) is int
      and 0 <= node.value <= 10 and hasattr(node, "lineno")
    ):
      out.append((node, "off-by-one"))

  return out


def _mutate(node, kind, rng):
  if kind == "compare":
    node.ops[0] = _SWAP[type(node.ops[0])]()

  elif kind in ("operator", "boolean"):
    node.op = _SWAP[type(node.op)]()

  else:
    node.value += rng.choice([-1, 1]) if node.value else 1


def plant(source, n, rng):
  """The reference with n bugs on n different lines, as
  (buggy source, fixed source, bugs), or None. Both sources are
  ast.unparse output, so the only differences are the bugs."""
  try:
    fixed = ast.unparse(ast.parse(source))
    tree = ast.parse(fixed)

  except (SyntaxError, ValueError):
    return None

  # Line numbers of the unparsed text, which is what the model sees.
  sites = [(nd, k) for nd, k in _sites(tree) if getattr(nd, "lineno", None)]
  rng.shuffle(sites)
  chosen, lines = [], set()
  for nd, k in sites:
    if nd.lineno not in lines:
      chosen.append((nd, k))
      lines.add(nd.lineno)

    if len(chosen) == n:
      break

  if len(chosen) < n:
    return None

  for nd, k in chosen:
    _mutate(nd, k, rng)

  buggy = ast.unparse(tree)
  good_lines, bad_lines = fixed.splitlines(), buggy.splitlines()
  if len(good_lines) != len(bad_lines):
    return None

  bugs = [
    { "line": i + 1, "bad": b.strip(), "good": g.strip() }
    for i, (g, b) in enumerate(zip(good_lines, bad_lines)) if g != b
  ]
  if not bugs:
    return None

  return buggy, fixed, bugs


def _pytest(solution, tests):
  """(passed, names of the failed tests)."""
  work = tempfile.mkdtemp(prefix = "plant-")
  try:
    with open(os.path.join(work, "solution.py"), "w") as f:
      f.write(solution)

    with open(os.path.join(work, "test_solution.py"), "w") as f:
      f.write(tests)

    code, out = run_pytest(work, ["-rf", "test_solution.py"])
    return code == 0, _failed_re.findall(out)

  finally:
    shutil.rmtree(work, ignore_errors = True)


def _test_source(src, name):
  m = re.search(rf"^def {name}\(.*?(?=^\S|\Z)", src, re.M | re.S)
  return m.group(0).rstrip() if m else None


def _debug(task, rng, report):
  """A planted-bug variant of a code task, or None."""
  visible, hidden = task["files"]["test_solution.py"], task["hidden"]
  for _ in range(TRIES):
    found = plant(task["solution"], 1 if report or rng.random() < 0.6 else 2, rng)
    if found is None:
      continue

    buggy, fixed, bugs = found
    ok_hidden, failed = _pytest(buggy, hidden)
    if ok_hidden or not failed:
      continue

    ok_visible, _ = _pytest(buggy, visible)
    if report:
      # The bug report is a hidden test the visible ones don't have.
      shown = set(re.findall(r"^def (test\w*)\(", visible, re.M))
      extra = [n for n in failed if n not in shown]
      test = _test_source(hidden, extra[0]) if extra else None
      if not ok_visible or test is None:
        continue

      user = REPORT_USER.format(question = task["question"], test = test)

    else:
      if ok_visible:
        continue

      test = None
      user = DEBUG_USER.format(question = task["question"])

    return dict(
      task,
      id       = task["id"] + ("-report" if report else "-debug"),
      variant  = "report" if report else "debug",
      user     = user,
      files    = { "solution.py": buggy + "\n", "test_solution.py": visible },
      solution = fixed + "\n",
      bugs     = bugs,
      report   = test,
    )

  return None


def build(code_tasks, used, raw, rng):
  """code_tasks: data_code's tasks; used: ids already in a split;
  raw: KodCode rows by question_id (for the difficulty labels)."""
  fresh = [t for t in code_tasks if t["id"] not in used]
  for t in fresh:
    r = raw[t["id"][len("code-"):]]
    t["difficulty"] = r.get("gpt_difficulty")
    t["question"] = r["question"].strip()

  rng.shuffle(fresh)
  rank = { "hard": 0, "medium": 1 }
  hard_pool = sorted(
    (t for t in fresh if t["difficulty"] in rank),
    key = lambda t: rank[t["difficulty"]],
  )
  need = sum(SIZES["hard"])
  hard = [dict(t, id = t["id"] + "-hard", variant = "hard") for t in hard_pool[:need]]
  rng.shuffle(hard)

  # Bugs go into the rest, any difficulty: finding one is the work.
  rest = [t for t in fresh if t not in hard_pool[:need]]
  out = { "hard": hard }
  for fam, report in (("debug", False), ("report", True)):
    need = sum(SIZES[fam])
    pool = rest[:need * POOL[fam]]
    rest = rest[need * POOL[fam]:]
    seeds = [rng.random() for _ in pool]
    with ThreadPoolExecutor(WORKERS) as ex:
      results = list(ex.map(
        lambda ts: _debug(ts[0], random.Random(ts[1]), report),
        zip(pool, seeds),
      ))

    made = [t for t in results if t is not None][:need]
    print(f"{fam}: {len(made)} of {need} planted ({len(pool)} tried)")
    out[fam] = made

  return out
