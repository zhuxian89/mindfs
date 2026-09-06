---
doc_type: audit-finding
audit: 2026-09-06-relay-backend-review
finding_id: security-03
nature: security
severity: P1
confidence: high
suggested_action: cs-issue
status: fixed
resolved_in: working-tree
---

# Finding 03：密码重置后，并发旧密码登录仍能创建有效会话

## 2026-09-06 处理进度

已在当前工作区完成源码修复和回归验证，尚未部署到生产。具体行为、测试和部署注意见 [修复记录](../../issues/2026-09-06-relay-backend-hardening/relay-backend-hardening-fix-note.md)。以下保留修复前的审计证据。

## 速答

Login 校验密码和创建会话是两个独立步骤。如果密码重置发生在两步之间，重置虽然删除了已有会话，较早开始的旧密码登录仍会在重置后创建新的有效会话。

## 关键证据

- `cloud/internal/identity/service.go:131` 读取用户快照，`:136` 校验快照中的密码，`:151` 按邮箱创建会话：

  ```go
  user, lookupErr := s.store.GetUserByEmail(ctx, normalized)
  // ...
  matched, verifyErr := s.passwords.Verify(encoded, password)
  // ...
  user, err = s.store.CreateUserSession(ctx, normalized, session, now)
  ```

- `cloud/internal/store/sqlite_identity.go:242` 在事务中重新读取用户，但只检查 active；没有比较刚才验证过的 password hash 或密码版本：

  ```go
  user, err := getUserByEmail(ctx, tx, email)
  if err != nil || user.Status != "active" { /* ... */ }
  session.UserID = user.ID
  if err := insertUserSession(ctx, tx, session); err != nil { /* ... */ }
  ```

- `cloud/internal/store/sqlite_identity.go:274` 修改密码，`:289` 删除当时已经存在的会话；无法撤销随后才插入的会话。
- `cloud/internal/store/sqlite_identity.go:338` 起的 `GetUserBySession` 校验用户状态和会话有效期，不检查创建会话所依据的密码版本。

## 定向复现

`TestAuditPasswordResetAllowsInflightOldPasswordLogin` 用测试 hasher 包装真实密码校验，以 channel 精确控制业务时序：

1. 使用旧密码注册用户，取得已有会话；申请合成重置验证码。
2. 启动旧密码 Login，在密码校验完成后暂停。
3. 完成真实 ResetPassword 事务，确认之前的会话已经失效。
4. 恢复 Login，让它继续调用真实 CreateUserSession。
5. Login 成功，新生成的会话通过 Authenticate。

测试全部在临时 SQLite 中运行。Go 竞态检测通过不能排除此问题，因为这是跨事务的业务时序错误，不是无锁内存访问。

## 影响与边界

需要知道旧密码的一方恰好有一个与重置交错的登录请求。它不使重置后的普通旧密码登录成功，也不能让不知道密码的人凭空登录；但会破坏“修改泄露密码后清除旧凭据访问权”的预期。正常主动改密也使用相同的会话创建/撤销机制，应一起核验。

## 修复方向与建议动作

`cs-issue`：会话创建事务应原子确认被验证的密码 hash/凭据版本仍与数据库一致；改变密码后，依赖旧版本的在途操作必须失败。避免在持有 SQLite 写事务时执行昂贵 Argon2 运算。
