#!/usr/bin/env python3
"""Merge a LoRA adapter into its base weights.

The serving profiles load a single model directory, so a fine-tuned adapter has
to be folded into the base before it can be deployed. The merged output is a
normal 7B checkpoint that any matching profile can serve.

Usage:
    python3 merge_lora.py --base-model /models/base \
                          --adapter /lora/<workspace> \
                          --output-dir /merged/<workspace>
"""

from __future__ import annotations

import argparse
import os
import sys

os.environ.setdefault("HF_HUB_OFFLINE", "1")
os.environ.setdefault("TRANSFORMERS_OFFLINE", "1")


def log(message: str) -> None:
    print(f"[merge_lora] {message}", flush=True)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-model", required=True)
    parser.add_argument("--adapter", required=True)
    parser.add_argument("--output-dir", required=True)
    args = parser.parse_args()

    import torch
    from peft import PeftModel
    from transformers import AutoModelForCausalLM, AutoTokenizer

    log(f"torch {torch.__version__}")
    tokenizer = AutoTokenizer.from_pretrained(args.base_model, trust_remote_code=True)
    base = AutoModelForCausalLM.from_pretrained(
        args.base_model,
        dtype=torch.bfloat16,
        device_map="cpu",
        low_cpu_mem_usage=True,
        trust_remote_code=True,
    )
    log("base loaded")

    model = PeftModel.from_pretrained(base, args.adapter)
    merged = model.merge_and_unload()
    merged.config.use_cache = True
    log("adapter merged")

    os.makedirs(args.output_dir, exist_ok=True)
    merged.save_pretrained(args.output_dir, safe_serialization=True)
    tokenizer.save_pretrained(args.output_dir)
    log(f"saved merged model to {args.output_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
