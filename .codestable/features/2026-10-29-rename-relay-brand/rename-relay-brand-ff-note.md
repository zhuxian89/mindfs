---
doc_type: feature-ff-note
feature: rename-relay-brand
date: 2026-10-29
requirement: mindfs-compatible-cloud-backend
tags: [relay, branding]
---

## 做了什么
将独立云端 Relay 页面品牌从 MindFS Relay 改为“随风 relay”。保留指向 MindFS 客户端的操作提示和协议标识。

## 改了哪些
- `cloud/app/pages/shared.html` — 公共品牌及无障碍名称。
- `cloud/app/pages/login.html`、`bind.html`、`nodes.html` — 页面标题、登录模式切换标题、节点示意图和页脚品牌。
- `cloud/app/pages/DESIGN.md` — 同步设计文档名称。

## 怎么验证的
`go test ./app -run 'Test(Browser|BindPage|LoginPage)' -count=1`（在 `cloud/` 下执行）通过，`git diff --check` 通过。用户随后要求提交并推送；尚未验证云端部署效果。
