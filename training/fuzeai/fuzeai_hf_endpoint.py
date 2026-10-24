#!/usr/bin/env python3
"""
FuzeAI Hugging Face endpoint for Kaggle or Google Colab.

Loads r7194714R/FuzeAI-Final in 4-bit on a CUDA GPU and serves:
  GET  /health
  GET  /v1/models
  POST /v1/chat/completions

Run:
  python training/fuzeai/fuzeai_hf_endpoint.py

If the model repository is private, enter a Hugging Face read token when prompted.
The service binds to 127.0.0.1 by default; use cloudflared to expose it temporarily.
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import os
import re
import secrets
import subprocess
import sys
import threading
import time
from getpass import getpass
from typing import Any

MODEL_ID = os.environ.get("FUZEAi_MODEL_ID", "r7194714R/FuzeAI-Final")
HOST = os.environ.get("FUZEAi_HOST", "127.0.0.1")
PORT = int(os.environ.get("FUZEAi_PORT", "8080"))
MAX_NEW_TOKENS = int(os.environ.get("FUZEAi_MAX_NEW_TOKENS", "2048"))


def ensure_dependencies() -> None:
    packages = {
        "transformers": "transformers>=4.48.0",
        "accelerate": "accelerate>=1.2.0",
        "bitsandbytes": "bitsandbytes>=0.45.0",
        "fastapi": "fastapi>=0.115.0",
        "uvicorn": "uvicorn>=0.34.0",
        "huggingface_hub": "huggingface_hub>=0.27.0",
    }
    missing = [package for module, package in packages.items()
               if importlib.util.find_spec(module) is None]
    if missing:
        print("Installing missing dependencies:", ", ".join(missing), flush=True)
        subprocess.check_call([sys.executable, "-m", "pip", "install", "-q", *missing])


def main() -> None:
    parser = argparse.ArgumentParser(description="Serve FuzeAI over an OpenAI-compatible API.")
    parser.add_argument("--host", default=HOST)
    parser.add_argument("--port", type=int, default=PORT)
    parser.add_argument("--model", default=MODEL_ID)
    parser.add_argument("--max-new-tokens", type=int, default=MAX_NEW_TOKENS)
    args = parser.parse_args()

    ensure_dependencies()

    import torch
    from fastapi import FastAPI, HTTPException
    from pydantic import BaseModel
    from transformers import AutoModelForCausalLM, AutoTokenizer, BitsAndBytesConfig
    import uvicorn

    if not torch.cuda.is_available():
        raise RuntimeError(
            "No CUDA GPU detected. In Kaggle/Colab, enable a GPU accelerator, "
            "restart the runtime, and rerun this script."
        )
    if args.max_new_tokens < 1 or args.max_new_tokens > 8192:
        raise ValueError("--max-new-tokens must be between 1 and 8192.")

    token = os.environ.get("HF_TOKEN", "").strip()
    if not token:
        token = getpass("Hugging Face read token (input hidden; needed for private repo): ").strip()
    if not token:
        raise RuntimeError("A Hugging Face token is required for a private model repository.")

    from huggingface_hub import HfApi
    try:
        HfApi(token=token).whoami()
    except Exception as exc:
        raise RuntimeError("Hugging Face token authentication failed.") from exc

    print(f"Loading {args.model} in 4-bit mode...", flush=True)
    print("GPU:", torch.cuda.get_device_name(0), flush=True)
    quant_config = BitsAndBytesConfig(
        load_in_4bit=True,
        bnb_4bit_quant_type="nf4",
        bnb_4bit_use_double_quant=True,
        bnb_4bit_compute_dtype=torch.float16,
    )
    tokenizer = AutoTokenizer.from_pretrained(
        args.model, token=token, trust_remote_code=False
    )
    model = AutoModelForCausalLM.from_pretrained(
        args.model,
        token=token,
        quantization_config=quant_config,
        torch_dtype=torch.float16,
        device_map="auto",
        low_cpu_mem_usage=True,
        trust_remote_code=False,
    )
    model.eval()
    print("Model loaded successfully.", flush=True)

    app = FastAPI(title="FuzeAI OpenAI-compatible endpoint", docs_url=None, redoc_url=None)
    generation_lock = threading.Lock()

    class ChatRequest(BaseModel):
        model: str | None = None
        messages: list[dict[str, Any]]
        temperature: float = 0.2
        top_p: float = 0.9
        max_tokens: int | None = 1024
        stream: bool = False
        tools: list[dict[str, Any]] | None = None
        tool_choice: Any = None
        stop: str | list[str] | None = None

    @app.get("/health")
    def health():
        return {"status": "ok", "model": args.model}

    @app.get("/v1/models")
    def models():
        return {
            "object": "list",
            "data": [{
                "id": args.model,
                "object": "model",
                "created": int(time.time()),
                "owned_by": "fuzeai",
            }],
        }

    def text_from_content(content: Any) -> str:
        if isinstance(content, str):
            return content
        if isinstance(content, list):
            pieces = []
            for part in content:
                if isinstance(part, dict) and part.get("type") == "text":
                    pieces.append(str(part.get("text", "")))
            return "\n".join(pieces)
        return "" if content is None else str(content)

    @app.post("/v1/chat/completions")
    def chat_completions(req: ChatRequest):
        if not req.messages:
            raise HTTPException(status_code=400, detail="messages must not be empty")
        if req.stream:
            raise HTTPException(
                status_code=400,
                detail="Streaming is not supported by this endpoint; use stream=false.",
            )

        messages = []
        for item in req.messages:
            role = str(item.get("role", "user"))
            if role not in {"system", "user", "assistant", "tool"}:
                continue
            entry = {"role": role, "content": text_from_content(item.get("content"))}
            if role == "tool" and item.get("tool_call_id"):
                entry["name"] = str(item["tool_call_id"])
            if role == "assistant" and item.get("tool_calls"):
                entry["tool_calls"] = item["tool_calls"]
            messages.append(entry)

        max_tokens = req.max_tokens or 1024
        max_tokens = max(1, min(int(max_tokens), args.max_new_tokens))
        temperature = max(0.0, min(float(req.temperature), 2.0))
        top_p = max(0.05, min(float(req.top_p), 1.0))

        try:
            try:
                template_args = {
                    "conversation": messages,
                    "tokenize": True,
                    "add_generation_prompt": True,
                    "return_tensors": "pt",
                    "return_dict": True,
                }
                if req.tools:
                    template_args["tools"] = req.tools
                inputs = tokenizer.apply_chat_template(**template_args)
            except Exception as exc:
                if req.tools:
                    raise HTTPException(
                        status_code=400,
                        detail=f"The tokenizer chat template rejected the supplied tool schema: {exc}",
                    )
                raise

            inputs = {k: v.to(model.device) for k, v in inputs.items()}
            prompt_tokens = int(inputs["input_ids"].shape[-1])
            generation_args = {
                **inputs,
                "max_new_tokens": max_tokens,
                "do_sample": temperature > 0,
                "temperature": max(temperature, 0.01),
                "top_p": top_p,
                "pad_token_id": tokenizer.pad_token_id or tokenizer.eos_token_id,
                "eos_token_id": tokenizer.eos_token_id,
            }
            if req.stop:
                stop_words = [req.stop] if isinstance(req.stop, str) else req.stop
                stop_ids = []
                for word in stop_words[:4]:
                    ids = tokenizer.encode(word, add_special_tokens=False)
                    if ids:
                        stop_ids.append(ids)
                # Text stop strings are also trimmed below; don't pass malformed stopping criteria.
            with generation_lock, torch.inference_mode():
                output = model.generate(**generation_args)
            generated_ids = output[0][prompt_tokens:]
            completion_tokens = int(generated_ids.shape[-1])
            generated = tokenizer.decode(generated_ids, skip_special_tokens=False).strip()

            # Qwen-family tool-call templates commonly emit <tool_call>{...}</tool_call>.
            tool_calls = []
            for match in re.findall(r"<tool_call>\s*(\{.*?\})\s*</tool_call>", generated, flags=re.S):
                try:
                    payload = json.loads(match)
                    name = payload.get("name")
                    arguments = payload.get("arguments", {})
                    if name:
                        if isinstance(arguments, str):
                            try:
                                json.loads(arguments)
                            except Exception:
                                arguments = json.dumps({"input": arguments})
                        tool_calls.append({
                            "id": "call_" + secrets.token_hex(8),
                            "type": "function",
                            "function": {
                                "name": str(name),
                                "arguments": arguments if isinstance(arguments, str) else json.dumps(arguments, ensure_ascii=False),
                            },
                        })
                except Exception:
                    continue

            if tool_calls:
                content = re.sub(r"<tool_call>.*?</tool_call>", "", generated, flags=re.S).strip()
                finish_reason = "tool_calls"
                message = {"role": "assistant", "content": content or None, "tool_calls": tool_calls}
            else:
                content = re.sub(r"</?think>", "", generated).strip()
                content = re.sub(r"<\|im_end\|>|<\|endoftext\|>|<\|fim_suffix\|>", "", content).strip()
                if req.stop:
                    stop_words = [req.stop] if isinstance(req.stop, str) else req.stop
                    for word in stop_words:
                        if word and word in content:
                            content = content.split(word, 1)[0]
                finish_reason = "length" if completion_tokens >= max_tokens else "stop"
                message = {"role": "assistant", "content": content}

            return {
                "id": "chatcmpl-" + secrets.token_hex(12),
                "object": "chat.completion",
                "created": int(time.time()),
                "model": args.model,
                "choices": [{
                    "index": 0,
                    "message": message,
                    "finish_reason": finish_reason,
                }],
                "usage": {
                    "prompt_tokens": prompt_tokens,
                    "completion_tokens": completion_tokens,
                    "total_tokens": prompt_tokens + completion_tokens,
                },
            }
        except HTTPException:
            raise
        except Exception as exc:
            print(f"Inference request failed: {type(exc).__name__}: {exc}", flush=True)
            raise HTTPException(status_code=500, detail=f"Inference failed: {type(exc).__name__}")

    print(f"API ready at http://{args.host}:{args.port}/v1", flush=True)
    uvicorn.run(app, host=args.host, port=args.port, log_level="info")


if __name__ == "__main__":
    main()
