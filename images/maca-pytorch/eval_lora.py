#!/usr/bin/env python3
"""Compare a base model against the same model with a LoRA adapter attached.

Designed for a demo that has to be reproducible: a fixed slice of the
validation split, greedy decoding, the exact training prompt, and metrics that
are string comparisons rather than human judgement.

Usage:
    python3 eval_lora.py --base-model /models/base \
                         --adapter /out/<workspace> \
                         --dataset-dir /data/medmcqa \
                         --samples 100 --output /out/eval

Outputs a markdown summary on stdout and a JSON report next to it.
"""

from __future__ import annotations

import argparse
import glob
import json
import os
import random
import re
import sys

os.environ.setdefault("HF_HUB_OFFLINE", "1")
os.environ.setdefault("TRANSFORMERS_OFFLINE", "1")

LETTERS = ["A", "B", "C", "D"]
PROMPT_SUFFIX = "\nAnswer:"
ANSWER_RE = re.compile(r"Answer:\s*([A-D])\b", re.IGNORECASE)
ANY_LETTER_RE = re.compile(r"\b([A-D])\b")


def log(message: str) -> None:
    print(f"[eval_lora] {message}", flush=True)


def load_rows(dataset_dir: str, samples: int, seed: int):
    import pandas as pd

    files = sorted(glob.glob(os.path.join(dataset_dir, "**", "validation*.parquet"), recursive=True))
    if not files:
        files = sorted(glob.glob(os.path.join(dataset_dir, "**", "*.parquet"), recursive=True))
    if not files:
        raise SystemExit(f"no parquet files under {dataset_dir}")
    frame = pd.concat([pd.read_parquet(path) for path in files], ignore_index=True)

    rows = []
    for _, row in frame.iterrows():
        item = row.to_dict()
        gold = item.get("cop")
        try:
            index = int(gold)
        except (TypeError, ValueError):
            continue
        options = [item.get(f"op{letter}") for letter in ("a", "b", "c", "d")]
        if any(option is None or str(option).strip() == "" for option in options):
            continue
        if not (0 <= index < len(options)):
            continue
        rows.append(
            {
                "id": str(item.get("id", len(rows))),
                "question": str(item.get("question", "")).strip(),
                "options": [str(option).strip() for option in options],
                "gold": LETTERS[index],
            }
        )
    if not rows:
        raise SystemExit("no rows with a usable label")

    random.Random(seed).shuffle(rows)
    return rows[:samples]


def build_prompt(row) -> str:
    lines = [f"Question: {row['question']}"]
    lines += [f"{LETTERS[i]}. {text}" for i, text in enumerate(row["options"])]
    return "\n".join(lines) + PROMPT_SUFFIX


def extract_answer(text: str) -> str:
    match = ANSWER_RE.search(text)
    if match:
        return match.group(1).upper()
    match = ANY_LETTER_RE.search(text)
    return match.group(1).upper() if match else "none"


def evaluate(model, tokenizer, rows, max_new_tokens: int):
    import torch

    results = []
    for index, row in enumerate(rows, 1):
        prompt = build_prompt(row)
        inputs = tokenizer(prompt, return_tensors="pt").to("cuda")
        with torch.inference_mode():
            output = model.generate(
                **inputs,
                max_new_tokens=max_new_tokens,
                do_sample=False,
                temperature=None,
                top_p=None,
                pad_token_id=tokenizer.eos_token_id,
            )
        generated = tokenizer.decode(output[0][inputs["input_ids"].shape[1]:], skip_special_tokens=True)
        answer = extract_answer(generated)
        results.append(
            {
                "id": row["id"],
                "question": row["question"],
                "options": row["options"],
                "gold": row["gold"],
                "answer": answer,
                "formatted": bool(ANSWER_RE.search(generated)),
                "generated": generated.strip()[:600],
            }
        )
        if index % 10 == 0 or index == len(rows):
            log(f"  {index}/{len(rows)}")
    return results


def summarize(label: str, results):
    total = len(results)
    formatted = sum(1 for item in results if item["formatted"])
    correct = sum(1 for item in results if item["answer"] == item["gold"])
    unknown = sum(1 for item in results if item["answer"] == "none")
    length = sum(len(item["generated"]) for item in results) / max(1, total)
    return {
        "label": label,
        "samples": total,
        "format_rate": round(formatted / max(1, total), 4),
        "accuracy": round(correct / max(1, total), 4),
        "unparseable": unknown,
        "avg_chars": round(length, 1),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-model", required=True)
    parser.add_argument("--adapter", required=True)
    parser.add_argument("--dataset-dir", required=True)
    parser.add_argument("--samples", type=int, default=100)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--max-new-tokens", type=int, default=96)
    parser.add_argument("--output-dir", required=True)
    args = parser.parse_args()

    rows = load_rows(args.dataset_dir, args.samples, args.seed)
    log(f"evaluating {len(rows)} held-out rows (seed={args.seed})")

    import torch
    from peft import PeftModel
    from transformers import AutoModelForCausalLM, AutoTokenizer

    log(f"torch {torch.__version__} cuda={torch.cuda.is_available()}")
    tokenizer = AutoTokenizer.from_pretrained(args.base_model, trust_remote_code=True)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    base = AutoModelForCausalLM.from_pretrained(
        args.base_model, dtype=torch.bfloat16, device_map={"": 0}, trust_remote_code=True
    )
    base.config.use_cache = True

    log("pass 1/2: base")
    base_results = evaluate(base, tokenizer, rows, args.max_new_tokens)

    log("pass 2/2: base + adapter")
    tuned = PeftModel.from_pretrained(base, args.adapter)
    tuned.eval()
    tuned_results = evaluate(tuned, tokenizer, rows, args.max_new_tokens)

    report = {
        "base_model": args.base_model,
        "adapter": args.adapter,
        "seed": args.seed,
        "summary": [summarize("base", base_results), summarize("adapter", tuned_results)],
        "items": [
            {"row": row, "base": base_item, "adapter": tuned_item}
            for row, base_item, tuned_item in zip(rows, base_results, tuned_results)
        ],
    }

    os.makedirs(args.output_dir, exist_ok=True)
    with open(os.path.join(args.output_dir, "eval.json"), "w", encoding="utf-8") as handle:
        json.dump(report, handle, ensure_ascii=False, indent=2)

    print("\n| run | samples | format rate | accuracy | unparseable | avg chars |")
    print("|---|---|---|---|---|---|")
    for row in report["summary"]:
        print(
            f"| {row['label']} | {row['samples']} | {row['format_rate']:.0%} | "
            f"{row['accuracy']:.0%} | {row['unparseable']} | {row['avg_chars']} |"
        )
    return 0


if __name__ == "__main__":
    sys.exit(main())
