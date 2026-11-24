#!/usr/bin/env python3
"""A minimal OpenAI-compatible chat server for base-vs-adapter demos.

Why this exists: pulling the full sglang image into the cluster stalled, and a
demo only needs one request at a time. This uses the training image that is
already cached, serves both the base model and the LoRA adapter from one
process, and has no dependencies beyond the standard library plus what the
image already ships.

It is deliberately simple: requests are serialized, decoding is greedy by
default, and there is no batching. Do not use it for production traffic.

Endpoints:
    GET  /health
    GET  /v1/models
    POST /v1/chat/completions
"""

from __future__ import annotations

import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

os.environ.setdefault("HF_HUB_OFFLINE", "1")
os.environ.setdefault("TRANSFORMERS_OFFLINE", "1")

BASE_MODEL = os.environ.get("BASE_MODEL", "/models/base")
ADAPTER = os.environ.get("ADAPTER", "/adapter")
BASE_NAME = os.environ.get("BASE_NAME", "biomistral-base")
ADAPTER_NAME = os.environ.get("ADAPTER_NAME", "medmcqa-lora")
PORT = int(os.environ.get("PORT", "30000"))
MAX_NEW_TOKENS = int(os.environ.get("MAX_NEW_TOKENS", "160"))
# The recipe trains on raw text, not on a chat template. Serving through the
# template puts the adapter out of distribution, so default to passing the
# message content through unchanged.
RAW_PROMPT = os.environ.get("RAW_PROMPT", "1") == "1"

_lock = threading.Lock()
_model = None
_tokenizer = None


def log(message: str) -> None:
    print(f"[serve_lora] {message}", flush=True)


def load():
    global _model, _tokenizer
    import torch
    from peft import PeftModel
    from transformers import AutoModelForCausalLM, AutoTokenizer

    log(f"torch {torch.__version__} cuda={torch.cuda.is_available()}")
    _tokenizer = AutoTokenizer.from_pretrained(BASE_MODEL, trust_remote_code=True)
    if _tokenizer.pad_token is None:
        _tokenizer.pad_token = _tokenizer.eos_token

    base = AutoModelForCausalLM.from_pretrained(
        BASE_MODEL, dtype=torch.bfloat16, device_map={"": 0}, trust_remote_code=True
    )
    base.config.use_cache = True
    _model = PeftModel.from_pretrained(base, ADAPTER)
    _model.eval()
    log("model + adapter loaded")


def build_prompt(messages) -> str:
    if RAW_PROMPT:
        return "\n".join(str(message.get("content", "")) for message in messages)
    if hasattr(_tokenizer, "apply_chat_template"):
        try:
            return _tokenizer.apply_chat_template(
                messages, tokenize=False, add_generation_prompt=True
            )
        except Exception:
            pass
    parts = []
    for message in messages:
        parts.append(f"{message.get('role', 'user')}\n{message.get('content', '')}")
    parts.append("assistant\n")
    return "\n".join(parts)


def generate(model_name: str, messages, max_tokens: int):
    import torch

    prompt = build_prompt(messages)
    inputs = _tokenizer(prompt, return_tensors="pt").to("cuda")
    adapter = model_name == ADAPTER_NAME
    with _lock, torch.inference_mode():
        if adapter:
            output = _model.generate(
                **inputs,
                max_new_tokens=max_tokens,
                do_sample=False,
                pad_token_id=_tokenizer.eos_token_id,
            )
        else:
            # The adapter is attached to the same weights; disabling it yields
            # the untouched base model, which is the "before" side of the demo.
            with _model.disable_adapter():
                output = _model.generate(
                    **inputs,
                    max_new_tokens=max_tokens,
                    do_sample=False,
                    pad_token_id=_tokenizer.eos_token_id,
                )
    text = _tokenizer.decode(output[0][inputs["input_ids"].shape[1]:], skip_special_tokens=True)
    return text.strip()


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        log(fmt % args)

    def _json(self, status: int, payload: dict):
        body = json.dumps(payload, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self._json(200, {"status": "ok"})
        elif self.path == "/v1/models":
            self._json(200, {"object": "list", "data": [
                {"id": BASE_NAME, "object": "model"},
                {"id": ADAPTER_NAME, "object": "model"},
            ]})
        else:
            self._json(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/v1/chat/completions":
            self._json(404, {"error": "not found"})
            return
        length = int(self.headers.get("Content-Length", "0"))
        try:
            request = json.loads(self.rfile.read(length) or b"{}")
        except json.JSONDecodeError:
            self._json(400, {"error": "invalid json"})
            return

        model_name = request.get("model", ADAPTER_NAME)
        messages = request.get("messages") or []
        max_tokens = int(request.get("max_tokens") or MAX_NEW_TOKENS)
        try:
            text = generate(model_name, messages, max_tokens)
        except Exception as error:  # surface the failure instead of hanging
            log(f"generate failed: {error}")
            self._json(500, {"error": {"message": str(error), "type": "server_error"}})
            return

        self._json(200, {
            "id": "chatcmpl-local",
            "object": "chat.completion",
            "model": model_name,
            "choices": [{"index": 0, "message": {"role": "assistant", "content": text}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
        })


def main():
    load()
    server = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    log(f"listening on :{PORT}")
    server.serve_forever()


if __name__ == "__main__":
    main()
