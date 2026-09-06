---
doc_type: issue-analysis
issue: 2026-09-06-relay-performance-hardening
status: confirmed
tags: [relay, performance, memory, sqlite, websocket]
---

# 实施范围与验证约束

按用户要求推进已定位问题的优化。实现选择以保持协议和数据正确性为前提，源码优化不包含生产部署。

1. `internal/ops/metrics.go` 与测试：有限标准方法标签，其他方法统一 OTHER；只改变监控分类，不改变转发方法。覆盖异常方法持续增长及并发渲染。
2. `internal/store/sqlite.go`、`sqlite_connections.go`、`sqlite_nodes.go`、`schema.sql` 与新增数据库测试/基准：WAL + FULL 同步；所有写事务仍用单连接，独立最多 4 条只读连接服务 GetNode/ListNodesByOwner。不缓存节点、不降低 last_seen 写入精度、不迁移身份校验事务。补 session 过期索引。覆盖未提交写期间的读取、提交/删除后可见、只读池写保护、重开及现有备份/改密竞态测试。部署说明记录 WAL 文件与备份要求。
3. `internal/gateway/websocket.go`、`ws_frames.go`、`ws_message.go` 与测试/基准：节点到公网方向按消息大小处理。≤1 MiB 使用 4/32/256/1024 KiB 分档池，读完整后一次写出；更大消息读取长度并验证后，以 32 KiB 缓冲流式写一个 WebSocket 消息。截断或写失败不能发送完成帧。空消息、分档边界、最大消息、连续消息、文本/二进制、关闭码、非法长度/opcode、慢接收与断连均需覆盖。浏览器到节点需要先知道完整消息长度，继续保留整消息缓冲与原 32 MiB 限制，不增加协议格式或擅自降低上限。

附带更新本 issue 三份记录、性能审计处理状态和现状架构。已有 Cookie 防护及用户决定保留的同源边界不变；本地服务域名功能继续暂停。

验证按模块逐步进行，最终执行全包 race/vet 与真实未修改 Node HTTP/WS/E2EE/重连兼容测试。微基准在同一本机以 GOMAXPROCS=4 比较，不等同于 4 核 6 GB 生产机满负载压测。指标规范化是缺陷修复，其余属于内部访问/转发优化；不存在需要人工目视的前端改动。
