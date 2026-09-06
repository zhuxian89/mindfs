---
doc_type: audit-finding
audit: 2026-09-06-relay-performance
finding_id: performance-02
nature: performance
severity: P2
confidence: medium
suggested_action: cs-refactor
status: improved
resolved_in: working-tree
---

# Finding 02：会话鉴权逐次写盘，与节点查询争用唯一连接

## 2026-09-06 处理结果

已启用 WAL + FULL，写事务仍串行，GetNode/ListNodesByOwner 使用独立最多 4 条只读连接；增加 session expires_at 索引。相同持续鉴权写入场景的节点查询中位数从 187.9 µs 降至 13.5 µs；提交/删除可见、并发、取消等待、只读保护、重开和备份/改密回归通过。会话仍逐次写入，验证码 OR 清理扫描未改变，因此标为 improved。详见 [修复记录](../../issues/2026-09-06-relay-performance-hardening/relay-performance-hardening-fix-note.md)。以下为优化前证据。

## 速答

管理接口即使只是读取节点列表，也会更新 session 的 last_seen 时间。该写入和每次 Relay 请求开始时的节点查询共用唯一数据库连接，高频管理/绑定流量会增加普通转发的启动等待。

## 关键证据

- `cloud/internal/store/sqlite.go:33`：`db.SetMaxOpenConns(1)`；OpenSQLite 未显式切换 WAL 或同步级别。临时新库实测 `journal_mode=delete`、`synchronous=2`（FULL）。生产数据库可能有历史设置，本次未读取。
- `cloud/internal/store/sqlite_identity.go:381`：每次有效鉴权都会执行：

  ```go
  s.db.ExecContext(ctx,
      "UPDATE user_sessions SET last_seen_at = ? WHERE session_hash = ?",
      toMillis(now), sessionHash)
  ```

- `cloud/app/relay_nodes_handlers.go:27`：列表接口先 `a.authenticateUser(r)`；`cloud/app/relay_browser_handlers.go:77` 的节点页每 15 秒刷新一次列表，会反复经过这一写入路径。
- `cloud/internal/gateway/http.go:100`：HTTP/WS 开流前调用 `h.store.GetNode(ctx, nodeID)`，同样需要取得该连接。进入 body 转发后不持有数据库连接，因此这里主要影响请求启动，不表示整个文件下载都占用数据库。
- `cloud/internal/store/sqlite_identity.go:11`：绑定来源限流等也通过数据库事务读写计数，与上述流量共享连接。

## 本地测量

Apple M2 Pro、临时文件数据库、GOMAXPROCS=4，每个场景 500 次，重复三轮：

| 场景 | 平均耗时范围 | 三轮中位数 |
|---|---|---|
| 单独 GetNode | 14.1–22.8 µs/op | 17.6 µs/op |
| 单独 GetUserBySession | 338–376 µs/op | 344.7 µs/op |
| GetNode，同时一个 goroutine 持续鉴权写入 | 187.5–196.2 µs/op | 187.9 µs/op |

后一个场景中数据库等待计数增加，确认两个流程存在连接竞争。这里没有 Argon2 密码计算，测量的是已有会话鉴权；后台写入持续运行，比少量用户每 15 秒刷新更密集。

结果说明竞争机制和量级，不能推算线上每秒请求上限。即使在合成竞争场景下，本机查询仍为亚毫秒级，故定 P2，线上影响置信度为 medium。

## 清理方面的相关证据

`cloud/internal/store/sqlite_sessions.go:10` 删除过期 session，`:15` 删除过期或已消费验证码。真实驱动的 EXPLAIN QUERY PLAN 分别返回 `SCAN user_sessions` 和 `SCAN email_verification_codes`。前者没有 expires_at 索引；后者即使有 expires_at 索引，当前 OR 条件仍选用扫描。

这会随表规模增大而增加共享连接的占用时间，但本次未测大表清理耗时，不另报成已发生的长尾故障。已经修复并限额的 bind_challenges 回收不重复计入。

## 修复方向与建议动作

建议 `cs-refactor`：先以真实负载的连接等待时间验证优先级，评估读写访问方式及清理查询。不要直接扩大连接池而破坏现有事务一致性；如要降低 last_seen 持久化频率，需明确允许的时间精度与持久化语义变化。没有证据要求更换数据库。
