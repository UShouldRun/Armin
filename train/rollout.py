"""Multi-turn episodes with vLLM against ArminEnv, graded. Used for
difficulty labels, self-distillation and evaluation. Rendering and
reply parsing are TRL's own, so episodes look exactly like the
ones the RL trainer generates.

  python rollout.py --model M --split eval --mode free --n 1 --out base-eval
"""

import argparse
import inspect
import json
import os
from concurrent.futures import ThreadPoolExecutor

import env as environment
from common import (
  BASE, DATA, MAX_TURNS, OUT, messages, read_jsonl, reward, write_jsonl,
)

def tool_names():
  return [
    n for n, _ in inspect.getmembers(environment.ArminEnv, inspect.isfunction)
    if not n.startswith("_") and n not in ("reset", "get_reward")
  ]


def tool_schemas():
  from transformers.utils import get_json_schema

  return [get_json_schema(getattr(environment.ArminEnv, n)) for n in tool_names()]


def call_tool(env, call):
  fn = call.get("function", call)
  name = fn.get("name", "unknown")
  args = fn.get("arguments") or {}
  if isinstance(args, str):
    try:
      args = json.loads(args)

    except json.JSONDecodeError:
      return name, str({ "error": "arguments are not valid JSON" })

  if name not in tool_names():
    return name, str({ "error": f"Tool {name} not found." })

  try:
    return name, str(getattr(env, name)(**args))

  except Exception as e:
    return name, str({ "error": str(e) })


class Episode:
  def __init__(self, task, seed):
    self.task = task
    self.seed = seed
    self.env = environment.ArminEnv()
    self.env.reset(task_id = task["id"])
    self.prompt = messages(task)
    self.turns = []
    self.spent = 0
    self.final = None
    self.done = False


def run(llm, tok, tasks, n, think, budget, temperature):
  from trl.chat_template_utils import add_response_schema, get_training_chat_template
  from vllm import SamplingParams

  tok = add_response_schema(tok)
  template = get_training_chat_template(tok) or tok.chat_template
  schemas = tool_schemas()
  eps = [Episode(t, 1000 * i + k) for i, t in enumerate(tasks) for k in range(n)]

  for turn in range(MAX_TURNS):
    live = [e for e in eps if not e.done]
    if not live:
      break

    prompts = [
      tok.apply_chat_template(
        e.prompt + e.turns,
        tools                 = schemas,
        chat_template         = template,
        add_generation_prompt = True,
        enable_thinking       = think,
        tokenize              = False,
      )
      for e in live
    ]
    params = [
      SamplingParams(
        temperature = temperature,
        top_p       = 0.95 if think else 0.8,
        top_k       = 20,
        max_tokens  = max(budget - e.spent, 1),
        seed        = e.seed + turn,
      )
      for e in live
    ]
    outs = llm.generate(prompts, params)

    failed = []

    def step(item):
      e, out, prompt = item
      o = out.outputs[0]
      e.spent += len(o.token_ids)
      # The parser needs the prompt to see what the template prefilled
      # (an empty think block in nothink mode, <think> for some models).
      try:
        msg = dict(tok.parse_response(o.text, prefix = prompt))

      except Exception as err:
        failed.append(str(err)[:200])
        msg = { "content": o.text }

      msg["role"] = "assistant"
      e.turns.append(msg)
      if o.finish_reason == "length" or e.spent >= budget:
        e.done = True
        return

      calls = msg.get("tool_calls") or []
      if not calls:
        e.final = msg.get("content") or ""
        e.done = True
        return

      for call in calls:
        name, result = call_tool(e.env, call)
        e.turns.append({ "role": "tool", "name": name, "content": result })
        # Tool output counts against the budget, as it does in TRL.
        e.spent += len(result) // 3

    # Tool calls run pytest; a container's cpu_count is the host's.
    with ThreadPoolExecutor(min(os.cpu_count() or 4, 16)) as pool:
      list(pool.map(step, zip(live, outs, prompts)))

    if failed:
      print(f"turn {turn}: {len(failed)} replies failed to parse: {failed[0]}")

  rows = []
  for e in eps:
    if e.final is None:
      g = { "format": False, "correct": False, "abstained": False,
            "hallucinated": False, "grounded": False, "conf": None,
            "calls": e.env._calls }

    else:
      g = environment.grade(e.env, e.final)

    think_chars = sum(
      len(m.get("reasoning_content") or "") for m in e.turns
      if m["role"] == "assistant"
    )
    rows.append({
      "id":          e.task["id"],
      "kind":        e.task["kind"],
      "variant":     e.task["variant"],
      "mode":        "free" if think else "nothink",
      "turns":       e.turns,
      "final":       e.final,
      "tokens":      e.spent,
      "think_chars": think_chars,
      "grade":       g,
      "reward":      reward(e.task, g, think_chars),
    })
    e.env._cleanup()

  return rows


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--model",       default = BASE)
  ap.add_argument("--split",       required = True)
  ap.add_argument("--mode",        choices = ["free", "nothink"], default = "free")
  ap.add_argument("--n",           type = int, default = 1)
  ap.add_argument("--limit",       type = int, default = 0)
  ap.add_argument("--kinds",       default = "")
  ap.add_argument("--budget",      type = int, default = 8192)
  ap.add_argument("--temperature", type = float, default = 0.0)
  ap.add_argument("--out",         required = True)
  ap.add_argument("--text-only",   action = "store_true",
                  help = "vision-language model: skip its image and video inputs")
  args = ap.parse_args()

  from vllm import LLM

  environment.load(args.split)
  tasks = read_jsonl(os.path.join(DATA, args.split + ".jsonl"))
  if args.kinds:
    keep = set(args.kinds.split(","))
    tasks = [t for t in tasks if t["kind"] in keep]

  if args.limit:
    tasks = tasks[:args.limit]

  think = args.mode == "free"
  temperature = args.temperature or (0.6 if think else 0.7)
  llm = LLM(
    model                  = args.model,
    dtype                  = "bfloat16",
    max_model_len          = args.budget + 8192,
    gpu_memory_utilization = 0.9,
    enable_prefix_caching  = True,
    # Hybrid (Mamba) models need a cache block per sequence; the 27B
    # fits 797, under vLLM's default of 1024.
    max_num_seqs           = 512,
    seed                   = 0,
    **({ "limit_mm_per_prompt": { "image": 0, "video": 0 } } if args.text_only else {}),
  )
  rows = run(llm, llm.get_tokenizer(), tasks, args.n, think, args.budget, temperature)
  write_jsonl(os.path.join(OUT, "rollouts", args.out + ".jsonl"), rows)
  done = sum(r["grade"]["correct"] for r in rows)
  print(f"{args.out}: {done}/{len(rows)} correct")


if __name__ == "__main__":
  main()
