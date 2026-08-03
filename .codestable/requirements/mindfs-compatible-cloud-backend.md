---
doc_type: requirement
slug: mindfs-compatible-cloud-backend
pitch: 不用修改 MindFS 客户端，也能把本地节点接入自己的云端远程访问
status: current
last_reviewed: 2026-08-03
implemented_by: [cloud-relay-core]
tags: [mindfs, cloud, relay, self-hosted, compatibility]
---

# 为 MindFS 提供兼容的自建云后端

## 用户故事

- 作为 MindFS 使用者，我希望把本地节点接入自己的云服务，从公网继续使用现有 MindFS，而不是依赖官方云平台。
- 作为维护者，我希望持续更新上游 MindFS 源码，而不用反复处理自建云功能对客户端代码造成的冲突。
- 作为自托管服务的运营者，我希望先部署最核心的远程访问能力，之后再逐步增加账号、配额和云端扩展功能。

## 为什么需要

MindFS 客户端已经具备远程连接能力，但官方云端实现不在当前源码仓库中。只部署现有源码可以使用本地模式，却无法完整拥有和控制公网远程访问服务。

## 怎么解决

提供一个独立运行的兼容云后端。用户只需把 MindFS 指向自己的服务地址，现有客户端就能完成绑定并通过云端访问本地节点。云后端跟随客户端已有行为保持兼容，而不是要求客户端为它做适配。

## 边界

- 不修改 MindFS 的现有客户端、本地服务、Web 前端和移动端。
- 不改变 MindFS 的 Agent、会话、文件、Git、任务或端到端加密能力。
- 不保证复制官方平台未公开的内部行为，只保证现有客户端可观察协议兼容。
- 云后端的实现、依赖和构建必须与上游 MindFS 代码隔离。
- 使用前需要为 MindFS 配置自建云地址，并提供可用的 HTTPS/WSS 服务。

## 变更日志

- 2026-08-03：首个单实例 Relay 核心闭环验收通过，能力从 draft 升级为 current。
- 2026-08-03：V0 部署基线验收通过，增加 Web assets 托管、readiness、metrics、迁移、在线 SQLite 备份和 Docker/Caddy 部署能力。
