# Sandbox Manager 架构说明

## 为什么从 Hub 拆出来

Hub 是定义控制面，负责 Agent/Skill/Tool/MCP 的注册、版本、授权、审计；Sandbox Manager 是执行基础设施控制面，负责 K8s Pod/PVC/Service/NetworkPolicy/日志/Lease。拆分后 Hub 不再持有高风险 K8s 权限，也不会被高频 tool 数据面拖垮。

## 推荐调用链

```text
AgentKit Runtime
  -> Hub resolve agent snapshot
  -> Sandbox Manager ensure sandbox
  -> sandbox Pod tool-server /v1/tools
  -> Model 只接收 tool schema + skill 使用说明
  -> tool_call -> AgentKit ToolGateway -> sandbox Pod /v1/tools/call
  -> failure -> Hub
```

## 组件边界

### Sandbox Manager

- 创建 Namespace，可选。
- 创建 PVC：`aisb-ws-<sandboxId>`。
- 创建 ConfigMap：`aisb-<sandboxId>`，包含 `tool-manifest.json` 和 `sandbox-request.json`。
- 创建 Service：`aisb-<sandboxId>`，暴露 tools/browser/web 端口。
- 创建 NetworkPolicy：按 offline/restricted/online 控制 egress。
- 创建 Pod：运行安全 sandbox 镜像。
- 查询 Pod 状态和日志。

### Sandbox Pod

- 内置 `/workspace` 文件工具。
- 内置 browser 工具。
- 可选 shell 工具。
- 后续可挂载 MCP stdio、二进制工具、额外技能包。

### Hub

- 只保存 Tool/Service 定义与执行策略。
- 不执行具体 tool。
- 不直接管理 Pod 生命周期。
- 可以保留 Sandboxes 控制台页面，通过 Sandbox Manager API 查看状态。

### Auth

- 平台身份认证仍在 `aisphere-auth`。
- Sandbox Manager 通过 service token 调 `/auth/sessions/introspect` 和 `/iam/resource-grants/check`。
- Sandbox Manager 不理解 Agent/Skill/Tool 分享规则，只判断 sandbox 资源访问和租户额度。

## 网络模式

- `offline`：默认无 egress。
- `restricted`：允许 DNS + 指定 CIDR。
- `online`：删除 per-pod deny NetworkPolicy，依赖集群默认策略。

NetworkPolicy 是否生效取决于 CNI，Calico/Cilium 需要实测。

## 后续建议

1. 增加数据库或 CRD 记录 sandbox 生命周期。
2. 增加 org/project 级资源配额。
3. 增加 idle GC 控制器。
4. 增加多集群调度。
5. 增加正式 Lease 校验，确保 Runtime 只能调用自己的 sandbox。
6. AgentKit Runtime 直接接 Sandbox Manager，不再通过 Hub 代理创建沙箱。
