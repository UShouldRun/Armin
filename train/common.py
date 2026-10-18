"""Paths, prompts, final-answer parsing, reward and file helpers
shared by every step. Standard library only."""

import json
import os
import re

ROOT = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(ROOT, "data")
OUT  = os.path.join(ROOT, "out")
BASE = os.environ.get("ARMIN_BASE", "Qwen/Qwen3-8B")

# Reward weights; README.md explains each one.
W_FALSE_ABSTAIN = 0.5
W_HALLUCINATION = 0.5
W_UNGROUNDED    = 0.3
W_THINK         = 0.3
THINK_BUDGET    = 12000
W_RETRIEVE      = 0.2    # x share of the needed paragraphs retrieved
W_REPEAT        = 0.05   # per repeated search query, up to 4
W_PROCESS       = 0.15   # followed the workflow (env._process)
# ask_user hints: each costs more than the last, since it tells
# more; solving after a hint wins a little back. Solving alone (1.0)
# beats 1, 2, 3 hints and a solve (0.95, 0.75, 0.5), which beat
# failing (about 0) or a false CANNOT_SOLVE (-0.5).
HINT_COST       = (0.15, 0.2, 0.25)
W_HINT_SOLVED   = 0.1
MAX_TURNS       = 12

# Appended to armin's own system prompt. armin should carry the same
# block when it runs this model, so CONFIDENCE can drive routing.
ANSWER_RULES = """# Final answer
Finish with a message that calls no tool. Its last line is:
CONFIDENCE: <probability from 0 to 1 that your answer is correct>
- When the task asks a question, put the short answer on its own \
line before that: ANSWER: <answer>
- When the task cannot be done with what the project, memories, \
skills and tools give you (requirements that contradict each \
other; a file, module, memory, skill or fact that does not exist \
or cannot be found), write CANNOT_SOLVE: <the exact problem> \
instead of an answer. Never fill the gap from what you remember.
- Facts come from tools (web_search, files, memories), not from \
what you remember. Numbers come from run_program, not from mental \
arithmetic. Code is checked with run_tests before you finish.
- Think only as much as the task needs; easy tasks need none.

# How to work
- Science and math: look up each constant, formula or data value \
you need with web_search, as many searches as it takes. Write a \
short plan to the scratchpad (scratchpad_write), then write and \
run a script (run_program) that computes the answer, and answer \
with the number it printed. A problem that gives its numbers is \
solvable.
- Questions of fact: search once per fact you need, putting what \
earlier results told you into the next query. Keep what you found \
in the scratchpad, then answer from the results, citing them.
- Stuck after trying the tools: ask_user, at most 3 times. Prefer \
working it out yourself."""

_think_re   = re.compile(r"^\s*<think>(.*?)</think>\s*", re.S)
_conf_re    = re.compile(r"CONFIDENCE:\s*\**\s*([01](?:\.\d+)?|\.\d+)")
_answer_re  = re.compile(r"^\s*\**ANSWER:?\**:?\s*(.+?)\s*$", re.M)
_abstain_re = re.compile(r"CANNOT_SOLVE:\s*(.+)")
_cite_re    = re.compile(r"\[(\d{1,3})\]")


def system_prompt(task):
  with open(os.path.join(DATA, "system.txt")) as f:
    text = f.read().strip()

  if task.get("memories"):
    text += "\n\n# Memories (memory_read for details)\n"
    text += "".join(
      f"- {m['name']} ({m['scope']}): {m['description']}\n"
      for m in task["memories"]
    )

  if task.get("skills"):
    text += "\n\n# Skills (use_skill to load)\n"
    text += "".join(
      f"- {s['name']}: {s['description']}\n" for s in task["skills"]
    )

  return text.rstrip() + "\n\n" + ANSWER_RULES


def messages(task):
  return [
    { "role": "system", "content": system_prompt(task) },
    { "role": "user",   "content": task["user"] },
  ]


def split_think(text):
  m = _think_re.match(text or "")
  if m is None:
    return "", text or ""

  return m.group(1).strip(), text[m.end():]


def final_parse(text):
  _, body = split_think(text)
  confs = _conf_re.findall(body)
  answers = _answer_re.findall(body)
  abstain = _abstain_re.search(body)

  return {
    "conf":    min(max(float(confs[-1]), 0.0), 1.0) if confs else None,
    "answer":  answers[-1] if answers else None,
    "abstain": abstain.group(1).strip() if abstain else None,
    "cites":   sorted({ int(c) for c in _cite_re.findall(body) }),
    "body":    body,
  }


def reward(task, g, think_chars):
  if not g["format"]:
    return -1.0

  y = 1.0 if g["correct"] else 0.0
  r = y - (g["conf"] - y) ** 2
  if g["abstained"] and task["solvable"]:
    r -= W_FALSE_ABSTAIN

  if g["hallucinated"]:
    r -= W_HALLUCINATION

  if g["correct"] and not g["grounded"]:
    r -= W_UNGROUNDED

  # Shaping toward the workflow; small next to being right.
  r += W_RETRIEVE * g.get("support", 0.0)
  r -= W_REPEAT * min(g.get("repeats", 0), 4)
  if g.get("process"):
    r += W_PROCESS

  used = g.get("hints", 0)
  r -= sum(HINT_COST[:used])
  if used and g["correct"]:
    r += W_HINT_SOLVED

  spent = min(think_chars / THINK_BUDGET, 1.0)
  r -= W_THINK * task.get("easy", 0.0) * spent

  return r


def read_jsonl(path):
  with open(path) as f:
    return [json.loads(line) for line in f if line.strip()]


def write_jsonl(path, rows):
  os.makedirs(os.path.dirname(path), exist_ok = True)
  tmp = path + ".tmp"
  with open(tmp, "w") as f:
    for row in rows:
      f.write(json.dumps(row) + "\n")

  os.replace(tmp, path)


def tasks_by_id(*names):
  out = {}
  for name in names:
    for t in read_jsonl(os.path.join(DATA, name + ".jsonl")):
      out[t["id"]] = t

  return out
