"""Merges a LoRA adapter into its base model, so vLLM, the next
training stage and llama.cpp's GGUF converter see plain weights.

  python merge.py --base Qwen/Qwen3-8B --adapter out/sft --out out/sft-merged
"""

import argparse


def main():
  ap = argparse.ArgumentParser()
  ap.add_argument("--base",    required = True)
  ap.add_argument("--adapter", required = True)
  ap.add_argument("--out",     required = True)
  args = ap.parse_args()

  from peft import PeftModel
  from transformers import AutoModelForCausalLM, AutoTokenizer

  model = AutoModelForCausalLM.from_pretrained(args.base, dtype = "bfloat16")
  model = PeftModel.from_pretrained(model, args.adapter).merge_and_unload()
  model.save_pretrained(args.out)
  AutoTokenizer.from_pretrained(args.base).save_pretrained(args.out)


if __name__ == "__main__":
  main()
