#!/usr/bin/env python3
"""Minimal AgentKit session worker runtime for sandbox-native sessions.

This is intentionally small. It proves the architecture where a session is born
inside the sandbox. Production AgentKit will replace the placeholder message
handler with the real Agent loop while keeping the same HTTP/event contract.
"""
from __future__ import annotations

import json
import os
import queue
import sys
import urllib.error
import urllib.request
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

WORKSPACE = Path(os.environ.get("AISPHERE_WORKSPACE", "/workspace")).resolve()
WORKER_PORT = int(os.environ.get("AISPHERE_WORKER_PORT", "8088"))
SESSION_ID = os.environ.get("AISPHERE_SESSION_ID", "")
AGENT_ID = os.environ.get("AISPHERE_AGENT_ID", "")
SNAPSHOT_ID = os.environ.get("AISPHERE_SNAPSHOT_ID", "")
TOOL_SERVER = os.environ.get("AISPHERE_TOOL_SERVER", "http://127.0.0.1:18081")
MODEL_BASE_URL = os.environ.get("AISPHERE_MODEL_BASE_URL", "http://aisphere-gateway:18083/v1").rstrip("/")
MODEL_TOKEN = os.environ.get("AISPHERE_MODEL_TOKEN", "")
MODEL_PROFILE = os.environ.get("AISPHERE_MODEL_PROFILE", "deepseek-v4-agent")
EVENT_LOG = WORKSPACE / ".aisphere" / "session-events.jsonl"

EVENTS: "queue.Queue[dict[str, Any]]" = queue.Queue(maxsize=2048)


def emit(event: dict[str, Any]) -> None:
    event.setdefault("ts", time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
    try:
        EVENTS.put_nowait(event)
    except queue.Full:
        pass
    EVENT_LOG.parent.mkdir(parents=True, exist_ok=True)
    with EVENT_LOG.open("a", encoding="utf-8") as f:
        f.write(json.dumps(event, ensure_ascii=False) + "\n")


def read_json(handler: BaseHTTPRequestHandler) -> dict[str, Any]:
    n = int(handler.headers.get("Content-Length") or 0)
    if n <= 0:
        return {}
    return json.loads(handler.rfile.read(n).decode("utf-8"))


def write_json(handler: BaseHTTPRequestHandler, status: int, body: dict[str, Any]) -> None:
    data = json.dumps(body, ensure_ascii=False).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Content-Length", str(len(data)))
    handler.end_headers()
    handler.wfile.write(data)


class Handler(BaseHTTPRequestHandler):
    server_version = "AgentKitSessionWorker/0.1"

    def log_message(self, fmt: str, *args: Any) -> None:
        sys.stderr.write("%s %s\n" % (self.log_date_time_string(), fmt % args))

    def do_GET(self) -> None:  # noqa: N802
        if self.path in ("/healthz", "/readyz"):
            return write_json(self, 200, {"ok": True, "sessionId": SESSION_ID, "agentId": AGENT_ID, "workspace": str(WORKSPACE), "toolServer": TOOL_SERVER})
        if self.path == "/v1/session":
            return write_json(self, 200, {"sessionId": SESSION_ID, "agentId": AGENT_ID, "snapshotId": SNAPSHOT_ID, "workspace": str(WORKSPACE), "toolServer": TOOL_SERVER})
        if self.path.startswith("/v1/events"):
            # Simple long-poll endpoint. The production worker can upgrade this to SSE.
            items: list[dict[str, Any]] = []
            deadline = time.time() + 2
            while time.time() < deadline and len(items) < 50:
                try:
                    items.append(EVENTS.get(timeout=0.2))
                except queue.Empty:
                    if items:
                        break
            return write_json(self, 200, {"items": items})
        return write_json(self, 404, {"error": "not_found"})

    def do_POST(self) -> None:  # noqa: N802
        if self.path not in ("/v1/messages", "/v1/session/messages"):
            return write_json(self, 404, {"error": "not_found"})
        body = read_json(self)
        run_id = body.get("runId") or "run_" + uuid.uuid4().hex[:16]
        text = str(body.get("message") or body.get("content") or "")
        emit({"type": "user_message", "runId": run_id, "content": text})
        try:
            reply, usage = call_model_gateway(run_id, text)
            emit({"type": "model_usage", "runId": run_id, **usage})
        except Exception as exc:  # keep worker alive; surface error as event
            reply = f"模型网关调用失败：{exc}. 当前 session 仍在沙箱 {WORKSPACE} 内运行，tool server: {TOOL_SERVER}."
            emit({"type": "error", "runId": run_id, "message": str(exc), "source": "model_gateway"})
        emit({"type": "assistant_message", "runId": run_id, "content": reply})
        return write_json(self, 200, {"ok": True, "runId": run_id, "accepted": True})


def call_model_gateway(run_id: str, text: str) -> tuple[str, dict[str, Any]]:
    payload = {
        "model": MODEL_PROFILE,
        "messages": [
            {"role": "system", "content": f"你运行在 AI Sphere Sandbox 内。当前工作目录是 {WORKSPACE}。所有文件操作都应限定在 /workspace。"},
            {"role": "user", "content": text},
        ],
        "stream": False,
    }
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(MODEL_BASE_URL + "/chat/completions", data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    if MODEL_TOKEN:
        req.add_header("Authorization", "Bearer " + MODEL_TOKEN)
    req.add_header("X-AISphere-Session-ID", SESSION_ID)
    req.add_header("X-AISphere-Run-ID", run_id)
    req.add_header("X-AISphere-Agent-ID", AGENT_ID)
    with urllib.request.urlopen(req, timeout=600) as resp:
        body = resp.read().decode("utf-8")
    root = json.loads(body)
    content = ""
    choices = root.get("choices") or []
    if choices:
        msg = choices[0].get("message") or {}
        content = str(msg.get("content") or "")
    usage = root.get("usage") or {}
    return content, {
        "model": root.get("model") or MODEL_PROFILE,
        "promptTokens": int(usage.get("prompt_tokens") or 0),
        "completionTokens": int(usage.get("completion_tokens") or 0),
        "totalTokens": int(usage.get("total_tokens") or 0),
    }


def main() -> None:
    WORKSPACE.mkdir(parents=True, exist_ok=True)
    os.chdir(WORKSPACE)
    emit({"type": "worker_start", "sessionId": SESSION_ID, "agentId": AGENT_ID, "snapshotId": SNAPSHOT_ID, "workspace": str(WORKSPACE)})
    server = ThreadingHTTPServer(("0.0.0.0", WORKER_PORT), Handler)
    print(json.dumps({"event": "session_worker_start", "port": WORKER_PORT, "sessionId": SESSION_ID, "workspace": str(WORKSPACE)}, ensure_ascii=False), flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
