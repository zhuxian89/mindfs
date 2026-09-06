---
doc_type: issue-report
issue: 2026-09-06-relay-release-compatibility
status: resolved
severity: P2
tags: [cloud-relay, upstream-compatibility, e2ee, assets]
---

# Relay 发布兼容问题

## 1. 来源与授权

用户在发布/API 审核后明确说“开始修复吧”。本次按已建议的 Cloud 编码路径修复、发布资源覆盖检查和固定兼容验证推进。源码、官方 release、官方公开 HTTP 的证据见 [审核补充](../../audits/2026-09-06-relay-upstream-compatibility/release-followup.md)。

## 2. 已复现问题

1. 浏览器为 `/api/sessions/.../toolcalls/claude-task-list%3A1` 签名，Cloud 清空 RawPath，将 `%3A` 变成 `:`。官方 v0.5.0 Node 直连验签通过，经 Relay 返回 401 `e2ee_proof_invalid`。
2. Node 升级独立于 Cloud 发布。现有一次性 asset-sync 只在执行时查询新版本；缺新版 JS/CSS 时页面入口 200、资源 404。`readyz` 只验证当前镜像入口，无法发现尚未导入的新 release。

## 3. 期望

- HTTP 与 WS 转发保留 Node 路径原始转义、query、method、body 和 E2EE headers。
- 提供可重复、无需重启的同步 + 版本覆盖检查步骤；新版缺失、损坏、发布列表不可查询时明确失败。
- 正式发布包与源码 Node 均有永久加密路径/新增 API 回归；资源保留旧版本。
- 不修改上游客户端，不操作线上服务或安装调度任务，不重启/部署。本地服务域名仍暂停，剩余 Cookie 隔离不纳入。
