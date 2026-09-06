---
doc_type: audit-finding
audit: 2026-09-06-relay-backend-review
finding_id: performance-06
nature: performance
severity: P1
confidence: high
suggested_action: cs-issue
status: fixed
resolved_in: working-tree
---

# Finding 06：匿名绑定请求生成的记录永久累积

## 2026-09-06 处理进度

已在当前工作区完成源码修复和回归验证，尚未部署到生产。具体行为、测试和部署注意见 [修复记录](../../issues/2026-09-06-relay-backend-hardening/relay-backend-hardening-fix-note.md)。以下保留修复前的审计证据。

## 速答

匿名客户端每提交一个新的合法格式绑定码，数据库就新增一条 challenge。过期清理只把状态改成 expired，始终不删除记录；该入口也没有来源限流或总量上限，长期或恶意请求会不断增加持久化数据和清理扫描成本。

## 关键证据

- `cloud/app/binding_handlers.go:59` 的 poll 接口不要求 Cloud 登录：

  ```go
  response, err := a.binding.Poll(r.Context(), r.URL.Query().Get("code"), r.Header.Get("X-MindFS-Device-ID"))
  ```

- `cloud/internal/binding/service.go:72` 校验绑定码格式和非空 device ID，然后调用 ObserveChallenge；未设置速率或容量限制。
- `cloud/internal/store/sqlite_binding.go:27` 首次观察未知 code hash 时直接 INSERT：

  ```go
  if errors.Is(err, ErrNotFound) {
      // ...
      const insert = "INSERT INTO bind_challenges " +
          "(code_hash, device_id, status, token_derivation_version, expires_at, created_at) " +
          "VALUES (?, ?, ?, ?, ?, ?)"
  }
  ```

- `cloud/internal/store/sqlite_sessions.go:27` 对这张表只执行 UPDATE：

  ```go
  const query = "UPDATE bind_challenges SET status = ? " +
      "WHERE expires_at <= ? AND status IN (?, ?)"
  _, err := s.db.ExecContext(ctx, query, BindExpired, nowMillis, BindPending, BindConfirmed)
  ```

- `cloud/app/app.go:122` 起每分钟运行清理。`cloud/internal/store/schema.sql` 没有 bind_challenges 的 expires_at/status 索引，`cloud/internal/store/sqlite.go:33` 将 SQLite 连接数限制为 1，因此增长的扫描会与其他控制面查询争用该连接。

## 定向复现

`TestAuditAnonymousBindChallengesSurviveExpiry` 使用真实 App 和临时 SQLite：

1. 不携带 Cookie，以相同 device ID 请求 32 个不同的合法格式绑定码，全部返回 200。
2. 调用真实 DeleteExpired，把时间推进 365 天。
3. 32 条记录仍全部可以读取，只是状态变为 expired。

这是小样本生命周期验证，不是洪泛或生产容量测试。确定性结论是记录没有保留上限；达到磁盘耗尽或明显延迟所需的规模未测量。

## 影响与边界

少量正常绑定可能长期感觉不到影响。持续制造新 code 可永久增加数据库大小；已有记录每分钟都被无相应索引的清理语句扫描。已确认的 Device Token 保存在独立表中，不能因此无限保留所有匿名 pending/expired 请求。

## 修复方向与建议动作

`cs-issue`：在不破坏官方绑定轮询和 confirmed 重试契约的前提下，设置终态记录的有限保留策略、入口速率/容量限制，并按清理查询建立合适索引。
