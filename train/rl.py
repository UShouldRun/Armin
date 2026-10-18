"""Multi-turn GRPO with LoRA: the model works each task through
armin's tools (ArminEnv) and is scored by env.grade. Reward per
episode, from common.reward:

  correct - (confidence - correct)^2   calibration (RLCR, Brier)
  - 0.5  called a solvable task impossible
  - 0.5  hallucinated: invented a module, answered an unanswerable
         question, or gave the remembered answer over the searched one
  - 0.3  right but ungrounded: no test run, no cited result, no
         run_program, no memory_read / use_skill behind it
  - 0.3 * easy * thinking / budget
  + 0.2 * share of the needed paragraphs retrieved (search)
  - 0.05 per repeated search query, up to 4
  + 0.15 followed the workflow: plan in the scratchpad, then the
         script whose output is the answer (math, science); search
         with notes (search)
  - 0.15, 0.2, 0.25 for the 1st, 2nd, 3rd ask_user hint
  + 0.1  solved after a hint
  -1     malformed or truncated

--search-weight repeats each search task in the pool; with --gens
12 --prompts 3 a step costs about the same as 8 x 4 but each
question gets more tries, so a rare multi-hop success is likelier
to show up in its group. --curriculum orders the pool easy to hard
(2-hop questions first) instead of shuffling.

  python rl.py --model out/sft-merged --tasks rl.labeled --out out/rl [--resume]
"""

import argparse
import math
import os
import random

import env as environment
from common import MAX_TURNS, messages, reward


def final_turn(completion):
  for m in reversed(completion):
    if m.get("role") == "assistant" and not m.get("tool_calls"):
      return m.get("content") or ""

  return None


def score(prompts, completions, environments, log_metric, **_):
  rewards, stats = [], {}
  for completion, e in zip(completions, environments):
    final = final_turn(completion)
    if final is None:
      g = { "format": False, "correct": False, "hallucinated": False,
            "grounded": False, "abstained": False }

    else:
      g = environment.grade(e, final)

    think = sum(
      len(m.get("reasoning_content") or "") for m in completion
      if m.get("role") == "assistant"
    )
    rewards.append(reward(e._task, g, think))
    kind = e._task["kind"]
    for key in ("format", "correct", "hallucinated", "grounded"):
      stats.setdefault(f"{key}/{kind}", []).append(float(g[key]))

    if g.get("process") is not None:
      stats.setdefault(f"process/{kind}", []).append(float(g["process"]))

    if kind == "search":
      stats.setdefault("support/search", []).append(g.get("support", 0.0))

    stats.setdefault(f"searches/{kind}", []).append(e._trace.count("search"))
    stats.setdefault(f"hints/{kind}", []).append(e._hints)

    stats.setdefault("think_chars", []).append(think)
    stats.setdefault("tool_calls", []).append(e._calls)

  for name, values in stats.items():
    log_metric(name, sum(values) / len(values))

  return rewards


def deadline_callback(deadline):
  """Stops training before the next step would end past `deadline`
  (unix seconds), saving a checkpoint so a later run can resume."""
  import time
  from transformers import TrainerCallback

  class Deadline(TrainerCallback):
    def __init__(self):
      self.last = time.time()
      self.mean = None

    def on_step_end(self, args, state, control, **_):
      now = time.time()
      took = now - self.last
      self.last = now
      self.mean = took if self.mean is None else 0.8 * self.mean + 0.2 * took
      if now + self.mean > deadline:
        print(f"deadline: stopping after step {state.global_step}")
        control.should_save = True
        control.should_training_stop = True

      return control

  return Deadline()


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--model",   required = True)
  ap.add_argument("--tasks",   default = "rl.labeled")
  ap.add_argument("--out",     required = True)
  ap.add_argument("--steps",   type = int, default = 250)
  ap.add_argument("--lr",      type = float, default = 2e-5)
  ap.add_argument("--rank",    type = int, default = 64)
  ap.add_argument("--gens",    type = int, default = 8)
  ap.add_argument("--prompts", type = int, default = 4)
  ap.add_argument("--batch",   type = int, default = 2)
  ap.add_argument("--max-len", type = int, default = 8192)
  ap.add_argument("--search-weight", type = int, default = 1)
  ap.add_argument("--curriculum", action = "store_true")
  ap.add_argument("--resume",  action = "store_true")
  ap.add_argument("--deadline", type = float, default = 0,
                  help = "unix time; stop (and checkpoint) before it")
  args = ap.parse_args()

  from datasets import Dataset
  from peft import LoraConfig
  from trl import GRPOConfig, GRPOTrainer

  environment.load(args.tasks)
  rng = random.Random(0)
  pool = []
  for tid, t in environment.TASKS.items():
    copies = args.search_weight if t["kind"] == "search" else 1
    for _ in range(copies):
      # Search: 2, 3, 4 hops in that order; the rest spread evenly.
      level = len(t["hops"]) - 2 if t.get("hops") else rng.randint(0, 2)
      pool.append((level, rng.random(), tid, t))

  if args.curriculum:
    pool.sort(key = lambda x: x[:2])

  data = Dataset.from_list([
    { "prompt": messages(t), "task_id": tid } for _, _, tid, t in pool
  ])

  config = GRPOConfig(
    output_dir                  = args.out,
    model_init_kwargs           = { "dtype": "bfloat16" },
    learning_rate               = args.lr,
    lr_scheduler_type           = "constant_with_warmup",
    warmup_steps                = 10,
    max_steps                   = args.steps,
    # prompts * gens episodes per step, trained `batch` at a time:
    # 8 long sequences at once ran an 8B out of memory on 80 GB.
    per_device_train_batch_size = args.batch,
    gradient_accumulation_steps = args.prompts * args.gens // args.batch,
    num_generations             = args.gens,
    max_completion_length       = args.max_len,
    max_tool_calling_iterations = MAX_TURNS,
    shuffle_dataset             = not args.curriculum,
    temperature                 = 0.8,
    top_p                       = 0.95,
    beta                        = 0.0,
    loss_type                   = "dapo",
    scale_rewards               = "none",
    mask_truncated_completions  = True,
    use_vllm                    = True,
    vllm_mode                   = "colocate",
    vllm_gpu_memory_utilization = 0.35,
    # Frees vLLM's weights and KV cache while the trainer backpropagates.
    vllm_enable_sleep_mode      = True,
    vllm_max_model_length       = args.max_len + 8192,
    bf16                        = True,
    gradient_checkpointing      = True,
    logging_steps               = 5,
    # Divides the step count, so the last step is always a checkpoint
    # and a later run with more --steps resumes exactly where this ended.
    save_steps                  = math.gcd(25, args.steps),
    save_total_limit            = 2,
    report_to                   = "none",
  )
  lora = LoraConfig(
    r              = args.rank,
    lora_alpha     = args.rank * 2,
    target_modules = "all-linear",
    task_type      = "CAUSAL_LM",
  )
  trainer = GRPOTrainer(
    model               = args.model,
    reward_funcs        = score,
    args                = config,
    train_dataset       = data,
    peft_config         = lora,
    environment_factory = environment.ArminEnv,
  )
  if args.deadline:
    trainer.add_callback(deadline_callback(args.deadline))

  resume = None
  if args.resume and os.path.isdir(args.out):
    found = [d for d in os.listdir(args.out) if d.startswith("checkpoint-")]
    resume = True if found else None

  trainer.train(resume_from_checkpoint = resume)
  trainer.save_model(args.out)
  with open(os.path.join(args.out, ".steps"), "w") as f:
    f.write(str(trainer.state.global_step))


if __name__ == "__main__":
  main()
