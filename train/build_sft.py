"""Builds the warm-up SFT set from the model's own graded episodes
(self-distillation, no paid teacher). Only grounded successes are
kept: a right answer that no tool backed up teaches recall.

  easy task  -> a grounded success without thinking
  hard task  -> the grounded success that thought the least
  unsolvable -> an episode that named the problem, or a scripted one
                that runs the real tool first (run_tests, web_search)

The CONFIDENCE line is rewritten to the measured success rate of
that mode on that task, so the model starts out calibrated.

  python build_sft.py --nothink base-sft-nt --free base-sft-fr --out sft_train

Search tasks get a scripted multi-hop episode instead (plan in the
scratchpad, one search per hop, notes, cited answer): one search of
the whole question is what the model does on its own, and it can't
answer a multi-hop question. --topup builds a top-up warm-up set:
those search episodes, a bigger model's grounded successes on the
other families (--teacher, its rollouts on the sft split), and a
replay of an earlier set's non-search episodes:

  python build_sft.py --topup --teacher big-sft --replay sft_train --out sft_r2
"""

import argparse
import json
import os
import random
import re
import sys

import env as environment
from common import ANSWER_RULES, DATA, OUT, messages, read_jsonl, write_jsonl
from hints import phrase
from rollout import call_tool, tool_schemas

EASY       = 0.75
MAX_UNSOLV = 0.15
SCRIPTED_P = 0.9
DEMO_P     = 0.8
HINT_P     = 0.7

HOW_TO     = ANSWER_RULES[ANSWER_RULES.index("\n\n# How to work"):]

_conf_re = re.compile(r"CONFIDENCE:\s*\**\s*[0-9.]+")


def with_conf(turns, p):
  p = min(max(round(p * 20) / 20, 0.05), 0.95)
  turns = [dict(m) for m in turns]
  last = turns[-1]
  found = list(_conf_re.finditer(last["content"]))
  if found:
    m = found[-1]
    last["content"] = (
      last["content"][:m.start()] + f"CONFIDENCE: {p:.2f}"
      + last["content"][m.end():]
    )

  return turns


def by_task(name):
  out = {}
  for r in read_jsonl(os.path.join(OUT, "rollouts", name + ".jsonl")):
    out.setdefault(r["id"], []).append(r)

  return out


def good(r):
  return r["grade"]["correct"] and r["grade"]["grounded"]


def rate(rows):
  return sum(map(good, rows)) / len(rows) if rows else 0.0


def best(rows):
  ok = [r for r in rows if good(r)]
  return min(ok, key = lambda r: (r["think_chars"], r["tokens"])) if ok else None


def scripted(task):
  """An abstention that looks before it concludes."""
  why = task["why"][0].upper() + task["why"][1:]
  env = environment.ArminEnv()
  env.reset(task_id = task["id"])
  turns = []
  probe = None
  if task["kind"] == "code":
    probe = { "name": "run_tests", "arguments": {} }

  elif task["kind"] == "search":
    if task.get("hops"):
      env._cleanup()
      return hop_search(task)

    probe = { "name": "web_search", "arguments": { "query": task["user"] } }

  if probe is not None:
    turns.append({
      "role":              "assistant",
      "reasoning_content": "Let me check before answering.",
      "content":           "",
      "tool_calls":        [{ "type": "function", "function": probe }],
    })
    name, result = call_tool(env, probe)
    turns.append({ "role": "tool", "name": name, "content": result })

  turns.append({
    "role":              "assistant",
    "reasoning_content": why + ". Nothing available lets me do this, so I should say so instead of guessing.",
    "content":           f"CANNOT_SOLVE: {why}.\nCONFIDENCE: {SCRIPTED_P:.2f}",
  })
  env._cleanup()

  return turns


class Script:
  """Plays tool calls through the real environment, recording turns."""

  def __init__(self, task):
    self.task = task
    self.env = environment.ArminEnv()
    self.env.reset(task_id = task["id"])
    self.turns = []

  def call(self, think, tool, **args):
    call = { "name": tool, "arguments": args }
    self.turns.append({
      "role":              "assistant",
      "reasoning_content": think,
      "content":           "",
      "tool_calls":        [{ "type": "function", "function": call }],
    })
    name, result = call_tool(self.env, call)
    self.turns.append({ "role": "tool", "name": name, "content": result })

    return result

  def finish(self, think, content, grounded = True):
    self.turns.append({
      "role":              "assistant",
      "reasoning_content": think,
      "content":           content,
    })
    g = environment.grade(self.env, content)
    self.env._cleanup()
    ok = g["correct"] and (g["grounded"] or not grounded)

    return self.turns if ok else None


_ref_re = re.compile(r"#(\d+)")


def _query(q, found):
  """A hop as a search query, earlier answers filled in."""
  q = _ref_re.sub(lambda m: found.get(int(m.group(1)), m.group(0)), q)
  return " ".join(q.replace(">>", " ").split())


def _hit(res, answer):
  low = answer.lower()
  return next((r for r in res["results"] if low in r["snippet"].lower()), None)


def hop_search(task):
  """The workflow for a multi-hop question: write the plan to the
  scratchpad, search once per hop (a second phrasing if the first
  misses), note each finding, then answer citing every hop. On an
  unanswerable task the missing hop ends it with CANNOT_SOLVE.
  None when a solvable hop can't be found this way."""
  hops = task["hops"]
  s = Script(task)
  plan = "\n".join(f"{i}. {phrase(h['question'])}" for i, h in enumerate(hops, 1))
  s.call(
    f"This needs {len(hops)} lookups, each using the one before.",
    "scratchpad_write", name = "plan", content = "Plan:\n" + plan + "\n",
  )
  found, cites, notes = {}, [], []
  for i, h in enumerate(hops, 1):
    want = phrase(h["question"], found)
    q = _query(h["question"], found)
    hit = _hit(json.loads(s.call(f"Step {i}: {want}.", "web_search", query = q)), h["answer"])
    if hit is None:
      alt = q + " " + task["user"] if i == len(hops) else want
      hit = _hit(json.loads(s.call(
        "Not in these results; another phrasing.", "web_search", query = alt, count = 10,
      )), h["answer"])

    if hit is None and task["solvable"] and i < len(hops):
      # Stuck: ask the user, up to the hint that names this step's
      # answer (hint 2 gives step 1, hint 3 every step but the last).
      need = 2 if i == 1 else 3
      reply = { "answer": "" }
      while s.env._hints < need:
        reply = json.loads(s.call(
          f"Two searches didn't find {want}; I'll ask.", "ask_user",
          question = f"I can't find {want}. I searched for: {q}. Can you help?",
        ))
        if h["answer"].lower() in reply["answer"].lower():
          break

      if h["answer"].lower() in reply["answer"].lower():
        found[i] = h["answer"]
        notes.append(f"{i}. {want}: {h['answer']} (from the user)\n")
        continue

    if hit is None:
      if task["solvable"]:
        s.env._cleanup()
        return None

      why = task["why"][0].upper() + task["why"][1:]
      s.call(
        f"No result gives {want}.", "scratchpad_write", name = "plan", append = True,
        content = "Found:\n" + "".join(notes) + f"{i}. {want}: not in any result.\n",
      )
      return s.finish(
        f"Two searches found nothing on {want}; I can't answer without it.",
        f"CANNOT_SOLVE: {why}; nothing found gives {want}.\nCONFIDENCE: {SCRIPTED_P:.2f}",
        grounded = False,
      )

    found[i] = h["answer"]
    cites.append(hit)
    notes.append(f"{i}. {want}: {h['answer']} [{hit['id']}]\n")

  s.call(
    "All steps found; noting them.", "scratchpad_write", name = "plan", append = True,
    content = "Found:\n" + "".join(notes),
  )
  ids = sorted({ c["id"] for c in cites })
  sources = "".join(f"- [{c['title']}]({c['url']})\n" for c in { c["id"]: c for c in cites }.values())
  gold = task["answers"][0]
  conf = HINT_P if s.env._hints else DEMO_P
  return s.finish(
    f"[{cites[-1]['id']}] gives the last step.",
    f"ANSWER: {gold} " + "".join(f"[{i}]" for i in ids)
    + f"\nCONFIDENCE: {conf:.2f}\n\nSources:\n{sources}",
  )


def debug_demo(task):
  """Reproduce, locate, fix, verify: the reported test added and
  run, solution.py read, each bug edited, the tests run again."""
  s = Script(task)
  if task.get("report"):
    tests = task["files"]["test_solution.py"].rstrip() + "\n\n\n" + task["report"] + "\n"
    s.call("First, reproduce the report as a test.", "write_file", path = "test_solution.py", content = tests)

  out = json.loads(s.call("Run the tests to see the failure.", "run_tests"))
  if out["passed"]:
    s.env._cleanup()
    return None

  s.call("The tests are right; the bug is in solution.py.", "read_file", path = "solution.py")
  with open(os.path.join(s.env._dir, "solution.py")) as f:
    text = f.read()

  plan = "\n".join(
    f"- line {b['line']}: `{b['bad']}` -> `{b['good']}`" for b in task["bugs"]
  )
  s.call(
    "Comparing the failing cases with the code, I see the cause.",
    "scratchpad_write", name = "bug", content = "Bug:\n" + plan + "\n",
  )
  for b in task["bugs"]:
    line = text.splitlines()[b["line"] - 1]
    if text.count(line) != 1:
      s.env._cleanup()
      return None

    s.call(
      f"Line {b['line']} should read `{b['good']}`.", "edit_file", path = "solution.py",
      old_string = line, new_string = line.replace(b["bad"], b["good"]),
    )

  out = json.loads(s.call("Verify the fix.", "run_tests"))
  if not out["passed"]:
    s.env._cleanup()
    return None

  return s.finish(
    "Every test passes now.",
    "Fixed solution.py: " + "; ".join(f"line {b['line']} read `{b['bad']}`, now `{b['good']}`" for b in task["bugs"])
    + f".\nCONFIDENCE: {DEMO_P:.2f}",
  )


def demo(task):
  """A right, tool-backed episode for a task the model never got
  right with grounding. None when no honest script exists (math and
  science: a script would only print the known answer)."""
  kind = task["kind"]
  if task.get("bugs"):
    return debug_demo(task)

  s = Script(task)
  if kind == "code":
    s.call("I'll read the visible tests first.", "read_file", path = "test_solution.py")
    s.call("Now the implementation.", "write_file", path = "solution.py", content = task["solution"])
    out = json.loads(s.call("Check it against the tests.", "run_tests"))
    if not out["passed"]:
      return None

    return s.finish("The visible tests pass.", f"solution.py is written and passes the visible tests.\nCONFIDENCE: {DEMO_P:.2f}")

  if kind == "search" and task.get("hops"):
    s.env._cleanup()
    return hop_search(task)

  if kind == "search":
    res = json.loads(s.call("I need to look this up.", "web_search", query = task["user"], count = 10))
    gold = task["answers"][0]
    hit = next((r for r in res["results"] if gold.lower() in r["snippet"].lower()), None)
    if hit is None:
      return None

    return s.finish(
      f"Result [{hit['id']}] states it.",
      f"ANSWER: {gold} [{hit['id']}]\nCONFIDENCE: {DEMO_P:.2f}\n\nSources:\n- [{hit['title']}]({hit['url']})",
    )

  if kind == "memory" and "answers" in task:
    s.call("A memory covers this.", "memory_read", name = task["memory"])
    return s.finish("The memory has the value.", f"ANSWER: {task['answers'][0]}\nCONFIDENCE: {DEMO_P:.2f}")

  if kind == "skill":
    body = json.loads(s.call("A skill covers release notes.", "use_skill", name = task["skill"]))
    rules = { r["type"]: r for r in task["rules"] }
    first = re.sub(r"\\(.)", r"\1", rules["first_line"]["regex"])
    bullet = rules["bullets"]["prefix"]
    changes = [
      re.sub(r"\s*\([A-Z]+-\d+\)", "", l[2:]).strip()
      for l in task["user"].splitlines() if l.startswith("- ")
    ]
    notes = "\n".join([first] + [bullet + c for c in changes][:rules["max_lines"]["n"] - 1])
    return s.finish("Following the skill's format.", f"{notes}\nCONFIDENCE: {DEMO_P:.2f}")

  if kind == "save":
    a, b = map(int, re.findall(r"(\d+) \* (\d+)", task["user"])[0])
    if task["should_save"]:
      s.call(
        "That's a lasting project convention; save it.", "memory_save",
        name = "python-formatter", description = "Formatter for Python in this project",
        content = f"Format Python with {task['key']}; the team agreed on it.",
      )

    s.call("Compute the product.", "run_program", binary = "python3", args = ["-c", f"print({a} * {b})"])
    return s.finish("Done.", f"ANSWER: {a * b}\nCONFIDENCE: {DEMO_P:.2f}")

  s.env._cleanup()
  return None


def pick(task, nothink, free):
  if not task["solvable"]:
    for rows in (free, nothink):
      r = best(rows)
      if r is not None:
        return with_conf(r["turns"], rate(rows)), "abstained"

    return scripted(task), "scripted"

  # The model's own search successes are one search of the whole
  # question; the scripted multi-hop episode is the habit to teach.
  if task["kind"] == "search" and task.get("hops"):
    turns = demo(task)
    if turns is not None:
      return turns, "demo"

  p0, p1 = rate(nothink), rate(free)
  r = best(nothink)
  if p0 >= EASY and r is not None:
    return with_conf(r["turns"], p0), "nothink"

  r = best(free)
  if r is not None:
    return with_conf(r["turns"], p1), "think"

  r = best(nothink)
  if r is not None:
    return with_conf(r["turns"], p0), "nothink"

  turns = demo(task)
  if turns is not None:
    return turns, "demo"

  return None, "unsolved"


def sft_tasks():
  """The sft split, plus round 2's code tasks when built."""
  rows = read_jsonl(os.path.join(DATA, "sft.jsonl"))
  if os.path.exists(os.path.join(DATA, "sftx.jsonl")):
    rows += read_jsonl(os.path.join(DATA, "sftx.jsonl"))

  return rows


def teacher_best(rows):
  """The teacher episode to imitate: right and grounded, the
  workflow followed where there is one, then the least thinking."""
  ok = [r for r in rows if good(r)]
  if not ok:
    return None

  return min(ok, key = lambda r: (
    r["grade"].get("process") is False, r["think_chars"], r["tokens"],
  ))


def with_plan(task, turns):
  """A math or science teacher episode that ran its script without a
  scratchpad plan gets one, written from the teacher's own reasoning
  just before the script: the workflow armin wants, in the teacher's
  words. Other episodes are returned as they are."""
  if task["kind"] not in ("math", "science"):
    return turns

  first = None
  for i, m in enumerate(turns):
    names = [c["function"]["name"] for c in m.get("tool_calls") or []]
    if "scratchpad_write" in names:
      return turns

    if "run_program" in names:
      first = i
      break

  if first is None:
    return turns

  think = " ".join((turns[first].get("reasoning_content") or "").split())
  if len(think) > 600:
    cut = think.rfind(". ", 0, 600)
    think = think[:cut + 1 if cut > 200 else 600]

  if len(think) < environment.PLAN_MIN:
    return turns

  plan = "Plan:\n" + think + "\n"
  write = {
    "role":              "assistant",
    "reasoning_content": turns[first].get("reasoning_content") or "",
    "content":           "",
    "tool_calls":        [{ "type": "function", "function": {
      "name": "scratchpad_write", "arguments": { "name": "plan", "content": plan },
    } }],
  }
  result = {
    "role":    "tool",
    "name":    "scratchpad_write",
    "content": json.dumps({ "name": "plan", "bytes": len(plan) }),
  }
  run = dict(turns[first], reasoning_content = "Now the script, following the plan.")
  return turns[:first] + [write, result, run] + turns[first + 1:]


def topup(args, tools):
  """Multi-hop search episodes for every sft search task; the
  teacher's best episode for every other task it got right (and for
  search tasks with no scripted episode); up to --replay-n non-search
  episodes from --replay, so a top-up warm-up doesn't forget them."""
  teacher = {}
  for name in (args.teacher or "").split(","):
    if name:
      teacher.update(by_task(name))

  rows, counts = [], {}
  for t in sft_tasks():
    turns, why = None, "unsolved"
    if t.get("bugs"):
      turns = debug_demo(t)
      why = "debug" if turns else why

    if t["kind"] == "search" and t.get("hops"):
      turns = hop_search(t)
      if turns is not None:
        asked = any(c["function"]["name"] == "ask_user" for m in turns for c in m.get("tool_calls") or [])
        why = "abstain" if not t["solvable"] else ("asked" if asked else "demo")

    if turns is None and t["id"] in teacher:
      r = teacher_best(teacher[t["id"]])
      if r is not None:
        turns = with_plan(t, with_conf(r["turns"], rate(teacher[t["id"]])))
        why = "teacher/" + t["kind"]

    if turns is None and t["id"] not in teacher and t["kind"] != "search" and not t.get("bugs"):
      continue

    counts[why] = counts.get(why, 0) + 1
    if turns is not None:
      rows.append({ "messages": messages(t) + turns, "tools": tools })

  rng = random.Random(0)
  if args.replay:
    old = [
      _current(r, tools) for r in read_jsonl(os.path.join(DATA, args.replay + ".jsonl"))
      if not _is_search(r)
    ]
    rng.shuffle(old)
    rows += old[:args.replay_n]
    counts["replay"] = min(len(old), args.replay_n)

  rng.shuffle(rows)
  return rows, counts


def _current(row, tools):
  """An episode from an older set, with today's ANSWER_RULES and
  tool list."""
  system = row["messages"][0]
  if HOW_TO not in system["content"]:
    system["content"] += HOW_TO

  row["tools"] = tools
  return row


def _is_search(row):
  return any(
    c["function"]["name"] == "web_search"
    for m in row["messages"] for c in m.get("tool_calls") or []
  )


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--nothink")
  ap.add_argument("--free")
  ap.add_argument("--out",         required = True)
  ap.add_argument("--topup",       action = "store_true")
  ap.add_argument("--teacher",     help = "a bigger model's rollouts on the sft split(s), comma-separated")
  ap.add_argument("--replay",      help = "earlier sft set to mix in (not its search episodes)")
  ap.add_argument("--replay-n",    type = int, default = 400)
  args = ap.parse_args()

  environment.load(*(["sft", "sftx"] if os.path.exists(os.path.join(DATA, "sftx.jsonl")) else ["sft"]))
  tools = json.dumps(tool_schemas())
  if args.topup:
    rows, counts = topup(args, tools)
    if not rows:
      sys.exit("build_sft: no usable episodes; refusing to write an empty set")

    write_jsonl(os.path.join(DATA, args.out + ".jsonl"), rows)
    print(f"{args.out}: {len(rows)} episodes {counts}")
    return

  if not (args.nothink and args.free):
    ap.error("--nothink and --free are required without --topup")

  nothink, free = by_task(args.nothink), by_task(args.free)
  solved, unsolv, counts = [], [], {}
  for t in read_jsonl(os.path.join(DATA, "sft.jsonl")):
    if t["id"] not in nothink and t["id"] not in free:
      continue

    turns, why = pick(t, nothink.get(t["id"], []), free.get(t["id"], []))
    counts[why] = counts.get(why, 0) + 1
    if turns is None:
      continue

    row = { "messages": messages(t) + turns, "tools": tools }
    (solved if t["solvable"] else unsolv).append(row)

  cap = int(len(solved) * MAX_UNSOLV / (1 - MAX_UNSOLV))
  rng = random.Random(0)
  rng.shuffle(unsolv)
  rows = solved + unsolv[:cap]
  rng.shuffle(rows)
  if not rows:
    sys.exit("build_sft: no usable episodes; refusing to write an empty set")

  write_jsonl(os.path.join(DATA, args.out + ".jsonl"), rows)
  print(f"{args.out}: {len(rows)} episodes {counts}")


if __name__ == "__main__":
  main()
