"""Builds every task family, splits each into eval / sft / rl, adds
the unsolvable and counterfactual variants per split (so no variant
leaks across splits), and writes:

  data/{eval,sft,rl}.jsonl   tasks
  data/{eval,sft,rl}x.jsonl  round 2's code tasks (data_hard), split
                             the same way; their own seed, so the
                             splits above stay as they were
  data/corpus.jsonl          the web_search corpus (MuSiQue paragraphs,
                             then the science reference library)
  data/system.txt            armin's system prompt, from prompt.go

Standard library + pytest (to verify the code references)."""

import os
import random
import re

import data_code
import data_hard
import data_math
import data_search
import data_synth
import fetch
import science_ref
from common import DATA, ROOT, write_jsonl

SEED = 0

# eval, sft, rl tasks per family, before variants
SIZES = {
  "code":    (150, 600, 1200),
  "search":  (150, 500, 1000),
  "math":    (80, 250, 500),
  "science": (60, 200, 400),
  "memory":  (60, 200, 400),
  "skill":   (40, 120, 250),
  "save":    (40, 120, 250),
}
# share of each split that gets an extra variant task
VARIANTS = { "code": 0.12, "search": 0.30 }

PROMPT_GO = os.path.join(ROOT, "..", "internal", "think", "prompt.go")


def armin_prompt():
  # A GPU box with only train/ on it reuses the prompt saved by an
  # earlier prepare, so every round trains on the same one.
  saved = os.path.join(DATA, "system.txt")
  if not os.path.exists(PROMPT_GO) and os.path.exists(saved):
    with open(saved) as f:
      return f.read().strip()

  with open(PROMPT_GO) as f:
    src = f.read()

  m = re.search(r"const basePrompt = `(.*?)`", src, re.S)
  if m is None:
    raise RuntimeError("basePrompt not found in " + PROMPT_GO)

  return m.group(1).strip()


def main():
  rng = random.Random(SEED)
  os.makedirs(DATA, exist_ok = True)
  system = armin_prompt()

  total = { k: sum(v) for k, v in SIZES.items() }
  memory, skill, save = data_synth.build(
    rng, total["memory"], total["skill"], total["save"],
  )
  families = {
    "code":    data_code.build(rng),
    "search":  data_search.build(rng),
    "math":    data_math.build_math(rng),
    "science": data_math.build_science(rng),
    "memory":  memory,
    "skill":   skill,
    "save":    save,
  }
  makers = { "code": data_code.unsolvable, "search": data_search.variant }

  splits = { "eval": [], "sft": [], "rl": [] }
  for kind, tasks in families.items():
    start = 0
    for name, n in zip(splits, SIZES[kind]):
      part = tasks[start:start + n]
      start += n
      if len(part) < n:
        print(f"  {kind}/{name}: only {len(part)} of {n} tasks")

      extra = []
      share = VARIANTS.get(kind, 0)
      for t in rng.sample(part, int(len(part) * share)):
        v = makers[kind](t, rng)
        if v is not None:
          extra.append(v)

      splits[name] += part + extra

  for name, rows in splits.items():
    rng.shuffle(rows)
    write_jsonl(os.path.join(DATA, name + ".jsonl"), rows)
    counts = {}
    for t in rows:
      key = t["kind"] + "/" + t["variant"]
      counts[key] = counts.get(key, 0) + 1

    print(f"{name}: {len(rows)} tasks")
    for key in sorted(counts):
      print(f"  {key:24} {counts[key]}")

  # Round 2's code families, from KodCode tasks no split uses.
  used = {
    re.sub(r"-(contra|missing)$", "", t["id"])
    for rows in splits.values() for t in rows if t["kind"] == "code"
  }
  raw = { r["question_id"]: r for r in fetch.rows(data_code.DATASET, "kodcode") }
  extra = data_hard.build(families["code"], used, raw, random.Random(SEED + 1))
  xsplits = { "eval": [], "sft": [], "rl": [] }
  for fam, tasks in extra.items():
    start = 0
    for name, n in zip(xsplits, data_hard.SIZES[fam]):
      xsplits[name] += tasks[start:start + n]
      start += n

  xrng = random.Random(SEED + 2)
  for name, rows in xsplits.items():
    xrng.shuffle(rows)
    write_jsonl(os.path.join(DATA, name + "x.jsonl"), rows)
    print(f"{name}x: {len(rows)} tasks")

  # After the variants, so the library can't change which paragraphs
  # a search variant hides or rewrites.
  science_ref.append(os.path.join(DATA, "corpus.jsonl"))

  # Written last: run.sh treats it as "prepare finished".
  with open(os.path.join(DATA, "system.txt"), "w") as f:
    f.write(system + "\n")


if __name__ == "__main__":
  main()
