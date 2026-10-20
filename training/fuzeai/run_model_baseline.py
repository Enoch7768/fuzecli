#!/usr/bin/env python3
"""Run a reproducible prompt-only baseline for a pinned Hugging Face causal LM.

This captures raw generations and timing; it does not claim code correctness.
Use the same tasks, decoding settings, and runtime for base-vs-adapter comparisons.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import platform
import re
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def read_tasks(path: Path) -> list[dict[str, Any]]:
    tasks: list[dict[str, Any]] = []
    seen: set[str] = set()
    with path.open("r", encoding="utf-8") as stream:
        for line_number, line in enumerate(stream, 1):
            if not line.strip():
                continue
            try:
                item = json.loads(line)
            except json.JSONDecodeError as exc:
                raise ValueError(f"{path}:{line_number}: invalid JSON: {exc.msg}") from exc
            if not isinstance(item, dict) or not isinstance(item.get("id"), str) or not isinstance(item.get("prompt"), str):
                raise ValueError(f"{path}:{line_number}: each task needs string id and prompt")
            if not item["id"].strip() or not item["prompt"].strip() or item["id"] in seen:
                raise ValueError(f"{path}:{line_number}: empty prompt/id or duplicate id {item.get('id')!r}")
            seen.add(item["id"])
            tasks.append(item)
    if not tasks:
        raise ValueError(f"no tasks found in {path}")
    return tasks


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tasks", type=Path, default=Path("evals/fuzeai_baseline_v1.jsonl"))
    parser.add_argument("--model", required=True, help="Hugging Face model ID or local model path")
    parser.add_argument("--revision", required=True, help="Immutable model commit SHA; do not use main/latest")
    parser.add_argument("--output", type=Path, required=True, help="Output JSONL path for raw generations")
    parser.add_argument("--max-new-tokens", type=int, default=512)
    parser.add_argument("--load-in-4bit", action="store_true", help="Requires CUDA and bitsandbytes")
    parser.add_argument("--adapter", type=str, default=None, help="Optional PEFT adapter path for a comparable run")
    args = parser.parse_args()

    if args.max_new_tokens < 1 or args.max_new_tokens > 8192:
        parser.error("--max-new-tokens must be between 1 and 8192")
    if not re.fullmatch(r"[0-9a-fA-F]{40}", args.revision):
        parser.error("--revision must be the full 40-character immutable Hugging Face commit SHA")

    tasks_path = args.tasks.resolve()
    if not tasks_path.is_file():
        parser.error(f"task file does not exist: {tasks_path}")
    tasks = read_tasks(tasks_path)

    try:
        import torch
        from transformers import AutoModelForCausalLM, AutoTokenizer
    except ImportError as exc:
        print("Missing dependencies. Install torch and transformers; add accelerate/bitsandbytes for 4-bit.", file=sys.stderr)
        raise SystemExit(2) from exc

    if args.load_in_4bit and not torch.cuda.is_available():
        parser.error("--load-in-4bit requires a CUDA GPU")

    tokenizer = AutoTokenizer.from_pretrained(args.model, revision=args.revision, trust_remote_code=False)
    load_args: dict[str, Any] = {
        "revision": args.revision,
        "trust_remote_code": False,
        "device_map": "auto",
    }
    if args.load_in_4bit:
        try:
            from transformers import BitsAndBytesConfig
        except ImportError as exc:
            raise SystemExit("4-bit mode requires a transformers version with BitsAndBytesConfig and bitsandbytes installed") from exc
        load_args["quantization_config"] = BitsAndBytesConfig(
            load_in_4bit=True,
            bnb_4bit_quant_type="nf4",
            bnb_4bit_use_double_quant=True,
            bnb_4bit_compute_dtype=torch.float16,
        )
    else:
        load_args["torch_dtype"] = torch.float16 if torch.cuda.is_available() else torch.float32

    model = AutoModelForCausalLM.from_pretrained(args.model, **load_args)
    if args.adapter:
        try:
            from peft import PeftModel
        except ImportError as exc:
            raise SystemExit("Loading --adapter requires peft") from exc
        model = PeftModel.from_pretrained(model, args.adapter)
    model.eval()

    args.output.parent.mkdir(parents=True, exist_ok=True)
    environment = {
        "python": platform.python_version(),
        "platform": platform.platform(),
        "torch": torch.__version__,
        "transformers": __import__("transformers").__version__,
        "cuda_available": torch.cuda.is_available(),
        "gpu": torch.cuda.get_device_name(0) if torch.cuda.is_available() else None,
    }
    run_info = {
        "record_type": "run_metadata",
        "started_at_utc": datetime.now(timezone.utc).isoformat(),
        "model": args.model,
        "revision": args.revision,
        "system_prompt": "You are FuzeAI, a practical coding assistant. Provide clear and secure solutions, state important assumptions, and never claim code was executed unless it actually was.",
        "adapter": args.adapter,
        "tasks_path": str(tasks_path),
        "tasks_sha256": sha256(tasks_path),
        "task_count": len(tasks),
        "max_new_tokens": args.max_new_tokens,
        "temperature": 0,
        "sampling": False,
        "load_in_4bit": args.load_in_4bit,
        "environment": environment,
        "limitations": ["Generation-only baseline; output is not automatically considered correct.", "Review task rubrics and run code in isolated fixtures before reporting verified task success."],
    }

    elapsed_values: list[float] = []
    with args.output.open("w", encoding="utf-8") as out:
        out.write(json.dumps(run_info, ensure_ascii=False) + "\n")
        for index, task in enumerate(tasks, 1):
            prompt = task["prompt"]
            messages = [
                {"role": "system", "content": "You are FuzeAI, a practical coding assistant. Provide clear and secure solutions, state important assumptions, and never claim code was executed unless it actually was."},
                {"role": "user", "content": prompt},
            ]
            if hasattr(tokenizer, "apply_chat_template") and tokenizer.chat_template:
                encoded = tokenizer.apply_chat_template(messages, tokenize=True, add_generation_prompt=True, return_tensors="pt")
            else:
                encoded = tokenizer(prompt, return_tensors="pt").input_ids
            encoded = encoded.to(model.device)
            start = time.perf_counter()
            with torch.inference_mode():
                generated = model.generate(
                    input_ids=encoded,
                    max_new_tokens=args.max_new_tokens,
                    do_sample=False,
                    pad_token_id=tokenizer.pad_token_id if tokenizer.pad_token_id is not None else tokenizer.eos_token_id,
                    eos_token_id=tokenizer.eos_token_id,
                )
            elapsed = time.perf_counter() - start
            elapsed_values.append(elapsed)
            new_tokens = generated[0, encoded.shape[-1]:]
            response = tokenizer.decode(new_tokens, skip_special_tokens=True)
            row = {
                "record_type": "generation",
                "task_id": task["id"],
                "category": task.get("category", "uncategorized"),
                "prompt": prompt,
                "rubric": task.get("rubric", []),
                "response": response,
                "elapsed_seconds": round(elapsed, 4),
                "generated_tokens": int(new_tokens.numel()),
                "finished_with_eos": bool(tokenizer.eos_token_id is not None and generated[0, -1].item() == tokenizer.eos_token_id),
            }
            out.write(json.dumps(row, ensure_ascii=False) + "\n")
            out.flush()
            print(f"[{index}/{len(tasks)}] {task['id']}: {elapsed:.1f}s, {row['generated_tokens']} tokens")

    summary = {
        "record_type": "run_summary",
        "finished_at_utc": datetime.now(timezone.utc).isoformat(),
        "task_count": len(tasks),
        "mean_generation_seconds": round(sum(elapsed_values) / len(elapsed_values), 4),
        "raw_output_path": str(args.output.resolve()),
        "raw_output_sha256": sha256(args.output),
        "note": "No correctness score is computed. Score responses against rubrics and execute code checks in isolated fixtures.",
    }
    with args.output.open("a", encoding="utf-8") as out:
        out.write(json.dumps(summary, ensure_ascii=False) + "\n")
    print(json.dumps(summary, indent=2, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
