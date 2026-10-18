"""Summarizes graded rollouts per task family: success, grounding,
hallucination, abstention, calibration (Brier, ECE), thinking and
tool use. Several runs print side by side; --md also writes a
markdown verdict comparing the first run (the base model) with the
last, plus the RL reward trend from the trainer's log.

  python report.py base-eval sft-eval rl-eval-120
  python report.py --md out/REPORT.md --rl-log out/rl base-eval rl-eval-120
  python report.py --md out/REPORT2.md --ref big-eval base-eval r2-eval-80

--ref adds a reference run (the bigger model) as a column of the
verdict and a row of the tables, outside the base -> trained pair.
"""

import argparse
import glob
import json
import os

from common import OUT, read_jsonl

BINS = 10
UNSOLVABLE = ("contradict", "missing", "unanswerable", "absent")
COLS = [
  "n", "solved", "grounded", "halluc", "abstain_ok", "false_abs",
  "format", "brier", "ece", "think", "calls", "searches", "support",
  "process", "hints", "solo",
]


def ece(pairs):
  bins = [[] for _ in range(BINS)]
  for c, y in pairs:
    bins[min(int(c * BINS), BINS - 1)].append((c, y))

  err = 0.0
  for b in bins:
    if b:
      conf = sum(c for c, _ in b) / len(b)
      acc = sum(y for _, y in b) / len(b)
      err += len(b) / len(pairs) * abs(conf - acc)

  return err


def mean(xs):
  return sum(xs) / len(xs) if xs else None


def stats(rs):
  solv = [r for r in rs if r["variant"] not in UNSOLVABLE]
  unsolv = [r for r in rs if r["variant"] in UNSOLVABLE]
  good = [r for r in solv if r["grade"]["correct"]]
  pairs = [
    (r["grade"]["conf"], float(r["grade"]["correct"]))
    for r in rs if r["grade"].get("conf") is not None
  ]
  return {
    "n":          len(rs),
    "solved":     mean([r["grade"]["correct"] for r in solv]),
    "grounded":   mean([r["grade"]["grounded"] for r in good]),
    "halluc":     mean([r["grade"]["hallucinated"] for r in rs]),
    "abstain_ok": mean([r["grade"]["correct"] for r in unsolv]),
    "false_abs":  mean([r["grade"]["abstained"] for r in solv]),
    "format":     mean([r["grade"]["format"] for r in rs]),
    "brier":      mean([(c - y) ** 2 for c, y in pairs]),
    "ece":        ece(pairs) if pairs else None,
    "think":      mean([r["think_chars"] for r in rs]),
    "calls":      mean([r["grade"].get("calls", 0) for r in rs]),
    # Logged from round 2 on; "-" for older runs.
    "searches":   mean([r["grade"]["searches"] for r in rs if "searches" in r["grade"]]),
    "support":    mean([r["grade"]["support"] for r in rs if "support" in r["grade"]]),
    "process":    mean([r["grade"]["process"] for r in rs if r["grade"].get("process") is not None]),
    # Solved without asking: the number to compare with older runs,
    # which had no ask_user.
    "hints":      mean([r["grade"].get("hints", 0) for r in rs]),
    "solo":       mean([r["grade"]["correct"] and not r["grade"].get("hints") for r in solv]),
  }


def summarize(rows, by = "kind"):
  groups = {}
  for r in rows:
    key = r["kind"] if by == "kind" else r["kind"] + "/" + r["variant"]
    groups.setdefault(key, []).append(r)

  out = { "ALL": stats(rows) }
  for kind in sorted(groups):
    out[kind] = stats(groups[kind])

  return out


def fmt(col, v):
  if v is None:
    return "-"

  if col == "n":
    return str(v)

  if col in ("brier", "ece"):
    return f"{v:.3f}"

  if col in ("think", "calls", "searches", "hints"):
    return { "think": f"{v:.0f}", "hints": f"{v:.2f}" }.get(col, f"{v:.1f}")

  return f"{100 * v:.1f}"


BY = "kind"


def load(name):
  return summarize(read_jsonl(os.path.join(OUT, "rollouts", name + ".jsonl")), BY)


def print_table(name, table):
  print(f"\n== {name}")
  print(f"{'family':16}" + "".join(f"{c:>11}" for c in COLS))
  for kind, row in table.items():
    print(f"{kind:16}" + "".join(f"{fmt(c, row[c]):>11}" for c in COLS))


def rl_trend(rl_dir):
  """Reward and per-family metrics early vs late in RL training."""
  states = sorted(
    glob.glob(os.path.join(rl_dir, "checkpoint-*", "trainer_state.json")),
    key = lambda p: int(p.split("checkpoint-")[1].split("/")[0]),
  )
  if not states:
    return None

  with open(states[-1]) as f:
    logs = [l for l in json.load(f)["log_history"] if "reward" in l]

  if len(logs) < 2:
    return None

  k = max(1, len(logs) // 5)
  early, late = logs[:k], logs[-k:]
  keys = ["reward"] + sorted(
    { key for l in logs for key in l if key.startswith(("correct/", "hallucinated/", "grounded/", "format/", "process/", "support/", "searches/", "hints/")) }
  )
  rows = []
  for key in keys:
    a = mean([l[key] for l in early if key in l])
    b = mean([l[key] for l in late if key in l])
    if a is not None and b is not None:
      rows.append((key, a, b))

  # Still climbing if the last fifth beats the fifth before it.
  prev = logs[-2 * k:-k] if len(logs) >= 2 * k else early
  climbing = mean([l["reward"] for l in late]) > mean([l["reward"] for l in prev])
  return {
    "steps":    logs[-1].get("step"),
    "points":   len(logs),
    "rows":     rows,
    "climbing": climbing,
  }


def delta(a, b, lower_better = False, pct = True):
  if a is None or b is None:
    return "-"

  d = b - a
  better = d < 0 if lower_better else d > 0
  sign = "+" if d >= 0 else ""
  mark = "better" if better and abs(d) > 1e-9 else ("worse" if abs(d) > 1e-9 else "same")
  if pct:
    return f"{sign}{100 * d:.1f} pts ({mark})"

  return f"{sign}{d:.3f} ({mark})"


def write_md(path, names, tables, trend, ref = None):
  first, last = tables[0]["ALL"], tables[-1]["ALL"]
  big = ref[1]["ALL"] if ref else None
  L = []
  L.append("# armin model report\n")
  L.append(f"Runs compared: {', '.join(f'`{n}`' for n in names)} "
           f"({first['n']} eval tasks each). First = base model, last = trained.\n")

  L.append("## Verdict, base → trained\n")
  head = f" {ref[0]} |" if ref else ""
  L.append("| Metric | Base | Trained | Change |" + head)
  L.append("|---|---|---|---|" + ("---|" if ref else ""))
  rows = [
    ("Solved (solvable tasks)",            "solved",     False),
    ("Solved without asking for a hint",   "solo",       False),
    ("Grounded (share of solved backed by a tool)", "grounded", False),
    ("Hallucinated",                       "halluc",     True),
    ("Right CANNOT_SOLVE on unsolvable",   "abstain_ok", False),
    ("False CANNOT_SOLVE on solvable",     "false_abs",  True),
    ("Answer format followed",             "format",     False),
  ]
  def p(key, v):
    return "-" if v is None else fmt(key, v) + "%"

  for label, key, low in rows:
    extra = f" {p(key, big[key])} |" if ref else ""
    L.append(f"| {label} | {p(key, first[key])} | {p(key, last[key])} | {delta(first[key], last[key], low)} |" + extra)

  for label, key in (("Brier (calibration, lower is better)", "brier"), ("ECE (calibration, lower is better)", "ece")):
    extra = f" {fmt(key, big[key])} |" if ref else ""
    L.append(f"| {label} | {fmt(key, first[key])} | {fmt(key, last[key])} | {delta(first[key], last[key], True, False)} |" + extra)

  L.append(f"| Thinking (chars per episode) | {fmt('think', first['think'])} | {fmt('think', last['think'])} | |")
  L.append(f"| Tool calls per episode | {fmt('calls', first['calls'])} | {fmt('calls', last['calls'])} | |")
  L.append(f"| Searches per episode | {fmt('searches', first['searches'])} | {fmt('searches', last['searches'])} | |")
  L.append(f"| Workflow followed | {p('process', first['process'])} | {p('process', last['process'])} | |")
  L.append(f"| Hints asked per episode | {fmt('hints', first['hints'])} | {fmt('hints', last['hints'])} | |")
  L.append("")

  # From here on the reference is one more run in the tables.
  if ref:
    names, tables = names + [ref[0]], tables + [ref[1]]

  L.append("## Solved per family\n")
  L.append("| Family | n | " + " | ".join(names) + " |")
  L.append("|---|---|" + "---|" * len(names))
  kinds = sorted({ k for t in tables for k in t if k != "ALL" })
  for kind in kinds:
    cells = [fmt("solved", t.get(kind, {}).get("solved")) for t in tables]
    n = next(t[kind]["n"] for t in tables if kind in t)
    L.append(f"| {kind} | {n} | " + " | ".join(cells) + " |")

  L.append("")
  L.append("## Full tables\n")
  for name, table in zip(names, tables):
    L.append(f"### {name}\n")
    L.append("| family | " + " | ".join(COLS) + " |")
    L.append("|---|" + "---|" * len(COLS))
    for kind, row in table.items():
      L.append(f"| {kind} | " + " | ".join(fmt(c, row[c]) for c in COLS) + " |")

    L.append("")

  if trend is not None:
    L.append(f"## RL training trend ({trend['steps']} steps, {trend['points']} log points)\n")
    L.append("First fifth of training vs last fifth, as logged on the training batches.\n")
    L.append("| Metric | Early | Late |")
    L.append("|---|---|---|")
    for key, a, b in trend["rows"]:
      L.append(f"| {key} | {a:.3f} | {b:.3f} |")

    L.append("")
    L.append(
      "Reward was **still climbing** at the end: more RL steps would likely help."
      if trend["climbing"] else
      "Reward had **flattened** by the end: more of the same RL is unlikely to help much."
    )
    L.append("")

  L.append("Percentages are over the eval tasks in each family; small families "
           "(under ~20 tasks) move a lot on one task.")
  with open(path, "w") as f:
    f.write("\n".join(L) + "\n")


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--md")
  ap.add_argument("--rl-log")
  ap.add_argument("--ref", help = "a reference run, e.g. the bigger model's eval")
  ap.add_argument("--by", choices = ["kind", "variant"], default = "kind",
                  help = "group rows by family, or by family/variant")
  ap.add_argument("runs", nargs = "+")
  args = ap.parse_args()
  global BY
  BY = args.by

  names = [n for n in args.runs if os.path.exists(os.path.join(OUT, "rollouts", n + ".jsonl"))]
  tables = [load(n) for n in names]
  ref = None
  if args.ref and os.path.exists(os.path.join(OUT, "rollouts", args.ref + ".jsonl")):
    ref = (args.ref, load(args.ref))
    print_table(args.ref + " (reference)", ref[1])

  for name, table in zip(names, tables):
    print_table(name, table)

  if args.md and tables:
    trend = rl_trend(args.rl_log) if args.rl_log else None
    write_md(args.md, names, tables, trend, ref)
    print(f"\nwrote {args.md}")


if __name__ == "__main__":
  main()
