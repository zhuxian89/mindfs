# MindFS 架构总入口

> 状态：current
> 创建日期：2026-08-02
> 最近核对：2026-08-03

## 1. 项目简介

MindFS 仓库包含现有本地 Node、Web、CLI 和移动端上游代码，并在独立 `cloud/` Go module 中提供兼容后端。Cloud 侧依赖现有客户端协议，现有源码保持只读。

## 2. 核心概念 / 术语表

- **MindFS Node**：本地运行并主动连接 Relay 的现有 `server` 进程。
- **Cloud Relay**：负责绑定、Connector 和公网 HTTP/WS 反向转发的独立后端。
- **Relay Session**：Cloud Relay 为在线 Node 持有的 `yamux.Server` session。

## 3. 子系统 / 模块索引

- [Cloud Relay 核心架构](cloud-relay-core.md) — 单实例 Binding、Connector、yamux、HTTP/WS Gateway、SQLite 和内存 Session Registry。

## 4. 关键架构决定

- Cloud 能力只写入独立 `cloud/**`，不修改现有 MindFS 上游代码。
- Cloud Relay 兼容未修改客户端，而不是要求客户端适配新后端。

## 5. 已知约束 / 硬边界

- `server/`、`web/`、`cli/`、`android/`、`harmony/` 和根构建文件视为上游只读。
- 当前 Cloud Relay 是 SQLite + 内存 session 的单实例实现。
