# AI Sphere / AgentKit Session-Native Sandbox 设计文档

## 1. 设计目标

本设计把 AgentKit 的 session 从“外部 Runtime 调用远端沙箱工具”升级为“session 原生出生在沙箱内”。

核心目标：

- 新创建一个 session 后，Agent 的运行进程本身就在 Sandbox Pod 内。
- Agent 的工作目录天然是 `/workspace`。
- 文件、浏览器、shell、MCP 等工具在本地沙箱内执行，不通过 SSH 登录进去。
- Kubernetes 沙箱生命周期不重复造轮子，底层复用开源 `kubernetes-sigs/agent-sandbox` 的 CRD/Controller。
- 我们自己的 `aisphere-sandbox` 只做平台适配层：Auth、Quota、Profile、Lease、CRD Adapter。

一句话边界：

```text
Hub 管定义和授权，AgentKit 管 session/run，aisphere-sandbox 管平台适配和 lease，agent-sandbox 管 K8s 沙箱生命周期，Sandbox Pod 内运行 session worker 和 tools。
```

## 2. 组件边界

### 2.1 aisphere-auth

负责：

- 用户、组织、项目、角色。
- ResourceGrant 授权。
- 服务账号 / session token / introspect。

不负责：

- 不创建沙箱。
- 不理解 Pod/PVC/NetworkPolicy。

### 2.2 aisphere-hub

负责：

- Agent / Skill / Tool / MCP Registry。
- 版本、label、status、分享。
- Agent resolve，返回运行快照。
- Tool failure、审计、catalog event。
- SandboxProfile 的定义与授权。

不负责：

- 不直接创建 Pod / PVC / Service。
- 不持有高权限 K8s ServiceAccount。
- 不承接高频 tool 数据面调用。

### 2.3 AgentKit Runtime / Control Plane

负责：

- Session / Run 元数据。
- 前端消息入口和事件流转发。
- 调 Hub resolve agent snapshot。
- 调 aisphere-sandbox ensure sandbox。
- 把用户消息转发给 Sandbox Pod 内的 session worker。

不负责：

- 不直接在主进程执行文件、浏览器、shell 工具。
- 不 SSH 到沙箱。
- 不直接操作底层 Sandbox CRD 细节。

### 2.4 aisphere-sandbox Adapter

负责：

- 对外提供稳定的 Sandbox API。
- 复用 aisphere-auth 做认证和 ResourceGrant。
- 校验组织/项目是否允许使用某个 sandbox profile。
- 校验配额：并发沙箱数、CPU、内存、PVC 容量、是否允许联网/shell/browser。
- 创建 `agent-sandbox` 的 `Sandbox` 或 `SandboxClaim` CR。
- 返回 SandboxLease：sandboxId、endpoints、leaseToken、expiresAt、capabilities。
- 提供状态、日志、调试工具代理。

不负责：

- 不实现自己的 Pod/PVC/Service controller。
- 不替代 `agent-sandbox` controller。
- 不承载生产高频 tool call，只保留调试代理。

### 2.5 kubernetes-sigs/agent-sandbox

负责：

- Sandbox CRD。
- SandboxTemplate。
- SandboxClaim。
- SandboxWarmPool。
- Controller 创建和维护 Pod/PVC/Service/NetworkPolicy。
- Ready 条件和状态回写。

不负责：

- 不理解 AI Sphere 的用户/组织/Agent/Tool 权限。
- 不管理 Hub 定义。
- 不管理模型调用。

### 2.6 Sandbox Pod

负责：

- 运行 `agentkit-session-worker`。
- 运行 `aisphere-tool-server`。
- 提供 `/workspace` PVC。
- 提供 workspace/browser/shell/mcp tools。

第一版可以是单容器多进程：

```text
agentkit-sandbox container
  ├── agentkit-session-worker :8088
  ├── aisphere-tool-server    :18081
  └── chromium optional       :9222
```

后续生产版可以拆成多容器 Pod：

```text
worker container
sidecar tools container
sidecar browser container
sidecar mcp container
```

## 3. Session 出生在沙箱里的流程

### 3.1 创建 session

```text
Frontend
  -> AgentKit API: POST /api/sessions
  -> Hub resolve agent snapshot
  -> aisphere-sandbox ensure sandbox
  -> agent-sandbox controller 创建/分配 Sandbox Pod
  -> Pod 内 agentkit-session-worker ready
  -> session active
```

### 3.2 用户发消息

```text
Frontend
  -> AgentKit API
  -> session-router 查到 worker endpoint
  -> POST http://sandbox-worker:8088/v1/session/messages
  -> worker 在 /workspace 内运行 Agent loop
  -> worker 调 Model Gateway
  -> worker 本地调用 127.0.0.1:18081 tools
  -> worker 返回 events
  -> AgentKit API 转发给前端
```

### 3.3 不是 SSH

不使用 SSH，不开 sshd，不分发 SSH key。

通信方式是平台内部 HTTP/gRPC/WebSocket：

```text
AgentKit Control Plane <-> agentkit-session-worker
agentkit-session-worker <-> local tool-server
agentkit-session-worker <-> model-gateway
```

## 4. SandboxLease

`aisphere-sandbox` 返回给 AgentKit 的不是底层 CRD，而是 Lease：

```json
{
  "sandboxId": "sb-org-a-proj-a-sess-001",
  "phase": "Running",
  "lease": {
    "token": "lease_xxx",
    "expiresAt": "2026-06-22T12:00:00Z"
  },
  "endpoints": [
    {"name": "worker", "url": "http://xxx:8088", "port": 8088},
    {"name": "tools", "url": "http://xxx:18081", "port": 18081},
    {"name": "browser", "url": "http://xxx:9222", "port": 9222}
  ],
  "profile": "default-python-offline",
  "sessionId": "sess-001",
  "agentId": "ops-agent",
  "snapshotId": "agent_snap_xxx"
}
```

AgentKit 只关心 Lease，不关心底层是 Sandbox CR、SandboxClaim、WarmPool，还是未来多集群调度。

## 5. SandboxProfile

Profile 是平台层概念，建议由 Hub 管理，Adapter 执行。

示例：

```json
{
  "id": "default-python-offline",
  "templateRef": "aisphere-agent-session",
  "networkMode": "offline",
  "allowShell": false,
  "allowBrowser": false,
  "workspaceSize": "10Gi",
  "cpu": "500m",
  "memory": "1Gi"
}
```

AgentDefinition 只引用 profile：

```json
{
  "sandbox": {
    "profile": "default-python-offline",
    "reuse": "session"
  }
}
```

## 6. agent-sandbox 集成策略

### 第一阶段：Sandbox CR 直连

`aisphere-sandbox` 创建：

```text
agents.x-k8s.io/v1beta1 Sandbox
```

优点：

- 改造快。
- 不依赖 WarmPool。
- 可以立即把 Pod/PVC/Service 生命周期交给开源 controller。

### 第二阶段：SandboxTemplate

把 PodSpec、镜像、资源、网络策略、volumeClaimTemplates 收敛成模板：

```text
extensions.agents.x-k8s.io/v1beta1 SandboxTemplate
```

### 第三阶段：SandboxClaim + WarmPool

创建：

```text
extensions.agents.x-k8s.io/v1beta1 SandboxClaim
```

从：

```text
SandboxWarmPool
```

领取预热沙箱。

适合 browser/code interpreter 等冷启动慢的 session。

## 7. 模型上下文注入原则

给模型：

- 当前工作目录 `/workspace`。
- 可用 tool name、description、inputSchema。
- 简短 skill 指南。
- 是否允许联网、浏览器、shell。

不给模型：

- Pod 名称。
- PVC 名称。
- Service DNS。
- K8s namespace。
- ServiceAccount token。
- Hub token。
- Model API key。
- CRD 细节。

模型感知的是“我在一个隔离环境里，有本地文件系统和工具”，而不是“Kubernetes 资源细节”。

## 8. 当前代码改造状态

本版本已经开始做：

- `aisphere-sandbox` module 名称调整为 `github.com/actionlab-ai/aisphere-sandbox`。
- 默认 driver 改为 `agent-sandbox`。
- 新增 `AgentSandboxManager`，通过 Kubernetes API 创建 `Sandbox` CR。
- 保留 `direct-kubernetes` driver 作为 fallback。
- `SandboxEnsureRequest` 新增 `profile/templateRef/warmPoolRef`。
- `SandboxStatus` 新增 `profile/templateRef/warmPoolRef`。
- 沙箱镜像新增 `agentkit-session-worker.py`。
- entrypoint 支持 `AISPHERE_SESSION_WORKER_ENABLED=true` 时同时启动 tool-server 和 session-worker。
- 设计上预留 worker endpoint `:8088`。

## 9. 下一步代码任务

### AgentKit Runtime

新增：

```text
internal/sandboxclient
internal/sessionworkerclient
internal/sessionrouter
internal/modelgateway
```

改造：

```text
CreateSession:
  Hub Resolve -> Sandbox Ensure -> Wait Worker Ready -> Save Lease

SendMessage:
  Load Lease -> Forward to Worker -> Stream Events
```

### Hub

新增或完善：

```text
SandboxProfile Registry
AgentDefinition.sandbox.profile
Org/Project sandbox policy
```

### Sandbox Adapter

完善：

```text
Quota
Profile resolve from Hub
Lease token verification
SandboxClaim + WarmPool driver
```

### Sandbox Image

完善：

```text
真实 agentkit-session-worker
接 Model Gateway
接真实 AgentKit runner
接 MCP sidecar
```
