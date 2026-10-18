"""Round 2's progress as a small Markdown file: the stage round2.sh is
in, then one row per 25 RL steps (mean of the steps logged in that
window) from the newest checkpoint's trainer_state.json. Standard
library only; pod-guard.sh reruns it, supervise.sh copies it home.

  python progress.py --rl out/rl2 --log ../round2.log --out out/PROGRESS.md
"""

import argparse
import glob
import json
import os
import re
import time

EVERY = 25
COLS  = [
  ("reward",             "reward"),
  ("correct/search",     "search"),
  ("support/search",     "support"),
  ("correct/science",    "science"),
  ("correct/math",       "math"),
  ("correct/code",       "code"),
  ("correct/memory",     "memory"),
  ("correct/skill",      "skill"),
  ("hallucinated/search", "halluc search"),
  ("process/science",    "workflow sci"),
  ("think_chars",        "think chars"),
  ("tool_calls",         "tools"),
]


def stage(log):
  if not os.path.exists(log):
    return "no log", None

  lines = open(log, errors = "replace").read().replace("\r", "\n").splitlines()
  stages = [l for l in lines if l.startswith("== ")]
  last = stages[-1][3:] if stages else "starting"
  m = re.search(r"--out (\S+)", last)
  return (m.group(1) if m else last[:80]), (lines[-1][:160] if lines else "")


def rows(rl):
  states = sorted(
    glob.glob(os.path.join(rl, "checkpoint-*", "trainer_state.json")),
    key = lambda p: int(p.split("checkpoint-")[1].split("/")[0]),
  )
  if not states:
    return []

  with open(states[-1]) as f:
    logs = [l for l in json.load(f)["log_history"] if "reward" in l]

  out = {}
  for l in logs:
    out.setdefault((l["step"] - 1) // EVERY, []).append(l)

  table = []
  for w, ls in sorted(out.items()):
    row = [f"{w * EVERY + 1}-{max(l['step'] for l in ls)}"]
    for key, _ in COLS:
      vals = [l[key] for l in ls if key in l]
      if not vals:
        row.append("")

      elif key in ("think_chars", "tool_calls"):
        row.append(f"{sum(vals) / len(vals):.0f}" if key == "think_chars" else f"{sum(vals) / len(vals):.1f}")

      else:
        row.append(f"{sum(vals) / len(vals):.2f}")

    table.append(row)

  return table


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--rl",  default = "out/rl2")
  ap.add_argument("--log", default = "../round2.log")
  ap.add_argument("--out", default = "out/PROGRESS.md")
  args = ap.parse_args()

  name, last = stage(args.log)
  steps = open(os.path.join(args.rl, ".steps")).read().strip() if os.path.exists(os.path.join(args.rl, ".steps")) else "?"
  md = [
    "# Round 2 progress",
    "",
    f"Updated {time.strftime('%Y-%m-%d %H:%M UTC', time.gmtime())}. Stage: `{name}`.",
    f"Last log line: `{last}`" if last else "",
    "",
  ]
  table = rows(args.rl)
  if table:
    md += [
      f"RL, one row per {EVERY} steps (rates are shares of episodes; "
      "support = share of the needed paragraphs found):",
      "",
      "| steps | " + " | ".join(c for _, c in COLS) + " |",
      "|---" * (len(COLS) + 1) + "|",
    ]
    md += ["| " + " | ".join(r) + " |" for r in table]

  else:
    md.append(f"RL has not saved a checkpoint yet (steps done: {steps}).")

  tmp = args.out + ".tmp"
  with open(tmp, "w") as f:
    f.write("\n".join(md) + "\n")
  os.replace(tmp, args.out)


if __name__ == "__main__":
  main()
