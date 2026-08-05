---
doc_type: issue-fix
issue: 2026-08-05-binding-node-offline-race
path: fast-track
fix_date: 2026-08-05
tags: [mindfs, cloud, relay, binding, connector, race-condition]
---

# 绑定后首次打开节点短暂离线修复记录

## 1. 问题描述

Mac 本地 MindFS 完成自建 Cloud Relay 绑定后，绑定页立即提供节点链接。用户马上打开该链接时，Cloud 返回 `node_offline`；刷新页面后节点可以正常访问。

## 2. 根因

`cloud/internal/binding.Service.Confirm` 在创建节点和 Device Token 后立即返回 `node_url`。本地 MindFS 仍需轮询到 `confirmed`、保存凭据并建立 Connector WebSocket。原绑定页收到确认响应后立刻展示节点链接，没有等待 `/api/nodes` 报告目标节点已经 `online`，因此用户可能在 Connector 注册到 Cloud Session Registry 前打开节点。

## 3. 修复方案

Cloud 绑定页确认成功后保持节点链接隐藏，每秒读取当前账号的 `/api/nodes`：

- 目标节点为 `offline` 时显示连接等待状态，不提供节点直链。
- 目标节点为 `online` 后才显示节点链接，并保留原有 `root` 查询参数。
- 等待超过 15 秒时显示“节点仍在连接”，但继续轮询，不把用户导向必然返回 `node_offline` 的地址。
- 认证失效时仍按原有安全路径返回登录页。

该修复只调整 Cloud 提供的绑定页面，不修改上游 MindFS 客户端。

## 4. 改动文件清单

- `cloud/app/binding_handlers.go`：增加 Connector 在线等待和节点链接门控。
- `cloud/app/app_test.go`：增加绑定页在线门控契约回归测试。
- `.codestable/issues/2026-08-05-binding-node-offline-race/binding-node-offline-race-fix-note.md`：记录修复闭环。

## 5. 验证结果

- `go test -count=1 ./...`（`cloud/`）通过。
- `go vet ./...`（`cloud/`）通过。
- 内嵌绑定页 JavaScript 通过 `node --check`。
- Playwright 真实浏览器验证：模拟目标节点离线时，页面显示等待提示且 `Open node` 链接保持隐藏。
- Playwright 真实浏览器验证：模拟目标节点变为在线后，页面显示 `Binding confirmed. Node is online.`，并展示带 `?root=java_project` 的节点链接。
- `git diff --check` 通过。

## 6. 遗留事项

代码修复需要部署新的 Cloud Relay 镜像后才会在 `relay.20260310.best` 生效。已有节点、Device Token、账号数据和客户端配置不需要迁移或重新绑定。
