# AI Sphere Sandbox Manager

`aisphere-sandbox` 是 AI Sphere / AgentKit 的独立沙箱管理服务。它从 Hub 中拆出，专门负责 K8s Sandbox 生命周期、PVC、网络隔离、日志和调试工具代理。Hub 继续负责 Agent / Skill / Tool / MCP 的定义与授权，AgentKit Runtime 负责运行和 ToolGateway 调度。

## 模块边界

```text
Hub：Agent / Skill / Tool / MCP 定义、版本、授权、审计、Failure
Sandbox Manager：K8s Sandbox 生命周期、PVC、网络、资源、日志、Lease
AgentKit Runtime：Session / Run / LLM / ToolGateway，调用 Sandbox Manager 创建沙箱
Sandbox Pod：执行 workspace/browser/shell/mcp tools
```

## 主要能力

- 通过 Kubernetes API 创建/查询/重启/删除 Sandbox Pod。
- 为每个 sandbox 创建 `/workspace` PVC，Pod 重启不丢数据。
- 通过 ConfigMap 注入 `tool-manifest.json`，支持启动时挂载额外 tools。
- 支持 `offline / restricted / online` 三种网络模式，生成 NetworkPolicy。
- 支持读取 Pod 日志。
- 支持调试代理：列出 sandbox 内 tools、调用 sandbox tool-server。
- 支持对接 `aisphere-auth`：session introspect + ResourceGrant check。
- 支持 GitHub Actions 构建多架构镜像和多架构离线 `.run` 安装包。

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
go build -o bin/aisphere-sandbox-manager ./cmd/sandbox-manager
```

## 构建镜像

默认构建 `linux/amd64,linux/arm64`：

```bash
REGISTRY=ghcr.io/actionlab-ai VERSION=dev PUSH=true scripts/build-images.sh
```

本地单架构调试：

```bash
PLATFORMS=linux/amd64 LOAD=true REGISTRY=registry.local/aisphere VERSION=dev scripts/build-images.sh
```

镜像：

```text
ghcr.io/actionlab-ai/aisphere-sandbox-manager:<version>
ghcr.io/actionlab-ai/agentkit-sandbox:<version>
```

## 构建离线 .run 包

```bash
VERSION=v0.1.0 ARCHES="amd64 arm64" scripts/build-offline-run.sh
```

生成：

```text
dist/aisphere-sandbox-v0.1.0-amd64.run
dist/aisphere-sandbox-v0.1.0-arm64.run
```

离线环境安装示例：

```bash
chmod +x aisphere-sandbox-v0.1.0-amd64.run
./aisphere-sandbox-v0.1.0-amd64.run install \
  --registry sealos.hub:5000/aisphere \
  --namespace aisphere-sandbox-system \
  --sandbox-namespace aisphere-sandbox \
  --storage-class nfs-client \
  --auth-endpoint http://aisphere-auth.aisphere-system.svc:18080 \
  --apply
```

只解包：

```bash
./aisphere-sandbox-v0.1.0-amd64.run unpack ./offline
```

只加载镜像：

```bash
./aisphere-sandbox-v0.1.0-amd64.run load-images --registry sealos.hub:5000/aisphere
```

只渲染 YAML：

```bash
./aisphere-sandbox-v0.1.0-amd64.run render --registry sealos.hub:5000/aisphere
```

## K8s 部署

```bash
kubectl apply -f deployments/kubernetes/manager-rbac.yaml
kubectl apply -f deployments/kubernetes/sandbox-rbac.yaml
kubectl apply -f deployments/kubernetes/deployment.yaml
```

或先渲染再部署：

```bash
REGISTRY=registry.local/aisphere VERSION=v0.1.0 scripts/render-manifests.sh
kubectl apply -f dist/rendered/namespace.yaml
kubectl apply -f dist/rendered/manager-rbac.yaml
kubectl apply -f dist/rendered/sandbox-rbac.yaml
kubectl apply -f dist/rendered/deployment.yaml
```

## 创建 sandbox 示例

```bash
curl -s http://127.0.0.1:18082/v1/sandboxes/ensure \
  -H 'Content-Type: application/json' \
  -d '{
    "runtimeId": "agentkit-runtime-01",
    "sessionId": "sess-001",
    "orgId": "org-a",
    "projectId": "project-a",
    "agentId": "ops-agent",
    "snapshotId": "agent_snap_xxx",
    "workspaceSize": "10Gi",
    "network": {"mode":"offline"},
    "limits": {"cpu":"500m", "memory":"1Gi"}
  }'
```

## Auth 对接

默认 `auth.enabled=false`，适合开发环境。

生产推荐：

```json
{
  "auth": {
    "enabled": true,
    "mode": "aisphere",
    "authEndpoint": "http://aisphere-auth.aisphere-system.svc:18080",
    "sessionIntrospectPath": "/auth/sessions/introspect",
    "iamCheckPath": "/iam/resource-grants/check",
    "serviceToken": "CHANGE_ME",
    "failClosed": true,
    "app": "aihub"
  }
}
```

资源对象：

```text
aihub:sandbox:<sandboxId>
```

动作：

```text
read    查看列表、详情、日志
run     ensure/restart/tools call
delete  删除沙箱
```

## GitHub Actions

- `.github/workflows/ci.yml`：Go test + build。
- `.github/workflows/images.yml`：构建并推送多架构镜像到 GHCR。
- `.github/workflows/offline-run.yml`：构建 amd64/arm64 离线 `.run` 包，tag 触发时发布到 GitHub Release。

## 注意

- 当前 Kubernetes client 是轻量 REST client，支持 in-cluster ServiceAccount 或 `apiServer/token/caFile`，暂不解析 kubeconfig。
- Tool 调用代理用于调试和控制台，不建议生产高频工具调用都经过 Sandbox Manager；生产推荐 AgentKit Runtime 直接调用 sandbox ToolServer。
- NetworkPolicy 是否生效取决于 CNI，Calico/Cilium 一般支持，生产环境需要实测。
