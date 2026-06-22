#!/usr/bin/env python3
"""Minimal AgentKit sandbox tool server.

This server intentionally uses only Python's standard library so the sandbox
image can be built in offline/private environments. It exposes workspace-safe
file/search tools and light browser control. AgentKit ToolGateway should call
these endpoints; the model should only see the tool schemas, not this internal
server contract.
"""
from __future__ import annotations

import fnmatch
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time
import traceback
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Callable

WORKSPACE = Path(os.environ.get("AISPHERE_WORKSPACE", "/workspace")).resolve()
TOOL_PORT = int(os.environ.get("AISPHERE_TOOL_PORT", "18081"))
BROWSER_PORT = int(os.environ.get("AISPHERE_BROWSER_PORT", "9222"))
NETWORK_MODE = os.environ.get("AISPHERE_NETWORK_MODE", "offline")
ENABLE_SHELL = os.environ.get("AISPHERE_ENABLE_SHELL", "false").lower() == "true"
MAX_OUTPUT_BYTES = int(os.environ.get("AISPHERE_MAX_OUTPUT_BYTES", "1048576"))
DEFAULT_READ_LIMIT = int(os.environ.get("AISPHERE_DEFAULT_READ_LIMIT", "65536"))


def now_ms() -> int:
    return int(time.time() * 1000)


def json_response(handler: BaseHTTPRequestHandler, status: int, data: Any) -> None:
    body = json.dumps(data, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


def read_json(handler: BaseHTTPRequestHandler) -> dict[str, Any]:
    length = int(handler.headers.get("Content-Length", "0") or "0")
    if length <= 0:
        return {}
    body = handler.rfile.read(length)
    return json.loads(body.decode("utf-8"))


def safe_path(value: str | None, *, must_exist: bool = False) -> Path:
    raw = value or "."
    candidate = (WORKSPACE / raw.lstrip("/")).resolve()
    try:
        candidate.relative_to(WORKSPACE)
    except ValueError as exc:
        raise ValueError("path escapes workspace") from exc
    if must_exist and not candidate.exists():
        raise FileNotFoundError(str(candidate.relative_to(WORKSPACE)))
    return candidate


def rel(path: Path) -> str:
    return str(path.resolve().relative_to(WORKSPACE))


def truncate_text(data: bytes | str, limit: int = MAX_OUTPUT_BYTES) -> tuple[str, bool]:
    if isinstance(data, str):
        b = data.encode("utf-8", errors="replace")
    else:
        b = data
    truncated = len(b) > limit
    b = b[:limit]
    return b.decode("utf-8", errors="replace"), truncated


def file_info(p: Path) -> dict[str, Any]:
    st = p.stat()
    return {
        "path": rel(p),
        "type": "dir" if p.is_dir() else "file",
        "size": st.st_size,
        "mtime": int(st.st_mtime),
    }


def tool_workspace_list(inp: dict[str, Any]) -> dict[str, Any]:
    root = safe_path(str(inp.get("path", ".")), must_exist=True)
    recursive = bool(inp.get("recursive", False))
    max_entries = int(inp.get("maxEntries", 200))
    max_entries = max(1, min(max_entries, 5000))
    if root.is_file():
        return {"entries": [file_info(root)], "truncated": False}
    entries: list[dict[str, Any]] = []
    walker = root.rglob("*") if recursive else root.iterdir()
    for p in walker:
        if len(entries) >= max_entries:
            return {"entries": entries, "truncated": True}
        try:
            entries.append(file_info(p))
        except OSError:
            continue
    entries.sort(key=lambda x: (x["type"], x["path"]))
    return {"entries": entries, "truncated": False}


def tool_workspace_read(inp: dict[str, Any]) -> dict[str, Any]:
    p = safe_path(str(inp.get("path", "")), must_exist=True)
    if not p.is_file():
        raise ValueError("path is not a file")
    offset = max(0, int(inp.get("offset", 0) or 0))
    limit = int(inp.get("limit", DEFAULT_READ_LIMIT) or DEFAULT_READ_LIMIT)
    limit = max(1, min(limit, MAX_OUTPUT_BYTES))
    with p.open("rb") as f:
        f.seek(offset)
        data = f.read(limit + 1)
    text, truncated = truncate_text(data, limit)
    return {"path": rel(p), "offset": offset, "content": text, "truncated": truncated, "size": p.stat().st_size}


def tool_workspace_write(inp: dict[str, Any]) -> dict[str, Any]:
    p = safe_path(str(inp.get("path", "")))
    append = bool(inp.get("append", False))
    content = str(inp.get("content", ""))
    p.parent.mkdir(parents=True, exist_ok=True)
    mode = "a" if append else "w"
    with p.open(mode, encoding="utf-8", newline="") as f:
        f.write(content)
    return {"path": rel(p), "size": p.stat().st_size, "sha256": sha256_file(p)}


def tool_workspace_patch(inp: dict[str, Any]) -> dict[str, Any]:
    p = safe_path(str(inp.get("path", "")), must_exist=True)
    if not p.is_file():
        raise ValueError("path is not a file")
    old = str(inp.get("old", ""))
    new = str(inp.get("new", ""))
    count = int(inp.get("count", 1) or 1)
    if old == "":
        raise ValueError("old cannot be empty")
    data = p.read_text(encoding="utf-8", errors="replace")
    occurrences = data.count(old)
    if occurrences == 0:
        return {"path": rel(p), "changed": False, "occurrences": 0}
    updated = data.replace(old, new, count if count > 0 else -1)
    p.write_text(updated, encoding="utf-8")
    return {"path": rel(p), "changed": True, "occurrences": occurrences, "sha256": sha256_file(p)}


def tool_workspace_delete(inp: dict[str, Any]) -> dict[str, Any]:
    p = safe_path(str(inp.get("path", "")), must_exist=True)
    recursive = bool(inp.get("recursive", False))
    if p == WORKSPACE:
        raise ValueError("cannot delete workspace root")
    if p.is_dir():
        if not recursive:
            raise ValueError("recursive=true is required to delete directories")
        shutil.rmtree(p)
    else:
        p.unlink()
    return {"path": rel(p.parent), "deleted": True}


def tool_workspace_mkdir(inp: dict[str, Any]) -> dict[str, Any]:
    p = safe_path(str(inp.get("path", "")))
    p.mkdir(parents=True, exist_ok=True)
    return {"path": rel(p), "created": True}


def tool_workspace_search_files(inp: dict[str, Any]) -> dict[str, Any]:
    root = safe_path(str(inp.get("path", ".")), must_exist=True)
    query = str(inp.get("query", "")).lower()
    pattern = str(inp.get("glob", "*"))
    max_results = max(1, min(int(inp.get("maxResults", 200) or 200), 2000))
    results: list[dict[str, Any]] = []
    for p in root.rglob("*"):
        if len(results) >= max_results:
            return {"matches": results, "truncated": True}
        rp = rel(p)
        if query and query not in rp.lower():
            continue
        if pattern and not fnmatch.fnmatch(p.name, pattern) and not fnmatch.fnmatch(rp, pattern):
            continue
        try:
            results.append(file_info(p))
        except OSError:
            continue
    return {"matches": results, "truncated": False}


def tool_workspace_search_text(inp: dict[str, Any]) -> dict[str, Any]:
    root = safe_path(str(inp.get("path", ".")), must_exist=True)
    query = str(inp.get("query", ""))
    if not query:
        raise ValueError("query is required")
    pattern = str(inp.get("glob", ""))
    max_results = max(1, min(int(inp.get("maxResults", 100) or 100), 1000))
    if shutil.which("rg"):
        cmd = ["rg", "--line-number", "--no-heading", "--color", "never", "--max-count", str(max_results), query, str(root)]
        if pattern:
            cmd[1:1] = ["--glob", pattern]
        proc = subprocess.run(cmd, cwd=str(WORKSPACE), text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
        lines = proc.stdout.splitlines()
        matches = []
        for line in lines[:max_results]:
            parts = line.split(":", 2)
            if len(parts) == 3:
                f, no, text = parts
                try:
                    fp = Path(f).resolve().relative_to(WORKSPACE)
                    f = str(fp)
                except Exception:
                    pass
                matches.append({"path": f, "line": int(no) if no.isdigit() else None, "text": text})
        return {"matches": matches, "truncated": len(lines) > max_results}
    matches: list[dict[str, Any]] = []
    for p in root.rglob("*"):
        if len(matches) >= max_results:
            return {"matches": matches, "truncated": True}
        if not p.is_file():
            continue
        if pattern and not fnmatch.fnmatch(p.name, pattern) and not fnmatch.fnmatch(rel(p), pattern):
            continue
        try:
            for i, line in enumerate(p.read_text(encoding="utf-8", errors="ignore").splitlines(), 1):
                if query in line:
                    matches.append({"path": rel(p), "line": i, "text": line[:1000]})
                    if len(matches) >= max_results:
                        return {"matches": matches, "truncated": True}
        except OSError:
            continue
    return {"matches": matches, "truncated": False}


def tool_shell_exec(inp: dict[str, Any]) -> dict[str, Any]:
    if not ENABLE_SHELL:
        raise PermissionError("shell.exec is disabled; set AISPHERE_ENABLE_SHELL=true for trusted sandboxes")
    command = inp.get("command")
    if not command:
        raise ValueError("command is required")
    cwd = safe_path(str(inp.get("cwd", ".")), must_exist=True)
    timeout = max(1, min(int(inp.get("timeoutSeconds", 30) or 30), 300))
    started = now_ms()
    proc = subprocess.run(command, cwd=str(cwd), text=True, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    stdout, stdout_truncated = truncate_text(proc.stdout)
    stderr, stderr_truncated = truncate_text(proc.stderr)
    return {
        "exitCode": proc.returncode,
        "stdout": stdout,
        "stderr": stderr,
        "stdoutTruncated": stdout_truncated,
        "stderrTruncated": stderr_truncated,
        "durationMillis": now_ms() - started,
    }


def tool_browser_status(_: dict[str, Any]) -> dict[str, Any]:
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{BROWSER_PORT}/json/version", timeout=2) as resp:
            data = json.loads(resp.read().decode("utf-8"))
        return {"available": True, "networkMode": NETWORK_MODE, "version": data}
    except Exception as exc:
        return {"available": False, "networkMode": NETWORK_MODE, "error": str(exc)}


def tool_browser_open(inp: dict[str, Any]) -> dict[str, Any]:
    url = str(inp.get("url", "about:blank"))
    if not (url.startswith("http://") or url.startswith("https://") or url.startswith("about:")):
        raise ValueError("url must start with http://, https://, or about:")
    encoded = urllib.parse.quote(url, safe="")
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{BROWSER_PORT}/json/new?{encoded}", timeout=5) as resp:
            data = json.loads(resp.read().decode("utf-8"))
        return {"opened": True, "target": data}
    except urllib.error.HTTPError:
        req = urllib.request.Request(f"http://127.0.0.1:{BROWSER_PORT}/json/new?{encoded}", method="PUT")
        with urllib.request.urlopen(req, timeout=5) as resp:
            data = json.loads(resp.read().decode("utf-8"))
        return {"opened": True, "target": data}


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return "sha256:" + h.hexdigest()


TOOLS: dict[str, Callable[[dict[str, Any]], dict[str, Any]]] = {
    "workspace.list": tool_workspace_list,
    "workspace.read": tool_workspace_read,
    "workspace.write": tool_workspace_write,
    "workspace.patch": tool_workspace_patch,
    "workspace.delete": tool_workspace_delete,
    "workspace.mkdir": tool_workspace_mkdir,
    "workspace.search_files": tool_workspace_search_files,
    "workspace.search_text": tool_workspace_search_text,
    "shell.exec": tool_shell_exec,
    "browser.status": tool_browser_status,
    "browser.open": tool_browser_open,
}


def load_tool_schemas() -> list[dict[str, Any]]:
    candidates = [
        Path(os.environ.get("AISPHERE_DEFAULT_TOOLS", "/opt/aisphere/tools/default-tools.json")),
        Path("/etc/aisphere/sandbox/default-tools.json"),
    ]
    for p in candidates:
        if p.exists():
            try:
                return json.loads(p.read_text(encoding="utf-8"))
            except Exception:
                pass
    return [{"name": name, "description": "Sandbox tool", "inputSchema": {"type": "object"}} for name in sorted(TOOLS)]


class Handler(BaseHTTPRequestHandler):
    server_version = "AisphereSandboxToolServer/0.1"

    def log_message(self, fmt: str, *args: Any) -> None:
        sys.stderr.write("%s %s\n" % (self.log_date_time_string(), fmt % args))

    def do_GET(self) -> None:  # noqa: N802
        if self.path == "/healthz":
            return json_response(self, 200, {"ok": True, "workspace": str(WORKSPACE), "networkMode": NETWORK_MODE})
        if self.path == "/v1/tools":
            tools = load_tool_schemas()
            if not ENABLE_SHELL:
                tools = [t for t in tools if t.get("name") != "shell.exec"]
            return json_response(self, 200, {"tools": tools})
        if self.path == "/v1/skills":
            skill_dir = Path("/opt/aisphere/skills")
            items = []
            for p in skill_dir.glob("*/SKILL.md"):
                items.append({"name": p.parent.name, "path": str(p)})
            return json_response(self, 200, {"skills": items})
        return json_response(self, 404, {"error": "not_found"})

    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/tools/call":
            return json_response(self, 404, {"error": "not_found"})
        started = now_ms()
        try:
            body = read_json(self)
            name = str(body.get("tool") or body.get("name") or "")
            inp = body.get("input") or {}
            if name not in TOOLS:
                return json_response(self, 404, {"ok": False, "error": {"code": "TOOL_NOT_FOUND", "message": name}})
            result = TOOLS[name](inp)
            return json_response(self, 200, {"ok": True, "tool": name, "result": result, "durationMillis": now_ms() - started})
        except Exception as exc:  # intentionally returns structured tool failure
            return json_response(self, 500, {
                "ok": False,
                "error": {"code": exc.__class__.__name__, "message": str(exc)},
                "trace": traceback.format_exc(limit=5),
                "durationMillis": now_ms() - started,
            })


def main() -> None:
    WORKSPACE.mkdir(parents=True, exist_ok=True)
    os.chdir(str(WORKSPACE))
    server = ThreadingHTTPServer(("0.0.0.0", TOOL_PORT), Handler)
    print(json.dumps({"event": "sandbox_tool_server_start", "port": TOOL_PORT, "workspace": str(WORKSPACE), "networkMode": NETWORK_MODE}, ensure_ascii=False), flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
