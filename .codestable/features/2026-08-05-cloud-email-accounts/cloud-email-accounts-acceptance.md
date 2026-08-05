---
doc_type: feature-acceptance
feature: 2026-08-05-cloud-email-accounts
status: passed
accepted: 2026-08-05
round: 1
---

# Cloud Email Accounts 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-08-05
> 关联方案：`.codestable/features/2026-08-05-cloud-email-accounts/cloud-email-accounts-design.md`
> 实现：commit `8ceb0dd`，已部署 `https://relay.20260310.best`
> 范围说明：`relay-local-service-domains` 按用户决定继续暂停，不属于本 feature 或本次验收。

## 1. 接口契约核对

对照方案第 2.1 节逐项核查：

- [x] `User`：`store.User` 保存唯一 QQ 邮箱、Argon2id PHC hash、pending/active/disabled 状态和密码/登录时间；邮箱同时作为登录账号和展示名。
- [x] `EmailVerificationCode`：`store.VerificationCode` 保存 purpose、随机 nonce、HMAC code hash、source hash、有效期、冷却、剩余尝试和消费时间，不保存明文 code。
- [x] `UserSession`：SQLite 只保存 Session hash、user ID、12 小时有效期与最近使用时间；Cookie 保存随机 Token。
- [x] `Node`：新增 `OwnerUserID`；Gateway 保留全局 `GetNode`，管理面使用 `List/Rename/DeleteNodeByOwner`。
- [x] `POST /api/auth/register/request-code`：返回 `resend_after_seconds`；非 QQ 邮箱 403，已注册 409，冷却/限流 429，发信故障 503。
- [x] `POST /api/auth/register`：接受 `email/password/code`，成功原子激活 User、消费 register code、创建 Session 并设置 Cookie。
- [x] `POST /api/auth/login`：只接受 `email/password`；成功创建 Session，错误密码和不存在/非 active 用户统一 `invalid_credentials`。
- [x] password request-code/reset/change：覆盖忘记密码和登录后修改密码，reset 撤销全部旧 Session，change 轮换当前 Session。
- [x] 配置：QQ SMTP、bootstrap email 和 Identity Key 均有实际落点；真实授权码只由 Git ignored `.env` 注入。
- [x] 流程图：RegisterPage→SMTP→Register、Login→Session、Reset→revoke sessions、Bind→Owner、Nodes→owner-scoped Store 均可从路由、Identity Service 和 Store grep 到实际调用链。

接口与名词层无未处理偏差。

## 2. 行为与决策核对

- [x] 用户只在注册和忘记密码时接收验证码；日常登录只使用 QQ 邮箱和 Relay 密码。
- [x] 不存在注册用户名、QQ 邮箱密码字段、验证码日常登录、OAuth/OIDC、tenant、RBAC、邀请或节点共享。
- [x] Relay 密码按 8-128 个 Unicode 字符校验，不 trim，不强制字符组合，只保存 Argon2id PHC hash。
- [x] register/password_reset code 按 purpose 隔离；6 位、10 分钟、60 秒冷却、5 次尝试、单次并发消费。
- [x] 验证码与登录按邮箱和来源持久化限流；未知 reset 邮箱保存不发信的伪记录，使冷却响应不泄露账号存在性。
- [x] Session Token 使用 32 字节随机数、12 小时 TTL、HttpOnly、SameSite=Lax、Path=/；HTTPS 部署设置 Secure。
- [x] safe-next 只接受 `/nodes`、`/bind?...` 和 `/n/{id}/...` 同源路径。
- [x] V0 ownerless 节点幂等迁移给 pending bootstrap User；该邮箱注册时激活原 User ID，因此保留 Node owner。
- [x] 绑定确认兼容官方 `{code,name}` 和 V0 `{code,action,node_name}`，并记录 current user。
- [x] 节点越权 list 不可见，rename/delete 统一 `node_not_found`；失败删除不会撤销 Token 或关闭 Session。
- [x] 旧 `/api/cloud/v1/auth/login` 线上与测试均为 404；AdminAuth/AdminSession 运行时代码已删除。
- [x] Gateway、Connector、HTTP/WS/E2EE 不读取 UserSession，owner 只约束控制面。

**挂载点反向核对**：

- [x] Auth 路由/页面：集中在 `app.go`、`identity_handlers.go`、`relay_browser_handlers.go`。
- [x] SMTP/Identity：集中在 `internal/identity/`、`internal/config/` 和 Compose 环境注入。
- [x] SQLite：identity 表、rate limit、owner 列和 V0 迁移集中在 `schema.sql`、`sqlite*.go`。
- [x] Binding：current user 在 `binding_handlers.go` 传入 Binding Service，Store 事务写 claimant/owner。
- [x] Nodes API：list/rename/delete 均调用 owner-scoped Store 契约。
- [x] grep 未发现清单外功能挂载；未修改 `server/`、`web/`、`cli/`、移动端或根构建文件。
- [x] 拔除沙盘：移除上述五类挂载后，原 Binding poll、Connector、Gateway、E2EE、assets 和 ops 数据面仍保持 V0 结构；无隐藏依赖。

## 3. 验收场景核对

- [x] **S1** 合法 QQ 邮箱收到 6 位注册码，DB/HTTP/log 无明文 code。证据：Identity 单测、schema/code review；用户确认线上注册成功。
- [x] **S2** 非 `@qq.com`、`vip.qq.com`、畸形地址发信前拒绝。证据：Identity/App 单测；线上 `acceptance@gmail.com` 返回 403 `email_not_allowed`。
- [x] **S3** 正确 email/password/register code 创建 active User + Session，后续密码登录无需验证码。证据：HTTP 集成测试；用户确认线上注册正常。
- [x] **S4** 密码边界、错误/过期/消费 code 不错误创建账号。证据：Identity Service 与 Store 事务测试。
- [x] **S5** active 重复注册返回 `email_taken`；pending bootstrap 激活原 ID。证据：Service/ownership migration 测试。
- [x] **S6** 正确密码创建 Session；错误密码、未注册、pending、disabled 统一失败。证据：Identity 登录测试与 dummy Argon2 路径 review。
- [x] **S7** DB 只保存 Argon2id PHC hash，无明文 Relay 密码。证据：SQLite 二进制内容测试、secret grep。
- [x] **S8** register/reset code 不可互用；5 次错误、过期、消费与并发重放符合约束。证据：purpose/replay/attempt/concurrent tests。
- [x] **S9** 冷却和小时限流跨重启持久；SMTP 失败删除 code。证据：Store reopen 与 mail-failure tests。
- [x] **S10** 未注册 QQ reset 请求不发邮件且与真实账号冷却响应一致；active 用户收到 reset code。证据：unknown reset/decoy cooldown tests。
- [x] **S11** reset 更新密码、撤销全部旧 Session；旧密码失败、新密码成功。证据：`TestPasswordResetRevokesSessions` 和 HTTP 集成测试。
- [x] **S12** 登录后 change 验证当前密码、撤销其他 Session、轮换当前 Session。证据：Service 与 HTTP same-origin/rotation tests。
- [x] **S13** `/api/auth/me` 只接受 UserSession；旧 AdminSession、过期/撤销会话 401。证据：浏览器 handler tests；线上无 Cookie 返回 401。
- [x] **S14** logout 服务端撤销成功后才清 Cookie，旧 Cookie 不可复用；撤销失败不假装成功。证据：logout success/failure tests。
- [x] **S15** 登录页包含登录、注册、忘记密码，不含范围外入口。证据：页面测试；Playwright headed 桌面和 390x844 移动端实测无溢出/遮挡。
- [x] **S16** 用户 A/B 分别绑定节点，只看到自己的节点；两种绑定请求体都写 owner。证据：`TestRelayNodeOwnershipIsolationAndBindingPayloads`。
- [x] **S17** B 修改/删除 A node 返回 404，A 的节点、Token、online Session 不变；owner 删除保持撤销语义。证据：App/Store owner isolation tests。
- [x] **S18** V0 DB 升级创建 pending bootstrap User，注册后原节点完整可见且 Connector Token 可用。证据：SQLite V0 migration 与 App bootstrap registration tests。
- [x] **S19** Gateway、Connector、HTTP/WS/E2EE、assets、backup、health/metrics 无回归。证据：`go test ./...`、race tests、真实未修改 CLI compat 全通过。
- [x] **S20** SMTP 授权码由 VPS `.env` 注入且 Git ignored；tracked files/SQLite/log/HTTP 无真实授权码。证据：`git check-ignore`、secret grep、部署后真实注册成功。

**前端浏览器验证**：

- [x] `https://relay.20260310.best/login` 桌面注册模式布局正常，邮箱/密码/确认密码/验证码/发送按钮完整。
- [x] 390x844 移动端忘记密码模式布局正常，按钮和输入框无重叠、截断或横向溢出。
- [x] 页面返回 no-store、CSP、nosniff、DENY、no-referrer 安全头。Cloudflare Insights 注入脚本被严格 CSP 拦截，属非功能性控制台噪声，不影响 Relay 页面脚本。

## 4. 术语一致性

- `Cloud User` → `store.User` / `identity.Service`，命名一致。
- `Cloud Session` → `UserSession` / `userSessionCookie`，旧 AdminSession 命名已移除。
- `Node Owner` → `OwnerUserID` / owner-scoped Store methods，命名一致。
- `Bootstrap User` → `pending_verification` user + `ClaimOwnerlessNodes`，语义一致。
- `注册验证码` / `重置验证码` → `PurposeRegister` / `PurposePasswordReset`，purpose 隔离一致。
- 禁用词 grep 仅命中“旧路由应为 404”的测试和第三方 Go module 名称；未形成范围外产品能力。

## 5. 架构归并

- [x] `.codestable/architecture/cloud-relay-core.md`：用 Cloud User/Session/Node Owner 替换单 AdminSession 现状；归并 Identity Service、QQ SMTP、Argon2id、verification code、rate limit、owner migration 与控制面/数据面边界。
- [x] `.codestable/architecture/ARCHITECTURE.md`：总入口增加 Cloud User 和 Node Owner，并更新 Cloud Relay 模块摘要。
- [x] 代码锚点更新为 `internal/identity/` 和拆分后的 `sqlite_identity/binding/nodes/sessions.go`。
- [x] `attention.md` 已明确 local-service TODO 禁止推进，本次无须改动。

## 6. requirement 回写

- [x] requirement `mindfs-compatible-cloud-backend` 当前为 `current`，本次改变了用户故事和产品边界，已执行 update。
- [x] 新增“注册一次后邮箱密码登录”“多用户只能管理自己的节点”“V0 bootstrap 邮箱认领”用户故事。
- [x] `implemented_by` 加入 `cloud-email-accounts`，边界明确仅 QQ 邮箱、无用户名/OAuth/tenant/RBAC/share。
- [x] 变更日志记录 2026-08-05 账号、密码恢复、Session、owner 隔离与数据面不变。

## 7. roadmap 回写

- [x] design frontmatter 的 `roadmap: mindfs-cloud-relay` 与 `roadmap_item: cloud-email-accounts` 均完整。
- [x] `mindfs-cloud-relay-items.yaml`：`cloud-email-accounts` 从 `in-progress` 更新为 `done`，feature 链接保持不变。
- [x] `mindfs-cloud-relay-roadmap.md`：V1 第 5 项状态更新为 done，状态摘要与变更日志同步。
- [x] `relay-local-service-domains` 保持 `planned / TODO`，并明确不属于当前交付。
- [x] YAML/frontmatter 校验通过。

## 8. attention.md 候选盘点

- 本 feature 未暴露需要追加到 `attention.md` 的新项目级陷阱。
- Cloudflare Insights 被 CSP 拦截只属于当前代理注入的非功能性浏览器控制台噪声，不改变项目构建、运行或协议约定，因此不登记为 attention 候选。

## 9. 遗留

- `relay-local-service-domains`：用户明确不做，继续作为独立暂停 TODO，不是本 feature 遗留缺陷。
- 仅支持 `@qq.com`、SQLite 单实例、不支持共享/OAuth/tenant/RBAC：均为已批准边界，不是未完成项。
- 线上注册已由用户确认；密码重置、多用户 owner 隔离和 V0 迁移以自动化与真实 compat 为验收证据，未发现阻断问题。
- 实现阶段和验收阶段无需要另开 issue 的未处理 finding。

最终验证：

```text
go test ./...
go test -race ./app ./internal/identity ./internal/store ./internal/connector ./internal/gateway
go vet ./...
MINDFS_RUN_COMPAT=1 go test ./compat -run TestUnmodifiedNodeRelayCompatibility -count=1 -v
git diff --check
```

结果全部通过。
