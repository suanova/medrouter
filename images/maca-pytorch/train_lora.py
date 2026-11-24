#!/usr/bin/env python3
"""LoRA supervised fine-tuning for MedRouter's one-click fine-tune.

The recipe is fixed on purpose: a job gets a base model directory, a dataset
directory of Parquet files, and an output directory. Everything else — LoRA
rank, learning rate, epochs — is a constant so two runs of the same model and
dataset produce the same result.

Dataset contract for the text-qa task (MedMCQA / PubMedQA style):
    question + options (opa..opd) + cop   -- MedMCQA
    question + context + label            -- PubMedQA
The loader accepts either, and fails loudly when neither fits rather than
silently training on garbage.

Usage:
    python3 train_lora.py --base-model /models/base \
                          --dataset-dir /data/medmcqa \
                          --output-dir /out/run-1 \
                          [--smoke] [--max-samples N] [--dry-run]
"""

from __future__ import annotations

import argparse
import glob
import json
import os
import sys

os.environ.setdefault("HF_HUB_OFFLINE", "1")
os.environ.setdefault("TRANSFORMERS_OFFLINE", "1")
os.environ.setdefault("TOKENIZERS_PARALLELISM", "false")

LORA_RANK = 16
LORA_ALPHA = 32
LORA_DROPOUT = 0.05
LEARNING_RATE = 2e-4
EPOCHS = 2
MAX_SEQ_LEN = 1024
# batch 4 x accum 2 keeps the effective batch at 8 while giving the GPU enough
# work per forward pass; packing fills 1024-token windows instead of wasting
# them on the ~215-token average MedMCQA sample.
BATCH_SIZE = 4
GRAD_ACCUM = 2
PACKING = True
LETTERS = ["A", "B", "C", "D"]


def log(message: str) -> None:
    print(f"[train_lora] {message}", flush=True)


def load_parquet(dataset_dir: str):
    import pandas as pd

    files = sorted(glob.glob(os.path.join(dataset_dir, "**", "*.parquet"), recursive=True))
    if not files:
        raise SystemExit(f"no parquet files under {dataset_dir}")
    log(f"found {len(files)} parquet file(s)")
    frames = []
    for path in files:
        frame = pd.read_parquet(path)
        log(f"  {os.path.basename(path)}: {len(frame)} rows, columns={list(frame.columns)[:10]}")
        frames.append(frame)
    import pandas

    return pandas.concat(frames, ignore_index=True)


def pick(row, *names):
    for name in names:
        if name in row and row[name] is not None:
            return row[name]
    return None


def to_text(row) -> str | None:
    """Render one row into a prompt+answer text. Returns None if unusable."""
    question = pick(row, "question", "prompt", "input", "text")
    if question is None:
        return None

    # MedMCQA: four options plus an index into them.
    options = [pick(row, f"op{c}") for c in ("a", "b", "c", "d", "1", "2", "3", "4")]
    options = [str(o) for o in options if o is not None and str(o).strip()]
    cop = pick(row, "cop", "label", "answer", "correct", "output", "final_decision")

    answer = None
    if cop is not None:
        try:
            index = int(cop)
            if 0 <= index < len(options):
                answer = LETTERS[index]
            elif 1 <= index <= len(options):
                answer = LETTERS[index - 1]
        except (TypeError, ValueError):
            raw = str(cop).strip()
            if options:
                # Multiple-choice rows must resolve to a letter; anything else
                # would teach the model an inconsistent format.
                if raw.upper()[:1] in LETTERS:
                    answer = raw.upper()[:1]
            else:
                answer = raw

    if answer is None:
        return None

    if options:
        prompt = f"Question: {question}\n" + "\n".join(
            f"{LETTERS[i]}. {text}" for i, text in enumerate(options)
        ) + "\nAnswer:"
    else:
        prompt = f"Question: {question}\nAnswer:"

    explanation = pick(row, "exp", "explanation", "rationale", "long_answer")
    completion = answer
    if explanation and str(explanation).strip():
        completion = f"{answer}. {str(explanation).strip()}"
    return f"{prompt} {completion}"


def build_dataset(frame, max_samples: int):
    from datasets import Dataset

    texts, skipped = [], 0
    for _, row in frame.iterrows():
        text = to_text(row.to_dict())
        if text:
            texts.append({"text": text})
        else:
            skipped += 1
        if max_samples and len(texts) >= max_samples:
            break
    if not texts:
        raise SystemExit("no rows produced a prompt; check the dataset columns")
    log(f"built {len(texts)} training rows ({skipped} skipped)")
    return Dataset.from_list(texts)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-model", required=True)
    parser.add_argument("--dataset-dir", required=True)
    parser.add_argument("--output-dir", required=True)
    parser.add_argument("--max-samples", type=int, default=0)
    parser.add_argument("--batch-size", type=int, default=BATCH_SIZE)
    parser.add_argument("--grad-accum", type=int, default=GRAD_ACCUM)
    parser.add_argument("--no-packing", action="store_true", help="disable sequence packing")
    parser.add_argument("--smoke", action="store_true", help="20 steps, tiny sample")
    parser.add_argument("--dry-run", action="store_true", help="print the recipe and exit")
    args = parser.parse_args()

    frame = load_parquet(args.dataset_dir)
    sample_limit = args.max_samples or (400 if args.smoke else 0)
    batch_size = max(1, args.batch_size)
    grad_accum = max(1, args.grad_accum)
    packing = PACKING and not args.no_packing
    dataset = build_dataset(frame, sample_limit)
    log(f"sample:\n{dataset[0]['text'][:400]}")

    recipe = {
        "base_model": args.base_model,
        "dataset_dir": args.dataset_dir,
        "output_dir": args.output_dir,
        "rows": len(dataset),
        "lora": {"r": LORA_RANK, "alpha": LORA_ALPHA, "dropout": LORA_DROPOUT},
        "learning_rate": LEARNING_RATE,
        "epochs": EPOCHS,
        "max_seq_len": MAX_SEQ_LEN,
        "batch_size": batch_size,
        "grad_accum": grad_accum,
        "packing": packing,
        "smoke": args.smoke,
    }
    log("recipe: " + json.dumps(recipe, ensure_ascii=False))

    if args.dry_run:
        return 0

    import torch
    from peft import LoraConfig
    from transformers import AutoModelForCausalLM, AutoTokenizer
    from trl import SFTConfig, SFTTrainer

    log(f"torch {torch.__version__} cuda={torch.cuda.is_available()} devices={torch.cuda.device_count()}")

    tokenizer = AutoTokenizer.from_pretrained(args.base_model, trust_remote_code=True)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token
    model = AutoModelForCausalLM.from_pretrained(
        args.base_model,
        dtype=torch.bfloat16,
        device_map={"": 0},
        trust_remote_code=True,
    )
    model.config.use_cache = False

    peft_config = LoraConfig(
        r=LORA_RANK,
        lora_alpha=LORA_ALPHA,
        lora_dropout=LORA_DROPOUT,
        bias="none",
        task_type="CAUSAL_LM",
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj"],
    )

    training_args = SFTConfig(
        output_dir=args.output_dir,
        num_train_epochs=EPOCHS,
        per_device_train_batch_size=batch_size,
        gradient_accumulation_steps=grad_accum,
        learning_rate=LEARNING_RATE,
        bf16=True,
        logging_steps=5,
        save_strategy="no",
        max_length=MAX_SEQ_LEN,
        dataset_text_field="text",
        packing=packing,
        report_to=[],
        max_steps=20 if args.smoke else -1,
    )

    trainer = SFTTrainer(
        model=model,
        args=training_args,
        train_dataset=dataset,
        peft_config=peft_config,
        processing_class=tokenizer,
    )
    trainer.train()

    os.makedirs(args.output_dir, exist_ok=True)
    trainer.save_model(args.output_dir)
    tokenizer.save_pretrained(args.output_dir)
    with open(os.path.join(args.output_dir, "recipe.json"), "w", encoding="utf-8") as handle:
        json.dump(recipe, handle, ensure_ascii=False, indent=2)
    log(f"saved adapter to {args.output_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
