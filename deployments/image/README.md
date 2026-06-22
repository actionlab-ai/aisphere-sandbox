# AgentKit Sandbox Image

This image is the default execution environment for AgentKit session sandboxes.
It contains common development tools, a small built-in HTTP tool server, optional
headless Chromium, and a workspace at `/workspace` mounted from PVC.

## Build

```bash
docker build -t registry.local/aisphere/agentkit-sandbox:latest \
  aisphere-hub/deployments/sandbox/image
```

For an offline site, mirror the base image and apt repositories first, then pass:

```bash
docker build \
  --build-arg BASE_IMAGE=sealos.hub:5000/devcontainers/base:debian-12 \
  -t sealos.hub:5000/aisphere/agentkit-sandbox:latest \
  aisphere-hub/deployments/sandbox/image
```

## Runtime endpoints

- `GET /healthz`
- `GET /v1/tools`
- `POST /v1/tools/call`
- `GET /v1/skills`

## Built-in tools

File/search tools are always workspace-scoped:

- `workspace.list`
- `workspace.read`
- `workspace.write`
- `workspace.patch`
- `workspace.search_files`
- `workspace.search_text`
- `workspace.mkdir`
- `workspace.delete`

Browser tools are available when Chromium is installed and started:

- `browser.status`
- `browser.open`

`shell.exec` is installed but disabled by default. Enable it only for trusted
sandboxes with `AISPHERE_ENABLE_SHELL=true`.

## Model-facing tool context

The sandbox exposes raw platform tool schemas at `GET /v1/tools`. AgentKit
ToolGateway should convert these into model-facing function definitions and keep
execution details out of the model context.

Pass to the model:

- tool name or normalized function name
- description
- input schema
- compact usage guidance from `skills/*/SKILL.md`

Do not pass to the model:

- Pod DNS / Kubernetes Service URL
- PVC name / mount source implementation
- ServiceAccount token or secret refs
- NetworkPolicy details

For APIs that do not accept dots in function names, map `workspace.read` to a
safe name such as `workspace__read`, then map it back inside ToolGateway before
calling `/v1/tools/call`.

## Hub proxy endpoints

During development, the Hub can proxy sandbox calls:

```text
GET  /v3/aihub/runtime/sandboxes/:sandboxId/tools
POST /v3/aihub/runtime/sandboxes/:sandboxId/tools/call
```

Production AgentKit Runtime can also call the sandbox ClusterIP Service directly
when it runs inside the same cluster.
