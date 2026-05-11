#!/usr/bin/env python3
"""Single local operator-attention listener for dev-all agent messages.

The listener intentionally stores only bounded dashboard/status metadata and
pointer references. It rejects raw payload/log/content fields so the devkit
inbox does not become a second evidence store.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import html
import json
import os
import re
import threading
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, urlparse


MAX_BODY_BYTES = 16 * 1024
MAX_SUMMARY_CHARS = 240
MAX_POINTERS = 8
MAX_LABEL_CHARS = 120
MAX_URI_CHARS = 512
MAX_KIND_CHARS = 80
DEFAULT_LIMIT = 100
SENDER_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,79}$")
ALLOWED_CATEGORIES = {
    "iam_network",
    "governance_approval",
    "dependency_tooling_approval",
    "operator_input",
}
FORBIDDEN_KEYS = {
    "body",
    "content",
    "evidence",
    "log",
    "logs",
    "payload",
    "raw",
    "secret",
    "secrets",
    "stacktrace",
    "transcript",
}


def now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def reject_forbidden_keys(value: Any, path: str = "") -> None:
    if isinstance(value, dict):
        for key, child in value.items():
            key_text = str(key)
            if key_text.lower() in FORBIDDEN_KEYS:
                where = f" at {path}.{key_text}" if path else f" {key_text}"
                raise ValueError(f"raw payload fields are not accepted:{where}")
            reject_forbidden_keys(child, f"{path}.{key_text}" if path else key_text)
    elif isinstance(value, list):
        for index, child in enumerate(value):
            reject_forbidden_keys(child, f"{path}[{index}]")


def bounded_text(value: Any, field: str, max_len: int, *, required: bool = True) -> str:
    if value is None:
        if required:
            raise ValueError(f"{field} is required")
        return ""
    text = str(value).strip()
    if required and not text:
        raise ValueError(f"{field} is required")
    if len(text) > max_len:
        raise ValueError(f"{field} exceeds {max_len} characters")
    return text


def normalize_sender(payload: dict[str, Any], query: dict[str, list[str]], headers: dict[str, str]) -> str:
    sender = (
        bounded_text(payload.get("senderAgent"), "senderAgent", 80, required=False)
        or bounded_text(payload.get("sender"), "sender", 80, required=False)
        or bounded_text(headers.get("x-sender-agent"), "X-Sender-Agent", 80, required=False)
        or bounded_text(query.get("sender", [""])[0], "sender", 80, required=False)
    )
    if not sender:
        raise ValueError("senderAgent is required")
    if not SENDER_PATTERN.match(sender):
        raise ValueError("senderAgent contains unsupported characters")
    return sender


def normalize_category(value: Any) -> str:
    category = bounded_text(value, "category", 80)
    if category not in ALLOWED_CATEGORIES:
        allowed = ", ".join(sorted(ALLOWED_CATEGORIES))
        raise ValueError(f"category must be one of: {allowed}")
    return category


def normalize_pointers(value: Any) -> list[dict[str, str]]:
    if not isinstance(value, list) or not value:
        raise ValueError("pointers must be a non-empty array")
    if len(value) > MAX_POINTERS:
        raise ValueError(f"pointers exceeds {MAX_POINTERS} entries")
    pointers: list[dict[str, str]] = []
    for index, pointer in enumerate(value):
        if not isinstance(pointer, dict):
            raise ValueError(f"pointers[{index}] must be an object")
        pointers.append(
            {
                "kind": bounded_text(pointer.get("kind"), f"pointers[{index}].kind", MAX_KIND_CHARS),
                "uri": bounded_text(pointer.get("uri"), f"pointers[{index}].uri", MAX_URI_CHARS),
                "label": bounded_text(pointer.get("label"), f"pointers[{index}].label", MAX_LABEL_CHARS),
            }
        )
    return pointers


def normalize_message(payload: dict[str, Any], query: dict[str, list[str]], headers: dict[str, str]) -> dict[str, Any]:
    reject_forbidden_keys(payload)
    sender = normalize_sender(payload, query, headers)
    category = normalize_category(payload.get("category"))
    summary = bounded_text(payload.get("summary"), "summary", MAX_SUMMARY_CHARS)
    pointers = normalize_pointers(payload.get("pointers"))
    received_at = now_iso()
    canonical = json.dumps(
        {
            "senderAgent": sender,
            "category": category,
            "summary": summary,
            "pointers": pointers,
            "reportedAt": bounded_text(payload.get("reportedAt"), "reportedAt", 80, required=False),
        },
        sort_keys=True,
        separators=(",", ":"),
    )
    return {
        "messageId": hashlib.sha256(canonical.encode("utf-8")).hexdigest()[:24],
        "receivedAt": received_at,
        "reportedAt": bounded_text(payload.get("reportedAt"), "reportedAt", 80, required=False) or received_at,
        "senderAgent": sender,
        "category": category,
        "summary": summary,
        "pointers": pointers,
    }


class InboxStore:
    def __init__(self, path: Path):
        self.path = path
        self.lock = threading.Lock()

    def append(self, message: dict[str, Any]) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        line = json.dumps(message, sort_keys=True, separators=(",", ":"))
        with self.lock:
            with self.path.open("a", encoding="utf-8") as handle:
                handle.write(line + "\n")

    def list(self, limit: int = DEFAULT_LIMIT) -> list[dict[str, Any]]:
        if not self.path.exists():
            return []
        with self.lock:
            lines = self.path.read_text(encoding="utf-8").splitlines()
        messages: list[dict[str, Any]] = []
        for line in lines[-limit:]:
            try:
                parsed = json.loads(line)
            except json.JSONDecodeError:
                continue
            if isinstance(parsed, dict):
                messages.append(parsed)
        return messages


def render_dashboard(messages: list[dict[str, Any]]) -> str:
    rows = []
    for message in reversed(messages):
        pointers = "".join(
            "<li><code>{kind}</code> <a href=\"{uri}\">{label}</a></li>".format(
                kind=html.escape(str(pointer.get("kind", ""))),
                uri=html.escape(str(pointer.get("uri", "")), quote=True),
                label=html.escape(str(pointer.get("label", ""))),
            )
            for pointer in message.get("pointers", [])
            if isinstance(pointer, dict)
        )
        rows.append(
            """
            <article>
              <header><strong>{sender}</strong> · {category} · <time>{received}</time></header>
              <p>{summary}</p>
              <ul>{pointers}</ul>
            </article>
            """.format(
                sender=html.escape(str(message.get("senderAgent", ""))),
                category=html.escape(str(message.get("category", ""))),
                received=html.escape(str(message.get("receivedAt", ""))),
                summary=html.escape(str(message.get("summary", ""))),
                pointers=pointers,
            )
        )
    body = "\n".join(rows) or "<p>No operator attention messages.</p>"
    return f"""<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Operator Attention Inbox</title>
  <style>
    body {{ font-family: system-ui, sans-serif; margin: 2rem; line-height: 1.45; max-width: 960px; }}
    article {{ border-bottom: 1px solid #ddd; padding: 1rem 0; }}
    header {{ color: #333; }}
    code {{ background: #f3f3f3; padding: 0.1rem 0.25rem; }}
  </style>
</head>
<body>
  <h1>Operator Attention Inbox</h1>
  {body}
</body>
</html>
"""


class ListenerHandler(BaseHTTPRequestHandler):
    store: InboxStore

    def log_message(self, fmt: str, *args: Any) -> None:
        print(f"[operator-attention-listener] {self.address_string()} {fmt % args}", flush=True)

    def send_bytes(self, status: HTTPStatus, body: bytes, content_type: str) -> None:
        self.send_response(status.value)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def send_head_only(self, status: HTTPStatus, content_type: str = "text/plain; charset=utf-8") -> None:
        self.send_response(status.value)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def send_json(self, status: HTTPStatus, payload: dict[str, Any] | list[Any]) -> None:
        self.send_bytes(status, json.dumps(payload, indent=2, sort_keys=True).encode("utf-8"), "application/json; charset=utf-8")

    def do_GET(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path == "/health":
            self.send_bytes(HTTPStatus.OK, b"ok", "text/plain; charset=utf-8")
            return
        if parsed.path == "/messages":
            query = parse_qs(parsed.query)
            try:
                limit = max(1, min(500, int(query.get("limit", [str(DEFAULT_LIMIT)])[0])))
            except ValueError:
                limit = DEFAULT_LIMIT
            self.send_json(HTTPStatus.OK, {"messages": self.store.list(limit)})
            return
        if parsed.path in {"", "/"}:
            self.send_bytes(HTTPStatus.OK, render_dashboard(self.store.list()).encode("utf-8"), "text/html; charset=utf-8")
            return
        self.send_json(HTTPStatus.NOT_FOUND, {"error": "not found"})

    def do_HEAD(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path == "/health":
            self.send_head_only(HTTPStatus.OK)
            return
        if parsed.path == "/messages":
            self.send_head_only(HTTPStatus.OK, "application/json; charset=utf-8")
            return
        if parsed.path in {"", "/"}:
            self.send_head_only(HTTPStatus.OK, "text/html; charset=utf-8")
            return
        self.send_head_only(HTTPStatus.NOT_FOUND, "application/json; charset=utf-8")

    def do_POST(self) -> None:
        parsed = urlparse(self.path)
        if parsed.path not in {"/message", "/messages"}:
            self.send_json(HTTPStatus.NOT_FOUND, {"error": "not found"})
            return
        content_length = int(self.headers.get("Content-Length", "0"))
        if content_length <= 0 or content_length > MAX_BODY_BYTES:
            self.send_json(HTTPStatus.BAD_REQUEST, {"error": f"body must be 1..{MAX_BODY_BYTES} bytes"})
            return
        try:
            payload = json.loads(self.rfile.read(content_length).decode("utf-8"))
            if not isinstance(payload, dict):
                raise ValueError("body must be a JSON object")
            message = normalize_message(payload, parse_qs(parsed.query), {k.lower(): v for k, v in self.headers.items()})
            self.store.append(message)
        except (json.JSONDecodeError, UnicodeDecodeError, ValueError) as exc:
            self.send_json(HTTPStatus.BAD_REQUEST, {"error": str(exc)})
            return
        self.send_json(HTTPStatus.CREATED, {"accepted": True, "message": message})


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default=os.environ.get("OPERATOR_ATTENTION_LISTENER_HOST", "127.0.0.1"))
    parser.add_argument("--port", type=int, default=int(os.environ.get("OPERATOR_ATTENTION_LISTENER_PORT", "7779")))
    parser.add_argument(
        "--state",
        default=os.environ.get("OPERATOR_ATTENTION_LISTENER_STATE", "/tmp/operator-attention/messages.jsonl"),
    )
    args = parser.parse_args()
    ListenerHandler.store = InboxStore(Path(args.state))
    server = ThreadingHTTPServer((args.host, args.port), ListenerHandler)
    print(f"[operator-attention-listener] listening on http://{args.host}:{args.port} state={args.state}", flush=True)
    server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
