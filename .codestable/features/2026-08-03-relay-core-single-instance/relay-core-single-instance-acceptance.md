---
doc_type: feature-acceptance
feature: 2026-08-03-relay-core-single-instance
status: passed
summary: 单实例 Cloud Relay 已完成绑定、Connector、yamux、HTTP/WS Gateway、SQLite 持久化和协议级验证
tags: [mindfs, cloud, relay, acceptance, yamux, websocket, sqlite]
---

# 单实例 Relay 核心闭环验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-08-03
> 关联方案 doc：`.codestable/features/2026-08-03-relay-core-single-instance/relay-core-single-instance-design.md`

## 1. 接口契约核对

**接口示例逐项核对**：

- [x] `GET /api/bind/poll` 首次返回 pending 和正数 `next_poll_after_ms`；实现位于 `cloud/app/binding_handlers.go:111`、`cloud/internal/binding/service.go:71`。
- [x] confirmed JSON 包含 `device_token`、`node_id`、`node_name`、`endpoint`；同一 code/device 重试完全一致。证据：`cloud/app/app_test.go:36`。
- [x] 不同 device ID 使用同一 code 只返回 `{"status":"claimed"}`。证据：`cloud/app/app_test.go:36`。
- [x] Connector 无效 Token 返回 401 `device_token_invalid`。证据：`cloud/app/protocol_integration_test.go:23`。
- [x] 管理员登录返回 bootstrap user、CSRF Token 和 HttpOnly/SameSite Session Cookie。证据：`cloud/app/binding_handlers.go:62`、`cloud/app/app_test.go:88`。
- [x] `/api/bind/status` 要求管理员 Session，Node 尚未 poll 时返回 `waiting_for_device`。证据：`cloud/app/app_test.go:158`。
- [x] `/api/bind/confirm` 要求 Session + CSRF，confirm 返回 Node URL，reject 返回 revoked。证据：`cloud/app/binding_handlers.go:145`。

**名词层“现状 → 变化”逐项核对**：

- [x] `Config` 包含方案定义的 V0 字段，管理员密码使用 `Secret`，Token Key 固定为 32 字节。代码：`cloud/internal/config/config.go:23`。
- [x] `BindChallenge`、`Node`、`DeviceTokenRecord` 与方案字段一致。代码：`cloud/internal/store/contracts.go:18`。
- [x] `NodePresence` 包含 node ID、connection ID、online 和 connected time。代码：`cloud/internal/connector/registry.go:18`。
- [x] `BindingService`、`DeviceTokenService`、`SessionRegistry` 均以方案术语形成接口。代码：`cloud/internal/binding/service.go:44`、`cloud/internal/binding/token.go:16`、`cloud/internal/connector/registry.go:38`。
- [x] SQLite 仅包含 `admin_sessions`、`bind_challenges`、`nodes`、`device_tokens`。证据：`cloud/internal/store/sqlite_test.go:55`。

**流程图核对**：

- [x] Node poll → SQLite challenge：`cloud/internal/binding/service.go:71`。
- [x] Admin confirm → Node + Token hash + challenge 同事务：`cloud/internal/store/sqlite.go:124`。
- [x] Connector → WebSocket net.Conn → `yamux.Server` → Registry：`cloud/internal/connector/handler.go:52`。
- [x] Public route → Registry open stream → Node：`cloud/internal/gateway/http.go:44`、`cloud/internal/gateway/websocket.go:35`。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 未修改 Node 可解析 confirmed 凭据并用 Bearer Token 建立 Connector；端到端证据：`cloud/app/protocol_integration_test.go:23`。
- [x] `/n/{nodeId}` 的 HTTP API 和 WebSocket 经同一 Connector 工作；端到端证据同上。
- [x] E2EE Header 原值保留、Node 收到的 path 已去 Relay 前缀，日志不记录 query/body/WS payload。证据：`cloud/internal/gateway/http_test.go:41`、`cloud/app/protocol_integration_test.go:23`、`cloud/app/request_log.go:54`。
- [x] Cloud 重启后 SQLite 绑定仍在，相同凭据可重新获取并用于重连。证据：`cloud/app/app_test.go:119`。
- [x] 删除 `cloud/` 和外部运行配置即可卸载本能力，根 module 和现有源码没有引用。

**明确不做逐项核对**：

- [x] 未修改 `server/`、`web/`、`cli/`、`android/`、`harmony/`、根 `go.mod`、`Makefile`、`agents.json`。
- [x] 未注册 Token Station、`/api/agents`、`/api/tips`、版本下载或本地服务 API；非空 binding purpose 明确返回 `invalid_request`。
- [x] 未实现节点管理、Token 轮换、多用户、OIDC、共享、配额、PostgreSQL、Redis 或多实例。
- [x] 未新增 Dockerfile、生产反代模板、readyz、metrics 或备份流程。
- [x] 未解析、解密或记录 E2EE body、Agent 消息、文件正文和 WebSocket payload。

**关键决策落地**：

- [x] D1 独立 Go module：`cloud/go.mod` 无根 module replace/import。
- [x] D2 单实例多 Node：Registry 以 node ID 建表，不限制 Node 数量。代码：`cloud/internal/connector/registry.go:43`。
- [x] D3 SQLite + 内存 session：`cloud/internal/store/sqlite.go:22` 与 `cloud/internal/connector/registry.go:38`。
- [x] D4 确定性 HMAC：严格使用 context、分隔符、code hash、device ID、node ID。代码：`cloud/internal/binding/token.go:41`。
- [x] D5 公网 route 使用 node_auth：Node 默认 `AccessMode: node_auth`。代码：`cloud/internal/binding/service.go:159`。
- [x] D6 Public URL 决定 ws/wss，拒绝 userinfo。代码：`cloud/app/app.go:124`、`cloud/internal/config/config.go:77`。
- [x] D7 `/bind` 为 Cloud 自有最小页面，不复用 `web/`。代码：`cloud/app/binding_handlers.go:17`。

**流程级约束核对**：

- [x] confirmed 幂等、不同 device 不泄露、确认事务原子提交。
- [x] 新 Connector 替换旧 session，Unregister 按 connection ID compare-and-delete。
- [x] Connector 断开后 Registry offline；新请求映射为 503。
- [x] HTTP body 由 `Request.Write` / `io.Copy` 流式转发，客户端取消关闭对应 stream。
- [x] 外部内部 Header 先删除再生成；E2EE Header 不改值。
- [x] WebSocket 单方向顺序保持，unknown/invalid → 1002，超限 → 1009，stream 统一释放。
- [x] 明文 Token 只出现在 confirmed JSON；数据库和请求日志不保存 Token、Authorization 或 payload。

**挂载点反向核对**：

- [x] M1 `cloud/go.mod`、`cloud/cmd/mindfs-relay` 可独立构建和删除。
- [x] M2 路由仅为方案清单八项，代码：`cloud/app/app.go:92`。
- [x] M3 环境配置键严格为方案清单六项，代码：`cloud/internal/config/config.go:39`。
- [x] M4 SQLite schema 严格为四张表，代码：`cloud/internal/store/schema.sql:1`。
- [x] M5 外部只需设置 `MINDFS_RELAY_BASE_URL` 指向 Cloud Relay。
- [x] 反向 grep 未发现清单外的现有源码挂载点。
- [x] 拔除沙盘：删除 `cloud/**` 和相关 `.codestable/**` 后，根 module、现有构建和客户端代码没有残留引用。

## 3. 验收场景核对

### 正常场景

- [x] **S1** 配置完整、SQLite 可写 → 启动并由 `/healthz` 返回 200。证据：`cloud/app/app_test.go:20`。
- [x] **S2** 首次合法 poll → pending + 正数轮询间隔。证据：`cloud/app/app_test.go:36`。
- [x] **S3** 管理员确认 → Node 下次 poll 得到 confirmed JSON。证据同上。
- [x] **S4** 同一 code/device 重复 poll → 四项凭据完全一致。证据同上。
- [x] **S5** 合法 Token 建立 Connector，Registry online，并接受 Cloud stream。证据：`cloud/internal/connector/handler_test.go:27`。
- [x] **S6** `/n/{nodeId}/` → Node 收到 `/`。证据：`cloud/internal/gateway/http_test.go:101`。
- [x] **S7** HTTP method/query/body/response 经 Gateway 保持。证据：`cloud/internal/gateway/http_test.go:41`。
- [x] **S8** WebSocket text、binary、close 双向保持。证据：`cloud/internal/gateway/websocket_test.go:21`。
- [x] **S9** HTTP/WS E2EE Header 保持，canonical path 无节点前缀。证据：`cloud/app/protocol_integration_test.go:23`。
- [x] **S10** Cloud 重启保留绑定；Node 使用原 Token 可重连。证据：`cloud/app/app_test.go:119` 与端到端 Connector 测试。

### 边界场景

- [x] **S11** `/bind` 早于 Node poll → `waiting_for_device`，不签发凭据。证据：`cloud/app/app_test.go:158`。
- [x] **S12** 不同 device 使用同一 code → claimed 且无凭据。证据：`cloud/app/app_test.go:36`。
- [x] **S13** challenge 超时 → expired，确认返回 `bind_expired`。证据：`cloud/app/app_test.go:158`。
- [x] **S14** 管理员拒绝 → revoked。证据同上。
- [x] **S15** 第二 Connector 替换旧连接，旧清理不影响新连接。证据：`cloud/internal/connector/registry_test.go:21`。
- [x] **S16** 伪造内部 Header 被覆盖/删除。证据：`cloud/internal/gateway/http_test.go:41`。
- [x] **S17** `/n/{nodeId}` 与 `/n/{nodeId}/` 均规范为 `/`。证据：`cloud/internal/gateway/http_test.go:101`。
- [x] **S18** WebSocket 超限 → 公网 1009、Node 收到 1009 frame、stream 释放。证据：`cloud/internal/gateway/websocket_test.go:158`。
- [x] **S19** 公网 HTTP 取消触发当前 stream close；Registry 和其他 stream 不被删除。实现证据：`cloud/internal/gateway/http.go:189`，race 测试通过。

### 错误场景

- [x] **S20** Token Key 缺失/错误 → 配置加载失败、尚未监听。证据：`cloud/internal/config/config_test.go:9`。
- [x] **S21** SQLite 无法打开 → `app.New` 失败且不降级。证据：`cloud/app/app_test.go:207`。
- [x] **S22** 管理员凭据错误 → 401 `auth_required`，不创建 Session。证据：`cloud/app/app_test.go:88`。
- [x] **S23** confirm 缺 Session/CSRF → 401/403，challenge 保持 pending。证据同上。
- [x] **S24** Connector 无效 Token → 401 `device_token_invalid`。证据：`cloud/app/protocol_integration_test.go:23`。
- [x] **S25** 不存在 node → 404 `node_not_found`。证据：`cloud/internal/gateway/http_test.go:77`。
- [x] **S26** 离线 node → 503 `node_offline`。证据同上。
- [x] **S27** stream 打开超时 → 504 `node_timeout`。证据同上。
- [x] **S28** Node WS 非 101 → 状态、Header、body 原样返回，不升级公网。证据：`cloud/internal/gateway/websocket_test.go:96`。
- [x] **S29** Connector 非 binary transport → Connector 关闭、Node offline。证据：`cloud/internal/connector/handler_test.go:115`。
- [x] **S30** 未知 frame / 非法 opcode → 公网 1002、stream 释放。证据：`cloud/internal/gateway/websocket_test.go:126`。

### 反向核对

- [x] **S31** 现有 MindFS 源码和根构建文件零改动。
- [x] **S32** `cloud/go.mod` 不引用 `mindfs/server/internal`。
- [x] **S33** Cloud 路由没有 Token Station、Hosted Content、版本或本地服务 API。
- [x] **S34** SQLite 没有 V1/V2/V3 表。证据：`cloud/internal/store/sqlite_test.go:55`。
- [x] **S35** 数据库不含明文 Device Token 或管理员密码；日志不记录 Authorization/body/WS payload。证据：`cloud/app/app_test.go:222`。
- [x] **S36** 未新增 Dockerfile、生产反代、PostgreSQL 或 Redis 依赖。

## 4. 术语一致性

- `CloudConfig` 对应代码 `config.Config`，字段语义一致；`AdminPassword` 使用 `Secret`。
- `Binding Challenge`：`BindChallenge` 全部命中同一状态模型。
- `Device Token`：`DeviceTokenService` 是派生/鉴权接口，数据库只出现 `TokenHash`。
- `Connector`：只指 Node 长期 WebSocket；代码未把 Gateway WebSocket 称作 Connector。
- `Relay Session`：`RelaySession` 只用于 Registry 持有的 yamux session。
- `Gateway Stream`：Gateway 通过 `SessionRegistry.OpenStream` 获得，不和 Connector transport 混用。
- 防冲突 grep 未发现 `cloud server`、第二套 pairing code 或 import 上游 internal 包。

## 5. 架构归并

- [x] 新增 `.codestable/architecture/cloud-relay-core.md`，写入 Binding、Connector、Gateway、SQLite 和内存 Registry 的现状、数据归属与稳定约束。
- [x] 更新 `.codestable/architecture/ARCHITECTURE.md`，增加 Cloud Relay 子系统入口、核心术语和上游只读边界。
- [x] 架构文档中的结构化断言均有 `cloud/**:line` 代码锚点；没有写入未来模块计划。
- [x] `cloud` type 当前只有一份架构 doc，不触发同类 ≥6 聚合。

## 6. requirement 回写

- [x] `.codestable/requirements/mindfs-compatible-cloud-backend.md` 从 `draft` 升级为 `current`。
- [x] `implemented_by` 更新为 `[cloud-relay-core]`，保留原始愿景、用户故事和边界。
- [x] 文末追加 2026-08-03 变更日志。
- [x] `.codestable/requirements/VISION.md` 已从 Draft 移到 Current。

## 7. roadmap 回写

- [x] `mindfs-cloud-relay-items.yaml` 中 `relay-core-single-instance` 从 `in-progress` 更新为 `done`。
- [x] roadmap 主文档 V0 子 feature 清单同步为 `done`，对应 feature 为 `2026-08-03-relay-core-single-instance`。
- [x] roadmap frontmatter 的 `related_architecture` 加入 `cloud-relay-core`。
- [x] YAML 校验通过。

## 8. attention.md 候选盘点

- [x] 无候选。Go 安装属于当前机器环境，不是仓库每个 feature 都必需重复记录的项目特殊规则；独立 module 构建命令已进入 architecture 和 roadmap。

## 9. 遗留

- 后续优化点：无本 feature 内未处理偏差。
- 已知限制：单实例、bootstrap 管理员、SQLite、无节点管理/Token 轮换、无生产部署模板；均是已批准的 V0 边界并已在 roadmap 分配后续条目。
- 实现阶段顺手发现：无范围外问题被修改。
- 独立黑盒兼容矩阵仍由 roadmap 的 `relay-compatibility-suite` 承担；本 feature 已提供 Cloud 内协议级端到端测试，但没有修改或启动真实上游 UI 自动化。
