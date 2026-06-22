# aisphere-sandbox

`aisphere-sandbox` 是 AI Sphere / AgentKit 的沙箱适配层。它不再重复实现 Kubernetes Pod/PVC/Service 控制器，而是复用开源 `kubernetes-sigs/agent-sandbox` 作为底层 CRD/Controller。

本服务定位：

```text
AgentKit Runtime / Hub
  -> aisphere-sandbox API Adapter
  -> kubernetes-sigs/agent-sandbox CRD
  -> Sandbox Pod
  -> agentkit-session-worker + aisphere-tool-server
```

## 关键边界

- `agent-sandbox`：K8s 原生沙箱生命周期，负责 Sandbox/SandboxTemplate/SandboxClaim/SandboxWarmPool、Pod/PVC/Service。
- `aisphere-sandbox`：平台适配层，负责 Auth、Quota、Profile、Lease、CRD Adapter、状态/日志/调试工具代理。
- `AgentKit Runtime`：负责 session/run、模型调用网关、消息转发。
- `Sandbox Pod`：session 原生运行环境，运行 `agentkit-session-worker` 和 `aisphere-tool-server`。

## 当前实现

- 默认 driver：`agent-sandbox`。
- fallback driver：`direct-kubernetes`。
- `POST /v1/sandboxes/ensure` 创建 `agents.x-k8s.io/v1beta1 Sandbox`。
- 可选 `useClaim=true` + `defaultWarmPool`，走 `extensions.agents.x-k8s.io/v1beta1 SandboxClaim`。
- 沙箱镜像支持 `AISPHERE_SESSION_WORKER_ENABLED=true`，使 session worker 原生运行在 `/workspace` 中。

## API

```text
GET    /healthz
GET    /readyz
GET    /v1/sandboxes
POST   /v1/sandboxes/ensure
GET    /v1/sandboxes/{sandboxId}
POST   /v1/sandboxes/{sandboxId}/restart
DELETE /v1/sandboxes/{sandboxId}?deleteWorkspace=false
GET    /v1/sandboxes/{sandboxId}/logs?tail=200
GET    /v1/sandboxes/{sandboxId}/tools
POST   /v1/sandboxes/{sandboxId}/tools/call
```

## 本地编译

```bash
go test ./...
go build -o bin/aisphere-sandbox ./cmd/sandbox-manager
```

## 配置

复制配置：

```bash
cp configs/config.json.example config.json
```

默认 driver：

```json
{
  "sandbox": {
    "driver": "agent-sandbox",
    "agentSandboxApiVersion": "v1beta1",
    "useClaim": false,
    "defaultTemplate": "aisphere-agent-session",
    "defaultProfile": "default-python-offline"
  }
}
```

## 创建沙箱

```bash
curl -s http://127.0.0.1:18082/v1/sandboxes/ensure \
  -H 'Content-Type: application/json' \
  -d '{
    "sessionId": "sess-001",
    "orgId": "org-a",
    "projectId": "project-a",
    "agentId": "demo-agent",
    "snapshotId": "agent_snap_xxx",
    "profile": "default-python-offline"
  }'
```

返回的 lease 会包含：

```text
worker endpoint :8088
tools endpoint  :18081
browser endpoint:9222
```

## 文档

- [Session Native Sandbox 设计](docs/SESSION_NATIVE_SANDBOX_DESIGN.md)
- [API](docs/API.md)
- [架构](docs/ARCHITECTURE.md)

## 部署示例

- `deployments/agent-sandbox/sandbox-direct-session.yaml`：直接创建 Sandbox CR。
- `deployments/agent-sandbox/sandboxtemplate-session-worker.yaml`：Session worker 模板。
- `deployments/agent-sandbox/sandboxwarmpool-session-worker.yaml`：预热池。
- `deployments/agent-sandbox/sandboxclaim-session-worker.yaml`：从 WarmPool 领取 sandbox。
