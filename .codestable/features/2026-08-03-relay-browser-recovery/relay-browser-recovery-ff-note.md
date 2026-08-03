---
doc_type: feature-ff-note
feature: relay-browser-recovery
date: 2026-08-03
requirement: mindfs-compatible-cloud-backend
tags: [relay, browser, pwa, recovery, node-auth]
---

## 做了什么
补齐自建 Relay 在 `node_auth` 模式下供 MindFS PWA 断线恢复使用的浏览器路由，避免 WebSocket 短暂断开后跳入不存在的 `/login` 并显示 404。

## 改了哪些
- `cloud/app/app.go` — 注册根入口、`/login`、`/nodes` 和 `/api/auth/me`。
- `cloud/app/browser_handlers.go` — 实现 node_auth 探针、安全节点回跳和不公开服务端节点目录的本地恢复页。
- `cloud/app/browser_handlers_test.go` — 覆盖 PWA 探针、恢复入口及开放重定向防护。

## 怎么验证的
通过 `go test ./...`、关键包竞态测试、`go vet ./...` 和未修改 MindFS Node 的真实 Relay 兼容测试。浏览器视觉与实际 PWA 断线恢复由用户在部署后确认。

## 顺手发现
- 当前 `/nodes` 是 V0 本地恢复入口，不提供服务端节点管理；完整节点列表、账号和 ACL 仍属于后续 roadmap feature。
