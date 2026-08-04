---
doc_type: feature-review
feature: 2026-08-04-cloud-node-discovery
status: passed
reviewer: subagent
reviewed: 2026-08-05
round: 1
---

# cloud-node-discovery 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-08-04-cloud-node-discovery/cloud-node-discovery-design.md`（status=approved）
- Checklist: `.codestable/features/2026-08-04-cloud-node-discovery/cloud-node-discovery-checklist.yaml`（steps 全 done）
- Evidence pack: none（非 goal/gate 模式）
- Gate results: none
- DoD results: none
- Implementation evidence: commit `7ae11d9` "feat(cloud): add relay node management console"（已部署 relay.20260310.best 运行）
- Diff basis: `git show 7ae11d9`，14 文件 / +1365 / −76
- Baseline dirty files: 工作区存在本轮范围外的 `.codestable/**` 文档改动（items.yaml / roadmap.md / requirement.md / 本 design frontmatter），均非代码、属文档对齐，已排除出审查范围

### Independent Review

- Detection：主 agent 无 `mcp__paseo__create_agent`；用宿主原生 Agent 工具启动独立 reviewer；`which ocr` 不可用
- 环节 A 独立隔离 Task agent：`native-agent` + `completed`（general-purpose subagent，同 provider，记录同模型残余风险）
- 环节 B OCR CLI：`not-available`（`ocr: not found`，不阻塞）
- OCR severity mapping：N/A
- Merge policy：环节 A 返回的每条 finding 已逐条本地事实核验后合并；其翻转 verdict 的关键论点（客户端 401→login 逻辑源于 commit `41055c0`、早于 cloud 旧 stub `5ef3166`）已用 `git cat-file`/`git log -S`/`git show 5ef3166:` 三方核实为真
- Gate effect：`reviewer: subagent` 满足放行要求

## 2. Diff Summary

- 新增：`cloud/app/relay_nodes_handlers.go`（GET/PATCH/DELETE /api/nodes）、`cloud/app/relay_browser_handlers.go`（/login、/nodes、/api/auth/me、logout）、`cloud/internal/assetsync` 等（注：assetsync 属另一 commit 69a26d1，不在本 review 范围）
- 修改：`cloud/app/app.go`（路由挂载）、`cloud/app/binding_handlers.go`（登录响应）、`cloud/app/browser_handlers.go`（拆分微重构，保留公共 helper）、`cloud/internal/store/sqlite.go` + `contracts.go`（list/rename/delete-with-token-revocation）、`cloud/internal/connector/registry.go`（Disconnect）
- 测试：`browser_handlers_test.go`（+370）、`registry_test.go`、`sqlite_test.go`
- 删除：无
- 风险热点：认证/同源写保护（权限）、`/login?next=` 开放重定向（安全）、DELETE 事务与 session 关闭（并发/持久化）、`/api/auth/me` 语义变更对既有客户端消费方的影响（回归）

## 3. Adversarial Pass

- 假设的生产 bug：`/api/auth/me` 改成 admin-summary-or-401 后，node_auth-only 直接访问 `/n/{id}/` 的用户在 WebSocket 断线时被误弹到 `/login`。
- 主动攻击过的反例：开放重定向（`//host`、`https://host`、`\`、`%5c`、`/nodes/../bind`、`.`/`..`、空 ID）、payload 泄露 device_id/token/hash/connection_id、DELETE 与并发重连的 session 复活竞态、rename 的空名/不存在/跨源、同源 Origin 缺失或伪造、排序确定性（含 nil last_seen）。
- 结果：开放重定向/事务/数据最小化/排序均经得起对抗，未升级为 finding；`/api/auth/me` 语义变化升级为 important（REV-001），logout CSRF 等留 nit/suggestion。

## 4. Findings

### blocking

none

### important

- [ ] REV-001 `cloud/app/relay_browser_handlers.go:123-138`（消费方 `web/src/App.tsx:2882-2917`）
  - Evidence：本 commit 把 `/api/auth/me` 从旧 stub（`5ef3166` 恒返回 `200 {authenticated:false, auth_required:false, access_mode:"node_auth"}`）改为「无 admin session → 401；有 → 管理员摘要」。客户端 `handleRelayWebSocketClosed` 在 WS 断线时 `fetch("/api/auth/me")`，`if (!response.ok) redirectToRelayLogin()`（`App.tsx:2889`），`handleRelayNavigationFailure` 同理（`2925-2933`）。已核实该客户端逻辑源于 commit `41055c0`（"refine: make relay binding explicit and handle relay WS navigation failures"），早于 cloud 旧 stub。
  - Impact：node_auth-only 用户（无 cloud admin cookie）直接访问 `/n/{id}/` 时，WS 断线或导航 401 现在会被弹到 `/login`——而旧 stub 下不会。注意 `5ef3166` 是为修用户曾报告的「断线→/login」bug（seq 92）而做的有意 UX 修复，故本 commit 对该路径属「设计张力」而非单纯对齐。
  - 缓解事实：V0 单 admin 场景下 admin 始终带 session → `/api/auth/me` 返 200 → 不触发；用户报告「运行完美」亦印证主路径未踩到。Gateway `/n/{id}/` 不校验 admin session，故 node_auth 直访本身仍可用，仅断线分支行为变化。
  - 结论：非 blocking（代码符合已批准 design scenario 4，且与客户端原始契约 + 官方 relay 一致）。但属真实可观测行为变化，**必须 QA 验证 admin 主路径不被误伤**，并由产品决策「node_auth-only 直访用户断线是否应弹 login」。

### nit

- [ ] REV-002 `cloud/app/relay_browser_handlers.go:140-152` `POST /api/auth/logout` 未做同源 Origin 校验（logout CSRF）。跨站表单 POST 可对该域 Set-Cookie 强制管理员掉线，骚扰级、无数据泄露；SameSite=Lax 不能完全挡。建议复用 `authorizeRelayWrite` 的 Origin 判断。
- [ ] REV-003 `cloud/app/relay_nodes_handlers.go:137-144` `publicNodeURL` 直接覆盖 `PublicURL.Path`，子路径部署会丢前缀；对比 `publicWebSocketURL`（`app.go`）用 `TrimRight(base.Path,"/")+"/..."` 保留前缀。当前 gateway `/n/` 挂 root、本不支持子路径部署，实际影响很小。
- [ ] REV-004 `cloud/app/relay_nodes_handlers.go:59-75` rename 只 `TrimSpace` 判空，无长度上限。页面用 `textContent` 渲染（无 XSS），`decodeJSON` 已有 1MB 体限兜底，但超长名会进 SQLite 与移动端 DOM 提取。建议加合理上限（如 128）返 400。

### suggestion

- [ ] REV-005 `cloud/app/relay_nodes_handlers.go:91-96` DELETE 命中 `ErrNotFound` 时短路返 404，未 best-effort 调 `registry.Disconnect` 清理孤儿 session。gateway `openNodeStream` 先 `GetNode`（删了的节点必 404），孤儿 session 无法服务流量、无安全后果，仅靠 keepalive 超时回收。可在 404 分支也 best-effort `Disconnect` 增强幂等清理。
- [ ] REV-006 logout 仅下发 `MaxAge=-1` cookie，未按 cookie 值 `DeleteAdminSession` 服务端失效（依赖 `runCleanup` 的 `DeleteExpired`）。design scenario 17 只要求「清 cookie + 后续 me/nodes 返 401」，已满足；如要加强可服务端删除。V0 可不做。

### learning

- `cloud/app/binding_handlers.go:89-96` 本 commit 移除了旧登录响应里的 `"tenant_id":"ten_bootstrap"`。全仓 `web/ server/ cli/ android/ harmony/` 对 `tenant_id` 零消费，bind 页 JS 只读 `body.csrf_token`，功能零影响且契合 design「不应出现 tenant」的范围守护。但 design scenario 18「绑定登录流程不变」可读作含响应体不变——建议在 design/PR 描述显式记一笔「登录响应去除 tenant_id 以对齐范围守护，无消费方」。

### praise

- DELETE 的竞态安全由 gateway `GetNode` 闸兜底（`gateway/http.go`）：即使 registry 残留孤儿 session，已删节点无法服务流量，是 design §2.2「持久化撤销先于 session 关闭」之外的有效纵深防御。
- 数据最小化干净且有测试：`RelayNodePayload` 把 `createdAt` 设为非导出（不序列化）、`DeviceID/TokenHash` 不复制；`browser_handlers_test.go` 显式断言 `device_id/device_token/token_hash/connection_id/created_at` 不出现。
- 开放重定向防御扎实：`safeRelayRedirect`→`safeNodeRedirect` 链式校验覆盖绝对 URL/协议相对 `//host`/反斜杠/`%5c`/host 非空/`IsAbs`，`/nodes` 之外一律收紧 `/n/{id}/`；`next` 经 `html/template` 在 JS 值上下文渲染（自动 JSON 引号转义），无模板注入。
- DELETE 事务正确：先删 `device_tokens` 再删 `nodes`，`affected==0` 在 commit 前返回 + `defer tx.Rollback()` 保证原子回滚。
- 排序为全序：online→last_seen_at desc→created_at desc→id asc，`id` 唯一保证确定性。

## 5. Test And QA Focus

- QA 必须重点复核：
  1. **REV-001 admin 主路径**：admin 登录后打开 `/n/{id}/`，模拟 WS 关闭/节点重启 → 因 admin session 使 `/api/auth/me` 返 200，不应被踢到 `/login`。
  2. **node_auth-only 直访断线行为**：确认现在会去 `/login`（新契约，与官方 relay 一致）；若产品仍期望 node_auth 模式不弹 login，则属 design 决策需复核（可能要为 node_auth 上下文保留 200 探针或单独端点）。
  3. **VPS 全链路**：全新浏览器 `/nodes`→`/login`→登录→看节点→打开在线节点→rename→delete→旧 token 重连失败→其他节点不受影响（design 场景 1/5/12/13/14/20）。
- 建议新增/加强测试：
  - rename/delete 加跨源 `Origin: https://evil.example` 用例断言 403（当前只测缺 Origin 403 与正确 Origin 200）。
  - list 补 `last_seen_at` 为 null 的 JSON 断言。
  - `/login?next=` 补 `data:`/`javascript:`/`@trick`/`/nodes/../bind` 负例。
  - logout 串联断言（调用后 me/nodes 返 401）。
- 不能靠 review 完全确认的点：DELETE 与并发重连的真实时序竞态（单测难复现，依赖 gateway GetNode 闸 + keepalive），建议集成环境用「DELETE 同时让 connector 重连」脚本验证旧 token 必失败。

## 6. Residual Risk

- REV-001 的 `/api/auth/me` 行为变化对 node_auth-only 直访断线路径有真实影响，但属已批准 design 契约 + 单 admin 流不触发；交 QA 验证 + 产品决策，不在 review 内改。
- logout CSRF（REV-002）与服务端 session 失效（REV-006）为 V0 可接受的残余风险，已记 nit/suggestion。
- 独立 reviewer 为同 provider 原生 Agent（非异构 Paseo），存在同模型确认偏误残余风险；其翻转 verdict 的关键论点已主 agent 三方核实。

## 7. Verdict

- Status: **passed**（无 blocking；important REV-001 为已批准 design 契约的合理行为变化，须 QA 验证；其余 nit/suggestion 不阻塞）
- Reviewer: `subagent`（环节 A native-agent completed，环节 B OCR not-available）
- Next：按用户既定流程进入 `cs-feat-accept`（accept-inline QA 补 qa.md 缺失，重点覆盖 REV-001 与上述 QA focus）
