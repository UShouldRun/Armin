"""Warm-up SFT with LoRA on data/sft_train.jsonl (multi-turn
episodes with armin's tools), loss on the model's own turns only.
Teaches the answer format, tool use, skipping thought on easy
tasks and calibrated CONFIDENCE lines, before RL sharpens them.

  python sft.py --data sft_train --out out/sft
"""

import argparse
import os

from common import BASE, DATA


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--model",  default = BASE)
  ap.add_argument("--data",   default = "sft_train")
  ap.add_argument("--out",    required = True)
  ap.add_argument("--epochs", type = float, default = 2)
  ap.add_argument("--lr",     type = float, default = 1e-4)
  ap.add_argument("--rank",   type = int, default = 64)
  args = ap.parse_args()

  from datasets import load_dataset
  from peft import LoraConfig
  from trl import SFTConfig, SFTTrainer

  data = load_dataset(
    "json",
    data_files = os.path.join(DATA, args.data + ".jsonl"),
    split      = "train",
  )
  config = SFTConfig(
    output_dir                  = args.out,
    model_init_kwargs           = { "dtype": "bfloat16" },
    max_length                  = 16384,
    assistant_only_loss         = True,
    per_device_train_batch_size = 1,
    gradient_accumulation_steps = 16,
    num_train_epochs            = args.epochs,
    learning_rate               = args.lr,
    lr_scheduler_type           = "cosine",
    warmup_steps                = 0.03,
    bf16                        = True,
    gradient_checkpointing      = True,
    logging_steps               = 10,
    save_strategy               = "epoch",
    report_to                   = "none",
  )
  lora = LoraConfig(
    r              = args.rank,
    lora_alpha     = args.rank * 2,
    lora_dropout   = 0.05,
    target_modules = "all-linear",
    task_type      = "CAUSAL_LM",
  )
  trainer = SFTTrainer(
    model         = args.model,
    args          = config,
    train_dataset = data,
    peft_config   = lora,
  )
  trainer.train()
  trainer.save_model(args.out)


if __name__ == "__main__":
  main()
