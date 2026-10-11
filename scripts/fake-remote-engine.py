#!/usr/bin/env python3
"""fake-remote-engine.py — deterministic OpenAI-compatible SSE engine fixture.

Stands in for the remote LLM backend the same way internal/api's
switchableEngine does for the Go unit tests: /v1/chat/completions answers
one streamed assistant message, in chunks, so the REAL run pipeline, the
REAL activity hub and the REAL Qt UI exercise genuine streaming — the
fixture replaces the model, never the product.
"""
import json
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

CHUNKS = ["Hello", " — ", "I ", "am ", "here ", "and ", "working", " ", "fine."]


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"status":"fixture engine ready"}')

    def do_POST(self):
        if not self.path.endswith("/chat/completions"):
            self.send_response(404)
            self.end_headers()
            return

        length = int(self.headers.get("Content-Length", "0") or 0)
        if length:
            self.rfile.read(length)

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()

        def emit(chunk):
            data = json.dumps({
                "id": "chatcmpl-fixture",
                "choices": [{"index": 0,
                             "delta": {"role": "assistant", "content": chunk},
                             "finish_reason": None}],
            })
            self.wfile.write(("data: " + data + "\n\n").encode())
            self.wfile.flush()

        try:
            for c in CHUNKS:
                emit(c)
                threading.Event().wait(0.15)
            self.wfile.write(b"data: [DONE]\n\n")
            self.wfile.flush()
        except BrokenPipeError:
            pass


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 18642
    srv = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"fixture engine on 127.0.0.1:{port}", flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
