"""What the simulated user answers when the model calls ask_user:
up to HINTS hints per task, each telling more than the one before
(and costing more reward, common.reward). Built from each task's
own data, so a hint never needs a paid teacher. Standard library
only."""

import math
import re

HINTS = 3

_ref_re   = re.compile(r"#(\d+)")
_boxed_re = re.compile(r"\\boxed")


def hint(task, k, index = None):
  """The k-th hint (1-based) for this task. The first hint of an
  unsolvable task is its family's usual one, so asking once never
  tells the model whether the task can be done."""
  if not task["solvable"] and k > 1:
    return _unsolvable(task, k)

  return {
    "code":    _code,
    "search":  _search,
    "math":    _math,
    "science": _science,
    "memory":  _memory,
    "skill":   _skill,
    "save":    _save,
  }[task["kind"]](task, k, index)


def _unsolvable(task, k):
  where = {
    "missing":      lambda: f"the {task.get('module', '')} module the tests import",
    "contradict":   lambda: "the test whose name ends in _regression",
    "unanswerable": lambda: "whether any result actually states the final answer",
    "absent":       lambda: "the memory index in the system prompt",
  }.get(task["variant"], lambda: "what the task assumes")()
  if k == 2:
    return f"Look closely at {where}."

  return f"I checked: {task['why']}."


def _bug(task, k):
  bugs = task["bugs"]
  if k == 1:
    if task.get("report"):
      return "Add the reported test to test_solution.py and run the tests to see it fail; the bug is in solution.py."

    return "Run the tests and read the first failure; the bug is in solution.py, not the tests."

  where = " and ".join(f"line {b['line']}" for b in bugs)
  if k == 2:
    return f"Look at {where} of solution.py."

  return " ".join(f"Line {b['line']}: `{b['bad']}` should be `{b['good']}`." for b in bugs)


def _code(task, k, _):
  if task.get("bugs"):
    return _bug(task, k)

  if k == 1:
    return (
      f"Run the visible tests and read the failures. The hidden tests "
      f"call {task['entry']} the same way, on more inputs and edge cases."
    )

  if k == 2:
    shown = task["files"]["test_solution.py"]
    names = re.findall(r"^def (test\w*)\(", task["hidden"], re.M)
    extra = [n for n in names if f"def {n}(" not in shown]
    if extra:
      body = re.search(
        rf"^def {extra[0]}\(.*?(?=^\S|\Z)", task["hidden"], re.M | re.S,
      ).group(0).rstrip()
      return "One of the hidden tests:\n\n" + body

    return "The hidden tests are the visible ones plus edge cases: empty, single and large inputs."

  lines = task["solution"].strip().splitlines()
  half = lines[:max(1, len(lines) // 2)]
  return "This is how the solution starts:\n\n" + "\n".join(half)


def phrase(q, found = None):
  """A MuSiQue hop as words, earlier answers filled in:
  "Green >> performer" -> "the performer of Green",
  "#1 >> member of" -> "what Steve Hillage is a member of"."""
  q = _ref_re.sub(lambda m: (found or {}).get(int(m.group(1)), f"(step {m.group(1)})"), q)
  if ">>" in q:
    subject, rel = (x.strip() for x in q.split(">>", 1))
    if rel.endswith((" of", " in", " by", " for", " to", " with")):
      return f"what {subject} is {rel}"

    return f"the {rel} of {subject}"

  q = " ".join(q.split()).strip().rstrip("?").strip()
  return q[:1].lower() + q[1:]


def _search(task, k, _):
  hops = task.get("hops") or []
  if not hops:
    return "Search for each part of the question separately, using what one search finds in the next."

  if k == 1:
    plan = "; ".join(f"{i}. {phrase(h['question'], {})}" for i, h in enumerate(hops, 1))
    return f"It takes {len(hops)} lookups, in this order: {plan}."

  if k == 2:
    first = hops[0]
    return (
      f"Step 1, {phrase(first['question'], {})}, is {first['answer']}. "
      "Search with that for the next step."
    )

  found = { i: h["answer"] for i, h in enumerate(hops[:-1], 1) }
  known = "; ".join(f"{phrase(h['question'], {})}: {h['answer']}" for h in hops[:-1])
  last = phrase(hops[-1]["question"], found)
  return f"So far: {known}. What's left is {last}; search for exactly that."


def _math(task, k, _):
  # Paragraphs of the worked solution, never the one with the answer.
  parts = [p for p in re.split(r"\n\s*\n", task.get("solution") or "") if p.strip()]
  parts = [p for p in parts if not _boxed_re.search(p)]
  if len(parts) < 2:
    return _numeric(task["number"], k)

  share = { 1: 1 / 3, 2: 2 / 3, 3: 1.0 }[k]
  n = max(1, min(len(parts) - 1, round(len(parts) * share)))
  return "The first steps of a solution:\n\n" + "\n\n".join(parts[:n])


def _science(task, k, index):
  if k == 1:
    refs = []
    if index is not None:
      refs = [
        d for d in index.search(task["user"], count = 60)
        if d["pid"].startswith("ref-")
      ][:2]

    if not refs:
      return "Identify the physical law or formula that links what is given to what is asked."

    return "These may help:\n\n" + "\n\n".join(f"{d['title']}: {d['text']}" for d in refs)

  v = task["number"]
  unit = re.sub(r"^[\s$\-\d.,]*(?:(?:\\times|×|x|\*)\s*10\^\{?-?\d+\}?)?", "", task.get("answer", "")).strip()
  unit = f" {unit}" if unit and len(unit) <= 20 else ""
  if k == 2:
    if v == 0:
      return f"The answer is 0{unit}."

    exp = math.floor(math.log10(abs(v)))
    sign = "negative, " if v < 0 else ""
    return f"The answer is {sign}of order 10^{exp}{unit}."

  rough = float(f"{v:.1g}")
  return f"The answer is roughly {rough:g}{unit}; compute it exactly."


def _numeric(v, k):
  """Hints from the answer alone, for problems with no worked
  solution: a method, then its kind and size, then a range."""
  if k == 1:
    return (
      "Write down what is given and what is asked, set it up as "
      "equations, and solve or search it with a script (fractions for "
      "exact values, brute force over small cases to spot a pattern)."
    )

  whole = abs(v - round(v)) < 1e-9
  if k == 2:
    kind = "a whole number" if whole else "not a whole number"
    if v == 0:
      return f"The answer is {kind}."

    exp = math.floor(math.log10(abs(v)))
    return f"The answer is {kind}, {'negative, ' if v < 0 else ''}of order 10^{exp}."

  lo, hi = sorted((v * 0.75, v * 1.3))
  if whole:
    lo, hi = math.floor(lo) - 1, math.ceil(hi) + 1
    return f"The answer is a whole number from {lo} to {hi}."

  return f"The answer is between {lo:.3g} and {hi:.3g}."


def _memory(task, k, _):
  name = task["memory"]
  if k == 1:
    return "The memory index in the system prompt may cover this."

  if k == 2:
    return f"Read the memory {name}."

  m = next((m for m in task["memories"] if m["name"] == name), None)
  return f"The memory {name} says: {m['content']}" if m else f"Read the memory {name}."


def _skill(task, k, _):
  if k == 1:
    return "A skill in the index covers this kind of writing."

  if k == 2:
    return f"Load the {task['skill']} skill and follow it exactly."

  body = next(s["body"] for s in task["skills"] if s["name"] == task["skill"])
  return "The rules are:\n" + body


def _save(task, k, _):
  if task["should_save"]:
    tips = [
      "Part of the request matters beyond this session.",
      f"The team's choice of {task['key']} is a lasting project convention.",
      f"Save the {task['key']} convention with memory_save, then answer the arithmetic.",
    ]

  else:
    tips = [
      "Ask yourself whether anything here should outlast this session.",
      "A one-off instruction for a script that gets deleted isn't worth remembering.",
      "Don't save anything; just answer the arithmetic.",
    ]

  return tips[k - 1]
