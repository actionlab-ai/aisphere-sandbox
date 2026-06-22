# Sandbox Workspace Tools Skill

Use this skill when an agent needs to inspect, edit, search, or generate files inside the session sandbox. The sandbox workspace is always rooted at `/workspace`; never assume access to host paths.

## Rules

1. Start with `workspace.list` or `workspace.search_files` when the target path is unknown.
2. Use `workspace.read` before editing an existing file.
3. Prefer `workspace.patch` for small targeted edits, because it preserves unrelated content.
4. Use `workspace.write` for new files or full rewrites.
5. Use `workspace.search_text` for keyword, symbol, config, or error-message lookup.
6. Never ask for host paths. Paths are workspace-relative.
7. Treat `shell.exec` as optional and restricted. Use it only when the sandbox policy enables it and when file/search tools are insufficient.
8. Browser tools run inside the sandbox browser; network access depends on the sandbox network mode.

## Typical flow

- Explore: `workspace.list({"path":".","recursive":false})`
- Locate files: `workspace.search_files({"query":"router","glob":"*.go"})`
- Locate text: `workspace.search_text({"query":"TODO","glob":"*.go"})`
- Read: `workspace.read({"path":"backend/main.go"})`
- Patch: `workspace.patch({"path":"backend/main.go","old":"old text","new":"new text"})`
- Create: `workspace.write({"path":"docs/notes.md","content":"..."})`

## What to expose to the model

Expose concise tool schemas and this skill guidance. Do not expose Kubernetes Pod specs, PVC names, service account tokens, network policy internals, or secret references in the model context.
