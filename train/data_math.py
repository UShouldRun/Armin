"""Math (DeepScaleR, MIT) and science (WebInstruct-verified,
Apache-2.0) problems with numeric answers, graded with a relative
tolerance. Only answers that parse as one plain number are kept, so
the grader never has to judge an expression."""

import json
import re

import fetch

MATH_URL = (
  "https://huggingface.co/datasets/agentica-org/"
  "DeepScaleR-Preview-Dataset/resolve/main/deepscaler.json"
)
SCIENCE  = "TIGER-Lab/WebInstruct-verified"
FIELDS   = ["Physics", "Chemistry", "Biology", "Engineering", "Economics"]
TYPES    = ["Float", "Integer", "Percentage"]
PER_PAIR = 400

_plain_re = re.compile(r"^-?\d+(\.\d+)?$")
_frac_re  = re.compile(r"^(-?)\\[dt]?frac\{(\d+)\}\{(\d+)\}$")
_sci_re   = re.compile(
  r"^\s*\$?\s*(-?\d+(?:\.\d+)?)\s*"
  r"(?:(?:\\times|×|x|\*)\s*10\^\{?(-?\d+)\}?|[eE]([-+]?\d+))?"
  r"\s*(%|\\%|percent\b)?\s*(?:[a-zA-Z°/\^\-\d\s\\{}·.]*)$"
)


def math_value(answer):
  a = answer.strip().strip("$").replace(" ", "")
  if _plain_re.match(a):
    return float(a)

  m = _frac_re.match(a)
  if m is not None and int(m.group(3)) != 0:
    v = int(m.group(2)) / int(m.group(3))
    return -v if m.group(1) else v

  return None


def science_value(answer):
  m = _sci_re.match(answer.replace(",", ""))
  if m is None:
    return None

  v = float(m.group(1))
  exp = m.group(2) or m.group(3)
  if exp:
    v *= 10 ** int(exp)

  if m.group(4):
    v /= 100

  return v


def build_math(rng):
  path = fetch.file(MATH_URL, "deepscaler.json")
  with open(path) as f:
    rows = json.load(f)

  tasks = []
  for i, r in enumerate(rows):
    v = math_value(r["answer"])
    if v is None:
      continue

    tasks.append({
      "id":        f"math-{i}",
      "kind":      "math",
      "variant":   "normal",
      "solvable":  True,
      "user":      r["problem"].strip(),
      "solution":  r.get("solution") or "",
      "number":    v,
      "tolerance": 1e-6,
    })

  print(f"math: {len(tasks)}/{len(rows)} with a numeric answer")
  rng.shuffle(tasks)

  return tasks


def build_science(rng):
  tasks = []
  for field in FIELDS:
    for kind in TYPES:
      where = f"\"category\"='{field}' AND \"answer_type\"='{kind}'"
      name = f"webinstruct-{field.lower()}-{kind.lower()}"
      for r in fetch.rows(SCIENCE, name, where = where, limit = PER_PAIR):
        v = science_value(r["answer"])
        if v is None:
          continue

        tasks.append({
          "id":        f"science-{r['id']}",
          "kind":      "science",
          "variant":   "normal",
          "solvable":  True,
          "field":     field,
          "user":      r["question"].strip(),
          "answer":    r["answer"],
          "number":    v,
          "tolerance": 0.02,
        })

  print(f"science: {len(tasks)} with a numeric answer")
  rng.shuffle(tasks)

  return tasks
