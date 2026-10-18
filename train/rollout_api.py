"""rollout.py's episodes against a hosted model on OpenRouter instead
of a local vLLM: same tasks, tools, grading and output rows, so the
results drop into build_sft as teacher episodes. Standard library
only (plus transformers for the tool schemas, as rollout.py).

Stops starting new episodes once the reported cost reaches --max-usd.
Rows are appended as episodes finish, and a rerun skips the ids
already written, so an interrupted run resumes.

  python rollout_api.py --split sft --kinds math,science,search,code \\
    --limit 1000 --out flash-sft --max-usd 10

The key is read from $OPENROUTER_API_KEY, --key-file, or armin's own
~/.config/armin/.env (where armin's OpenRouter models get it).
"""

import argparse
import json
import os
import random
import sys
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

import env as environment
from common import DATA, MAX_TURNS, OUT, final_parse, messages, read_jsonl, reward
from rollout import call_tool, tool_schemas

URL      = "https://openrouter.ai/api/v1/chat/completions"

# For the teacher only: the sft rows rebuild the prompt from the task,
# so neither the reminder nor a reformat request reaches the student.
REMINDER = """

# Format (strict)
Your final message must end with these lines, plain text, no bold:
ANSWER: <only the short answer: a number, name or phrase, nothing else>
CONFIDENCE: <a probability from 0 to 1>
or, when the task can't be done, CANNOT_SOLVE: <the problem> then the \
CONFIDENCE line. A coding task needs only the CONFIDENCE line.

# Workflow (strict)
Math and science: first look up any constant or formula you need \
with web_search, then write your plan with scratchpad_write, then \
compute with run_program, then answer with the number it printed. \
Fact questions: one web_search per fact, notes in scratchpad_write, \
then the cited answer. Code: run_tests after your last change."""

# --guide: what the first teacher run got wrong, per family. Teacher
# only, like REMINDER.
GUIDE = {
  "search": """

# Searching (strict)
The web_search corpus is the only authority. It may disagree with \
what you remember; then the corpus is right, and your answer must be \
what it says. Outside sources (Britannica, official sites) are not \
in it. Use short keyword queries, no quotes or operators. One search \
per hop; once the results answer the question, stop searching and \
answer. At most 8 searches in all. If a needed fact is still missing \
after two tries at that hop, reply CANNOT_SOLVE: <the missing fact>.""",
  "code": """

# Coding (strict)
If a test contradicts the task or another test, or the code needs a \
module that doesn't exist, don't edit the tests and don't write the \
missing module: reply CANNOT_SOLVE: <which test or module, and why>. \
Once run_tests passes, stop testing and give your final message.""",
  "math": """

# Answer (strict)
Think briefly; let run_program do the arithmetic. If the problem has \
several parts, ANSWER with the last part's value only.""",
  "science": """

# Answer (strict)
ANSWER is one number with its unit, the one your script printed, \
nothing else.""",
}
REFORMAT = (
  "Restate your final answer in the required format, nothing else: "
  "ANSWER: <only the short answer> (or CANNOT_SOLVE: <problem>), then "
  "CONFIDENCE: <0 to 1> on its own line. Plain text, no bold."
)
KEY_FILE = os.path.expanduser("~/.config/armin-train/openrouter.key")
ARMIN_ENV = os.path.expanduser("~/.config/armin/.env")


class Budget:
  def __init__(self, limit):
    self.limit = limit
    self.spent = 0.0
    self.lock = threading.Lock()

  def add(self, usd):
    with self.lock:
      self.spent += usd

  def over(self):
    with self.lock:
      return self.spent >= self.limit


def post(key, body, attempts = 8):
  data = json.dumps(body).encode()
  for attempt in range(attempts):
    req = urllib.request.Request(URL, data = data, headers = {
      "Authorization": "Bearer " + key,
      "Content-Type":  "application/json",
    })
    try:
      with urllib.request.urlopen(req, timeout = 600) as r:
        return json.loads(r.read())

    except urllib.error.HTTPError as e:
      text = e.read().decode("utf-8", "replace")[:300]
      # 402: out of credit; 400/401/403: a request that won't succeed.
      if e.code in (400, 401, 402, 403):
        raise RuntimeError(f"HTTP {e.code}: {text}")

      wait = min(2 ** attempt, 60) + random.random()
      print(f"  HTTP {e.code}, retrying in {wait:.0f}s: {text[:120]}", file = sys.stderr)

    except (OSError, json.JSONDecodeError) as e:
      wait = min(2 ** attempt, 60) + random.random()
      print(f"  {e}, retrying in {wait:.0f}s", file = sys.stderr)

    time.sleep(wait)

  raise RuntimeError("giving up after retries")


def wire(prompt, turns):
  """Our message format to OpenAI's: arguments as JSON strings, and
  ids pairing each tool result with its call."""
  out = list(prompt)
  pending = []
  for i, m in enumerate(turns):
    if m["role"] == "assistant":
      msg = { "role": "assistant", "content": m.get("content") or "" }
      calls = m.get("tool_calls") or []
      if calls:
        msg["tool_calls"] = []
        for j, c in enumerate(calls):
          cid = f"call_{i}_{j}"
          args = c["function"].get("arguments") or {}
          msg["tool_calls"].append({
            "id":       cid,
            "type":     "function",
            "function": {
              "name":      c["function"]["name"],
              "arguments": args if isinstance(args, str) else json.dumps(args),
            },
          })
          pending.append(cid)

      out.append(msg)

    else:
      out.append({
        "role":         "tool",
        "tool_call_id": pending.pop(0) if pending else f"call_{i}",
        "content":      m["content"],
      })

  return out


def episode(task, key, model, tools, budget_tokens, budget, max_turns = MAX_TURNS, guide = False):
  env = environment.ArminEnv()
  env.reset(task_id = task["id"])
  prompt = messages(task)
  extra = GUIDE.get(task["kind"], "") if guide else ""
  prompt[0] = dict(prompt[0], content = prompt[0]["content"] + REMINDER + extra)
  turns, spent, final = [], 0, None
  try:
    for _ in range(max_turns):
      if spent >= budget_tokens:
        break

      res = post(key, {
        "model":      model,
        "messages":   wire(prompt, turns),
        "tools":      tools,
        "max_tokens": max(budget_tokens - spent, 256),
        "reasoning":  { "enabled": True },
        "usage":      { "include": True },
      })
      usage = res.get("usage") or {}
      budget.add(float(usage.get("cost") or 0.0))
      spent += int(usage.get("completion_tokens") or 0)
      choice = (res.get("choices") or [{}])[0]
      m = choice.get("message") or {}
      msg = {
        "role":              "assistant",
        "content":           m.get("content") or "",
        "reasoning_content": m.get("reasoning") or "",
      }
      calls = m.get("tool_calls") or []
      if calls:
        msg["tool_calls"] = [
          { "type": "function", "function": {
            "name":      c["function"]["name"],
            "arguments": _args(c["function"].get("arguments")),
          } }
          for c in calls
        ]

      turns.append(msg)
      if choice.get("finish_reason") == "length":
        break

      if not calls:
        final = msg["content"]
        if not _formatted(task, final):
          final = _reformat(key, model, prompt, turns, budget) or final
          msg["content"] = final

        break

      for c in msg["tool_calls"]:
        name, result = call_tool(env, c)
        turns.append({ "role": "tool", "name": name, "content": result })
        spent += len(result) // 3

    if final is None:
      g = { "format": False, "correct": False, "abstained": False,
            "hallucinated": False, "grounded": False, "conf": None,
            "calls": env._calls }

    else:
      g = environment.grade(env, final)

    think = sum(len(m.get("reasoning_content") or "") for m in turns if m["role"] == "assistant")
    return {
      "id":          task["id"],
      "kind":        task["kind"],
      "variant":     task["variant"],
      "mode":        "free",
      "teacher":     model,
      "turns":       turns,
      "final":       final,
      "tokens":      spent,
      "think_chars": think,
      "grade":       g,
      "reward":      reward(task, g, think),
    }

  finally:
    env._cleanup()


def _formatted(task, text):
  f = final_parse(text)
  if f["conf"] is None:
    return False

  if task["kind"] in ("search", "math", "science", "memory"):
    return f["answer"] is not None or f["abstain"] is not None

  return True


def _reformat(key, model, prompt, turns, budget):
  """One follow-up asking for the format; the restated lines replace
  the end of the final message, keeping its reasoning and text."""
  res = post(key, {
    "model":      model,
    "messages":   wire(prompt, turns) + [{ "role": "user", "content": REFORMAT }],
    "max_tokens": 400,
    "reasoning":  { "enabled": False },
    "usage":      { "include": True },
  })
  budget.add(float((res.get("usage") or {}).get("cost") or 0.0))
  lines = ((res.get("choices") or [{}])[0].get("message") or {}).get("content") or ""
  lines = lines.replace("**", "").strip()
  if not lines:
    return None

  body = turns[-1]["content"].rstrip()
  return body + "\n\n" + lines


def _args(a):
  if isinstance(a, dict):
    return a

  try:
    return json.loads(a or "{}")

  except json.JSONDecodeError:
    return a or "{}"


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--model",    default = "qwen/qwen3.8-flash")
  ap.add_argument("--split",    required = True)
  ap.add_argument("--kinds",    default = "")
  ap.add_argument("--limit",    type = int, default = 0)
  ap.add_argument("--budget",   type = int, default = 8192, help = "completion tokens per episode")
  ap.add_argument("--workers",  type = int, default = 16)
  ap.add_argument("--turns",    type = int, default = MAX_TURNS)
  ap.add_argument("--max-usd",  type = float, required = True)
  ap.add_argument("--key-file", default = KEY_FILE)
  ap.add_argument("--out",      required = True)
  ap.add_argument("--ids",      help = "file of task ids to run, one per line")
  ap.add_argument("--guide",    action = "store_true", help = "add GUIDE for the task's family")
  args = ap.parse_args()

  key = os.environ.get("OPENROUTER_API_KEY")
  if not key and os.path.exists(args.key_file):
    with open(args.key_file) as f:
      key = f.read().strip()

  if not key and os.path.exists(ARMIN_ENV):
    with open(ARMIN_ENV) as f:
      for line in f:
        name, _, value = line.strip().partition("=")
        if name.strip() == "OPENROUTER_API_KEY" and value.strip():
          key = value.strip().strip("'\"")

  if not key:
    sys.exit(f"no key: set OPENROUTER_API_KEY, write {args.key_file}, or add it to {ARMIN_ENV}")

  environment.load(args.split)
  tasks = read_jsonl(os.path.join(DATA, args.split + ".jsonl"))
  if args.kinds:
    keep = set(args.kinds.split(","))
    tasks = [t for t in tasks if t["kind"] in keep]

  if args.ids:
    with open(args.ids) as f:
      keep = { l.strip() for l in f if l.strip() }
    tasks = [t for t in tasks if t["id"] in keep]

  if args.limit:
    tasks = tasks[:args.limit]

  path = os.path.join(OUT, "rollouts", args.out + ".jsonl")
  os.makedirs(os.path.dirname(path), exist_ok = True)
  done = { r["id"] for r in read_jsonl(path) } if os.path.exists(path) else set()
  todo = [t for t in tasks if t["id"] not in done]
  print(f"{args.out}: {len(todo)} to run ({len(done)} done), cap ${args.max_usd:.2f}")

  tools = tool_schemas()
  budget = Budget(args.max_usd)
  lock = threading.Lock()
  stats = { "done": 0, "correct": 0, "failed": 0 }

  def work(task):
    if budget.over():
      return

    try:
      row = episode(task, key, args.model, tools, args.budget, budget, args.turns, args.guide)

    except RuntimeError as e:
      with lock:
        stats["failed"] += 1
        print(f"  {task['id']}: {e}", file = sys.stderr)
        if any(f"HTTP {c}" in str(e) for c in (401, 402, 403)):
          budget.add(args.max_usd)   # stop everything: the key or credit is the problem

      return

    with lock:
      with open(path, "a") as f:
        f.write(json.dumps(row) + "\n")

      stats["done"] += 1
      stats["correct"] += bool(row["grade"]["correct"])
      if stats["done"] % 25 == 0:
        print(f"  {stats['done']}/{len(todo)} done, {stats['correct']} correct, ${budget.spent:.2f}", flush = True)

  with ThreadPoolExecutor(args.workers) as pool:
    list(pool.map(work, todo))

  print(f"{args.out}: {stats['done']} episodes, {stats['correct']} correct, "
        f"{stats['failed']} failed, ${budget.spent:.2f} spent")


if __name__ == "__main__":
  main()
