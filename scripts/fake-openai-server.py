#!/usr/bin/env python3
"""Tiny OpenAI-compatible streaming fixture used by TL Studio CI.

When FAKE_WRITE_PATH is set, the first provider turn calls Kilo's `write` tool
for that exact path. After Kilo sends the tool result back, the fixture emits
the final E2E marker. This exercises the real production agent loop and a real
workspace mutation without an external API key.
"""

from __future__ import annotations

import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

REPLY = "E2E_PRODUCT_OK"
WRITE_CONTENT = "TL_STUDIO_E2E_OK"
WRITE_PATH = os.environ.get("FAKE_WRITE_PATH", "")


def chat_chunk(delta=None, finish=None, usage=None):
    item = {
        "id": "chatcmpl-tl-studio",
        "object": "chat.completion.chunk",
        "choices": [{"index": 0, "delta": delta or {}}],
    }
    if finish is not None:
        item["choices"][0]["finish_reason"] = finish
    if usage is not None:
        item["usage"] = usage
    return item


def has_tool_result(body: dict) -> bool:
    messages = body.get("messages") if isinstance(body, dict) else None
    if not isinstance(messages, list):
        return False
    return any(isinstance(message, dict) and message.get("role") == "tool" for message in messages)


def chat_events(body: dict):
    if WRITE_PATH and not has_tool_result(body):
        arguments = json.dumps({"filePath": WRITE_PATH, "content": WRITE_CONTENT}, separators=(",", ":"))
        return [
            chat_chunk({"role": "assistant"}),
            chat_chunk({
                "tool_calls": [{
                    "index": 0,
                    "id": "call_tl_studio_write",
                    "type": "function",
                    "function": {"name": "write", "arguments": arguments},
                }]
            }),
            chat_chunk({}, "tool_calls", {
                "prompt_tokens": 8,
                "completion_tokens": 4,
                "total_tokens": 12,
            }),
        ]
    return [
        chat_chunk({"role": "assistant"}),
        chat_chunk({"content": REPLY}),
        chat_chunk({}, "stop", {
            "prompt_tokens": 8,
            "completion_tokens": 4,
            "total_tokens": 12,
        }),
    ]


def response_events(model: str):
    # Kept for providers that use the Responses endpoint. The production E2E
    # fixture uses @ai-sdk/openai-compatible and therefore chat/completions.
    return [
        {
            "type": "response.created",
            "sequence_number": 1,
            "response": {"id": "resp_tl_studio", "created_at": 0, "model": model, "service_tier": None},
        },
        {
            "type": "response.output_item.added",
            "sequence_number": 2,
            "output_index": 0,
            "item": {"type": "message", "id": "msg_tl_studio"},
        },
        {
            "type": "response.output_text.delta",
            "sequence_number": 3,
            "item_id": "msg_tl_studio",
            "delta": REPLY,
            "logprobs": None,
        },
        {
            "type": "response.output_item.done",
            "sequence_number": 4,
            "output_index": 0,
            "item": {"type": "message", "id": "msg_tl_studio"},
        },
        {
            "type": "response.completed",
            "sequence_number": 5,
            "response": {
                "incomplete_details": None,
                "service_tier": None,
                "usage": {
                    "input_tokens": 8,
                    "input_tokens_details": {"cached_tokens": 0},
                    "output_tokens": 4,
                    "output_tokens_details": {"reasoning_tokens": 0},
                },
            },
        },
    ]


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        print("fake-llm:", fmt % args, file=sys.stderr, flush=True)

    def _body(self):
        size = int(self.headers.get("content-length", "0") or "0")
        raw = self.rfile.read(size) if size else b"{}"
        try:
            return json.loads(raw)
        except Exception:
            return {}

    def _sse(self, events):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "close")
        self.end_headers()
        for event in events:
            self.wfile.write(f"data: {json.dumps(event, separators=(',', ':'))}\n\n".encode())
            self.wfile.flush()
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()
        self.close_connection = True

    def do_POST(self):
        body = self._body()
        model = body.get("model") if isinstance(body, dict) else None
        model = model if isinstance(model, str) else "test-model"

        if self.path.endswith("/chat/completions"):
            self._sse(chat_events(body if isinstance(body, dict) else {}))
            return

        if self.path.endswith("/responses"):
            self._sse(response_events(model))
            return

        self.send_response(404)
        self.send_header("Content-Length", "0")
        self.end_headers()


def main():
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    print(f"http://127.0.0.1:{server.server_port}/v1", flush=True)
    try:
        server.serve_forever(poll_interval=0.1)
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
