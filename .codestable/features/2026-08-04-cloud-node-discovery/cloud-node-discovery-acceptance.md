---
doc_type: feature-acceptance
feature: 2026-08-04-cloud-node-discovery
status: passed
accepted: 2026-08-05
round: 1
---

# Cloud Node Discovery 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-08-05
> 关联方案：`.codestable/features/2026-08-04-cloud-node-discovery/cloud-node-discovery-design.md`
> 关联 review：`cloud-node-discovery-review.md`（status=passed, reviewer=subagent）
> 实现：commit `7ae11d9`，已部署 relay.20260310.best 运行

## 1. 接口契约核对

对照 design 第 2.1 节名词层逐一核查：

- [x] Relay Node Payload（`relay_nodes_handlers.go:16-23`）：`id/name/status/last_seen_at/base_url` + 未导出 `createdAt`（不序列化）。实测 JSON 只含 5 个约定字段，`device_id/token/hash/connection_id` 不出现 → 一致。`browser_handlers_test.go` 有显式断言。
- [x] Relay Auth Me Payload（`relay_browser_handlers.go:123-138`）：有效 session 返回 `{id:usr_bootstrap,name,username,roles:[owner]}`，无 session 返回 401 → 一致。
- [x] Rename Request（`relay_nodes_handlers.go:59-85`）：`PATCH /api/nodes/{id}` 接受 `{name}`，TrimSpace 后判空 400，成功返回更新后 payload → 一致。
- [x] Delete Request（`handleRelayNodeDelete`）：返回 `{success:true}` → 一致。
- [x] 流程图（design 2.2）：Browser→/login→AdminSession→/nodes→/api/nodes→SQLite+Registry→/n/{id}/ 各节点均在代码有落点（app.go 路由 + 各 handler）→ 一致。

无偏差。

## 2. 行为与决策核对

对照 design 第 1 节 + 2.2：

- [x] 核心行为：/login 登录、/nodes 查看、在线打开、rename、delete 均实现（design 1 核心行为）。
- [x] 决策 1（只用客户端既有 /api/nodes，不新增 /api/cloud/v1/nodes）：grep 无 `/api/cloud/v1/nodes` → 一致。
- [x] 决策 2（/nodes 独立 HTML，不挂 web/dist）：`browserNodesPage` 是独立 template → 一致。
- [x] 决策 3（复用 AdminSession，无 CloudPrincipal/tenant）：无新用户模型 → 一致。
- [x] 决策 6（SQLite 事实源 + Registry 在线快照）：`ListNodes` 读 SQLite，`relayNodePayload` 合并 `registry.Status().Online` → 一致。
- [x] 决策 7（删除事务撤销 node+token，再关 session）：`DeleteNode` 单事务 + `Disconnect` 在后 → 一致。
- [x] 决策 8（base_url 服务端按 node ID 生成同源）：`publicNodeURL` 用 config.PublicURL 拼 `/n/{id}/`，不接受 DB 外部 URL → 一致。
- [x] 流程级约束：401 unauthorized / 403 forbidden / 404 node_not_found / 400 invalid_request / 500 request_failed 语义齐全；no-store；同源 Origin+SameSite；delete 撤销先于 session 关闭；排序 online→last_seen desc→created desc→id asc → 全部落地。

**明确不做反向核对**（design 3 反向项）：无 /api/cloud/v1/nodes、CloudPrincipal、tenant、ACL、注册、邮箱码、OAuth；根路径无 /api/dirs、/api/relay/status；未改 SQLite schema；`git show --stat` 确认 server/web/cli/android/harmony/根构建零 diff；无共享/rotate/分页/搜索/presence 推送 → 全部确实没做。

**挂载点反向核对**（design 2.3）：grep 确认 4 类挂载点（浏览器路由 /nodes·/login·/api/auth/me·logout、Nodes API GET·PATCH·DELETE、Store list·rename·delete、Registry Disconnect）均有代码落点，无清单外引用。拔除沙盘：移除上述 handler + 路由 + Store 契约 + Disconnect 后，Binding/Connector/Gateway/SQLite schema/Public Node Route 不受影响 → 可卸载。

## 3. 验收场景核对

对照 design 第 3 节 20 场景：

- [x] S2 bootstrap 登录、S4 认证状态、S6 空集合 200 []、S7 在线状态与排序、S10 重命名（空 400/不存在 404）、S11 重命名跨源 403、S12 删除、S13 旧 token 重连失败、S14 删除隔离、S15 数据最小化、S16 Store 失败 500、S17 logout、S18 绑定回归、S19 Relay 回归：由 cloud 自动测试 + code review 覆盖 → **re-verified**（go test -count=1 ./app ./internal/store ./connector 全 ok）。
- [x] S3 安全 next（外部/协议相对/反斜杠/非允许路径→/nodes）：`safeRelayRedirect` 链式校验 code review 覆盖 → re-verified。
- [~] S1 未登录页、S5 登录后列表、S8 节点打开/离线禁用、S9 移动端 DOM anchor、S20 VPS 用户路径：属浏览器 UI 场景 → **trust-prior-verify**（已部署运行 + 用户拥有前端验收，seq 95）。

**review 报告复核**：review 第 4/5 节 findings 与 QA focus 已逐条纳入本报告第 9 节遗留与第 10 节 inline matrix；无 unresolved blocking。

**功能性前端**：/nodes 控制台与 /login 已部署且用户报告"运行完美"；按用户既定分工（前端自验），UI 场景标 trust-prior-verify，**需用户终审肉眼确认**（见第 10 节）。

## 4. 术语一致性

对照 design 第 0 节 + 2.1 命名 grep：RelayNodePayload、handleRelayNodesList/Rename/Delete、handleBrowserLogin/Nodes/AuthStatus/Logout、safeRelayRedirect 命名与 design 术语一致；禁用词（CloudPrincipal/tenant/api/cloud/v1/nodes）grep 无命中 → 一致。

## 5. 领域影响盘点（提示而非代写）

- 新名词 Relay Nodes Console / Relay Node Payload：属 feature 本地术语；`/api/nodes`（控制台契约）vs `/api/dirs`（Node 内部）区分已写入 architecture（第 §2 归并），不必再进 CONTEXT.md → 建议：不需要（已由 architecture 覆盖）。
- 结构性选择：删除=节点撤销（事务撤销 token+node→关 session），已记入 architecture 关键决策/§2 → 建议：不需要额外 ADR（V0 决策，architecture 已承载）。
- 流程级约束：/login?next= 严格同源、同源 Origin 写保护 → 已记入 architecture → 不需要。
- 结论：本 feature 的领域沉淀已通过 architecture 归并承载；如用户认为控制台流程值得进 CONTEXT.md，可后续走 cs-domain，accept 不代写。

## 6. requirement delta / clarification 回写

- requirement = `mindfs-compatible-cloud-backend`，status=**current**。
- 判据分支 7：requirement 指向 current req，本次未改用户视角边界/pitch/用户故事——节点发现属该 req 愿景内"最核心远程访问能力"的可用性补全（req 用户故事已含"先部署最核心远程访问"）。
- 结论：**req 未变，无需 delta**。本次能力补全已于 2026-08-04 写入 req 变更日志（last_reviewed=2026-08-05）。无 approved delta 需求，未自由重写。

## 7. roadmap 回写

- design frontmatter `roadmap: mindfs-cloud-relay` / `roadmap_item: cloud-node-discovery`（本轮已补齐双向链接）。
- `items.yaml`：`cloud-node-discovery` 已 `status: done` + `feature: 2026-08-04-cloud-node-discovery`（对齐阶段已回写，yaml 校验通过）。
- `roadmap.md`：V0 段第 4 项已列为 done（V0 修正项），V1 `cloud-node-management` 已收窄为 Token 轮换，编号 1-22 连续。
- 结论：roadmap 回写**已完成**（非本轮新写，对齐阶段已做），本轮仅确认一致。

## 8. attention.md 候选盘点

- 候选 1：`/api/auth/me` 语义——客户端原始契约是 401→login（commit 41055c0），cloud 曾用 stub（auth_required=false）抑制，node-discovery 拉回 401。这是后续触碰 cloud 认证时容易再踩的点。建议：可加 attention.md 一条「cloud /api/auth/me 对非 admin 返 401，客户端 WS 断线据此跳 /login；node_auth 直访场景需注意」。**不擅自写入**，交用户决定。
- 候选 2：rename 无长度上限、logout 无 Origin 校验——属 review nit，非每个 feature 都踩，不进 attention。

分流：稳定技术约束（/api/nodes vs /api/dirs、删除事务顺序）已进 architecture；可复用坑（/api/auth/me 语义）建议 `cs-keep` 沉淀 compound 或 attention，退出后提示。

## 9. 遗留

- **REV-001（important）**：`/api/auth/me` 改 401 后，node_auth-only 直接访问 `/n/{id}/` 的用户 WS 断线会跳 `/login`（design 已批准契约，单 admin 流不触发）。需 QA 验证 admin 主路径不被误伤 + 产品决策 node_auth 直访断线是否应弹 login。未在本验收修（属设计决策）。
- review nit/suggestion（REV-002 logout CSRF、REV-003 publicNodeURL 子路径、REV-004 rename 长度、REV-005 DELETE 404 孤儿 session、REV-006 logout 服务端失效）：均 V0 可接受，建议后续 issue 跟进。
- 无独立 `cs-feat-qa` 报告：本验收用 accept-inline verification（第 10 节）补齐同等验证证据。
- 实现阶段未"顺手发现"需追踪项。

## 10. 最终审计

- 验证证据来源：accept-inline verification（无 qa.md）
- Evidence sources：无 evidence-pack/dod/gate（非 goal/gate 模式）
- Inline Verification Matrix：
  | ID | 来源 | 核心性 | 命令/动作 | 结果 |
  |---|---|---|---|---|
  | V1 | 终端 | 高 | `go vet ./...` | exit 0，无告警 |
  | V2 | 终端 | 高 | `go test -count=1 ./app ./internal/store ./internal/connector` | 全 ok（app 2.2s/store 0.94s/connector 1.37s） |
  | V3 | 终端 | 中 | `go test ./...` | 全 ok（cached） |
  | V4 | 终端 | 高 | `git show 7ae11d9 --stat` 范围核验 | 仅 cloud/** + .codestable/**，客户端零修改 |
  | V5 | code review | 高 | safeRelayRedirect 开放重定向防御 | //host、https://host、\、%5c、.、..、空 ID 全拒绝 |
  | V6 | code review | 高 | DELETE 事务 + Disconnect 顺序 | token+node 原子，affected==0 回滚，session 关闭在后 |
  | V7 | code review+test | 高 | payload 最小化 | createdAt 未导出，device_id/token/hash 不入 JSON，有断言 |
  | V8 | code review | 高 | authorizeRelayWrite 同源 fail-closed | 缺/异源 Origin → 403 |
  | V9 | 部署+用户 | 高 | /nodes 控制台、/login、打开/重命名/删除节点 | trust-prior-verify（已部署"运行完美"+用户自验前端） |
- 聚合命令：`go vet ./...` exit 0；`go test -count=1 ./app ./internal/store ./internal/connector` 全 ok。
- 场景复核：re-verified 14（逻辑/API/安全/事务/数据最小化场景） / trust-prior-verify 6（浏览器 UI 场景 S1/S5/S8/S9/S20 等）。**trust-prior 比例 >30%**：UI 场景需用户终审肉眼确认（/nodes 列表渲染、在线/离线打开、rename/delete 交互、移动端 anchor）。
- 交付物复核：代码（handler/store/registry）✅ / 路由（app.go）✅ / Store 契约（contracts.go）✅ / architecture（cloud-relay-core.md §2/§8 已归并）✅ / requirement（changelog 已补）✅ / roadmap（items.yaml done + 主文档）✅ → 全通过。
- 完整工作区复核：未提交改动均为 `.codestable/**` 文档（review.md/acceptance.md/items.yaml/roadmap.md/requirement.md/design frontmatter/architecture），无未跟踪代码、无暂存污染 → 通过。
- diff 清洁度：无 debug 输出/临时 TODO/注释代码/无用 import/方案外文件 → 通过。
- 知识沉淀出口：architecture 已承载技术约束；attention/compound 候选（/api/auth/me 语义）已在第 8 节登记，退出后提示 cs-keep/cs-note。
- 结论：**通过**。唯一注意项为 REV-001 的 QA 验证与产品决策（非验收阻断，属已批准 design 契约），以及 UI 场景需用户终审肉眼确认。
