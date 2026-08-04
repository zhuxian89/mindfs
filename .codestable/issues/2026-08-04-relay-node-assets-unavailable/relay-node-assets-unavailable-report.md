---
doc_type: issue-report
issue: 2026-08-04-relay-node-assets-unavailable
status: confirmed
severity: P1
summary: 通过 Relay 打开已绑定节点时主资源不可用，页面强制提示重新安装最新版 MindFS
tags:
  - cloud-relay
  - asset-compatibility
  - node-web-ui
---

# Relay Node Assets Unavailable Issue Report

## 1. 问题现象

管理员登录 `https://relay.20260310.best/nodes` 后点击一个已绑定节点，或直接访问该节点的 Relay URL，浏览器不显示节点 Web UI，而是强制弹出“版本太老，请重新安装最新版 mindfs”。关闭提示后仍无法进入节点，也看不到原本位于页面左下角的版本升级入口。

## 2. 复现步骤

1. 登录生产环境 Relay 管理页面 `https://relay.20260310.best/nodes`。
2. 点击节点 `nvyslm4tgeasawkyz5gmc6cm5le`，或直接打开 `https://relay.20260310.best/n/nvyslm4tgeasawkyz5gmc6cm5le/`。
3. 观察到浏览器弹出“版本太老，请重新安装最新版 mindfs”，节点 Web UI 未加载。

复现频率：稳定复现。

## 3. 期望 vs 实际

**期望行为**：通过 Relay 打开任意受支持版本的已绑定节点时，应正常加载节点 Web UI；如果节点确有新版本，也应在 UI 加载后继续使用客户端原有的升级入口。

**实际行为**：节点 Web UI 在启动前中断，只显示强制重新安装提示，原有升级入口不可访问。

## 4. 环境信息

- 涉及模块 / 功能：生产 Cloud Relay 的节点页面转发与共享静态资源服务。
- 相关文件 / 函数：待阶段 2 根因分析确认。
- 运行环境：生产环境 `relay.20260310.best`，Cloudflare 边缘代理，Docker Compose 部署。
- 节点信息：页面所用资源与官方最新 `v0.4.6` release 一致。
- 现场响应：节点 HTML 返回 200；其引用的 `index-DeNebQ9q.js` 返回 404；CSS 返回 200；`/readyz` 返回 200。
- 实现边界：不得修改 `web/`、`server/`、`cli/`、Android、Harmony 或其他客户端代码；修复只能在闭源兼容层 `cloud/**` 内完成，并保持现有客户端接口与路径完全兼容。

## 5. 严重程度

**P1** — Relay 节点 Web UI 完全不可用，影响核心远程访问流程；仍可通过 VPS 运维或节点机器 CLI 绕过，因此未定为 P0。

## 备注

截图显示的是浏览器原生 alert，文案为“版本太老，请重新安装最新版 mindfs”。用户要求彻底解决当前故障及同类跨版本资源兼容问题，而不是只临时补一个文件。
