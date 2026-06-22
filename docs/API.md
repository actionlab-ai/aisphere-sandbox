# Sandbox Manager API

## Ensure Sandbox

`POST /v1/sandboxes/ensure`

```json
{
  "runtimeId": "agentkit-runtime-01",
  "sessionId": "sess-001",
  "runId": "run-001",
  "orgId": "org-a",
  "projectId": "project-a",
  "agentId": "ops-agent",
  "snapshotId": "agent_snap_xxx",
  "network": { "mode": "offline" },
  "limits": { "cpu": "500m", "memory": "1Gi" },
  "services": [],
  "toolMounts": []
}
```

## List Sandboxes

`GET /v1/sandboxes?orgId=org-a&projectId=project-a&sessionId=sess-001`

## Get Sandbox

`GET /v1/sandboxes/{sandboxId}`

## Restart Sandbox

`POST /v1/sandboxes/{sandboxId}/restart`

删除 Pod，保留 PVC，并按 ConfigMap 中的 `sandbox-request.json` 重建。

## Delete Sandbox

`DELETE /v1/sandboxes/{sandboxId}?deleteWorkspace=false`

## Logs

`GET /v1/sandboxes/{sandboxId}/logs?tail=200`

## Tools Debug Proxy

`GET /v1/sandboxes/{sandboxId}/tools`

`POST /v1/sandboxes/{sandboxId}/tools/call`

```json
{
  "tool": "workspace.read",
  "input": { "path": "README.md" }
}
```
