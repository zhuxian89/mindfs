---
doc_type: issue-fix
issue: 2026-08-03-relayed-header-value
path: fast-track
fix_date: 2026-08-03
tags: [mindfs, cloud, relay, compatibility, static-assets]
---

# Relayed Header 值不兼容修复记录

## 1. 问题描述

真实 Cloud Relay 与未修改 MindFS CLI 建立 Connector 后，从 Public Node Route 请求 release 版本 fixture index，响应仍包含 `./assets/`，没有按客户端既有契约重写为 `/mindfs-assets/`。

## 2. 根因

`cloud/internal/gateway/http.go` 为转发到 Node 的 HTTP 和 WebSocket 请求生成 `X-MindFS-Relayed: true`，而未修改 Node 的 `server/internal/api/http.go` 只在该 Header 严格等于 `1` 时把请求识别为 Relay 流量。双方值不一致使 release-mode 静态资源重写分支永远不执行。

## 3. 修复方案

Cloud Gateway 统一生成 `X-MindFS-Relayed: 1`，并更新 HTTP、WebSocket 单元测试的协议断言。客户端源码保持不变。

## 4. 改动文件清单

- `cloud/internal/gateway/http.go`：将 Cloud 生成的 relayed Header 值从 `true` 改为 `1`。
- `cloud/internal/gateway/http_test.go`：固定 HTTP 转发契约为 `1`。
- `cloud/internal/gateway/websocket_test.go`：固定 WebSocket 转发契约为 `1`。
- `cloud/compat/scenario_test.go`：真实未修改 CLI 的静态 index 黑盒回归断言。

## 5. 验证结果

- `go test ./internal/gateway ./compat` 通过。
- `MINDFS_RUN_COMPAT=1 go test ./compat -run TestUnmodifiedNodeRelayCompatibility -v` 通过。
- 真实 release-version Test Node 经 Public Route 返回的 fixture index 不再包含 `./assets/`，并包含 `/mindfs-assets/app.js`。
- HTTP 和 WebSocket Gateway 单元测试均确认伪造值被清洗且 Cloud 只生成 `1`。

## 6. 遗留事项

无。本次只修复已确认的 Header 值不兼容，没有修改 Node、增加生产路由或扩展协议。
