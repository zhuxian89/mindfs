---
doc_type: requirement
slug: mindfs-compatible-cloud-backend
pitch: 不用修改 MindFS 客户端，也能把本地节点接入自己的云端远程访问
status: current
last_reviewed: 2026-08-05
implemented_by: [cloud-relay-core, cloud-email-accounts]
tags: [mindfs, cloud, relay, self-hosted, compatibility, qq, multi-user]
---

# 为 MindFS 提供兼容的自建云后端

## 用户故事

- 作为 MindFS 使用者，我希望把本地节点接入自己的云服务，从公网继续使用现有 MindFS，而不是依赖官方云平台。
- 作为维护者，我希望持续更新上游 MindFS 源码，而不用反复处理自建云功能对客户端代码造成的冲突。
- 作为自托管服务的运营者，我希望先部署最核心的远程访问能力，之后再逐步增加账号、配额和云端扩展功能。
- 作为自托管用户，我希望注册一次后直接使用 QQ 邮箱和自己设置的 Relay 密码登录，不必每次接收验证码。
- 作为多个用户中的一员，我希望只能看到、重命名和删除自己绑定的节点，其他用户无法通过猜测 node ID 越权管理。
- 作为 V0 升级用户，我希望原有节点在升级后不丢失，并可由指定 bootstrap QQ 邮箱完成注册后认领。

## 为什么需要

MindFS 客户端已经具备远程连接能力，但官方云端实现不在当前源码仓库中。只部署现有源码可以使用本地模式，却无法完整拥有和控制公网远程访问服务。

## 怎么解决

提供一个独立运行的兼容云后端。用户只需把 MindFS 指向自己的服务地址，现有客户端就能完成绑定并通过云端访问本地节点。Cloud 提供仅 `@qq.com` 的验证码注册、邮箱密码登录、密码恢复和 owner-scoped 节点管理；这些能力复用客户端已有 `/login`、auth、bind 和 nodes 契约，不要求修改客户端。

## 边界

- 不修改 MindFS 的现有客户端、本地服务、Web 前端和移动端。
- 不改变 MindFS 的 Agent、会话、文件、Git、任务或端到端加密能力。
- 不保证复制官方平台未公开的内部行为，只保证现有客户端可观察协议兼容。
- 云后端的实现、依赖和构建必须与上游 MindFS 代码隔离。
- 使用前需要为 MindFS 配置自建云地址，并提供可用的 HTTPS/WSS 服务。
- 账号只允许规范化后域名严格等于 `qq.com` 的邮箱；不支持用户名、其他邮箱、OAuth/OIDC、tenant、RBAC、邀请或节点共享。
- QQ SMTP 授权码只保存在服务端 Git ignored `.env`，不进入数据库、日志或 HTTP 响应。

## 变更日志

- 2026-08-03：首个单实例 Relay 核心闭环验收通过，能力从 draft 升级为 current。
- 2026-08-03：V0 部署基线验收通过，增加 Web assets 托管、readiness、metrics、迁移、在线 SQLite 备份和 Docker/Caddy 部署能力。
- 2026-08-04：补齐服务端节点发现闭环。bootstrap 管理员可在任意浏览器经客户端既有 Relay 控制台契约（/login、/nodes、GET/PATCH/DELETE /api/nodes、/api/auth/me、/api/auth/logout）发现、打开、重命名和删除服务端节点；换浏览器或清站点数据后不再需要手记 /n/{nodeId}/，单用户 V0 形成端到端可用闭环。
- 2026-08-04：Relay 持久化托管全部官方 release 的 web/assets（v0.1.8 起，逐文件校验 size + SHA-256，content-hash 文件只增不删），修复多版本客户端打开节点时主资源 404、被强制提示重新安装最新版的问题。
- 2026-08-05：完成仅 `@qq.com` 的验证码注册、Argon2id Relay 密码登录、忘记/修改密码、UserSession 和节点 owner 隔离；V0 节点由 bootstrap QQ 邮箱注册后认领，Gateway/Connector/E2EE 数据面保持不变。
