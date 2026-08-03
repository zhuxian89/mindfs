---
doc_type: feature-design
feature: 2026-08-03-relay-core-single-instance
requirement: mindfs-compatible-cloud-backend
roadmap: mindfs-cloud-relay
roadmap_item: relay-core-single-instance
status: approved
summary: 在独立 cloud Go module 中实现可被未修改 MindFS 客户端使用的单实例 Relay 核心闭环
tags: [mindfs, cloud, relay, binding, yamux, websocket, e2ee]
---

# 单实例 Relay 核心闭环

## 0. 术语约定

| 术语 | 定义 | 防冲突结论 |
|---|---|---|
| MindFS Node | 现有 server 进程及其 Relay 客户端 | 不称为 cloud server，现有代码只读 |
| Cloud Relay | 本 feature 新增的独立 cloud 后端进程 | 与 server/internal/relay 客户端包分开 |
| Binding Challenge | 由 Node 本地生成 code、由 Cloud Relay 观察并确认的一次绑定状态 | 对应客户端 pending code，不引入第二套 pairing code |
| Device Token | Node 建立 Connector 时使用的 Bearer 凭据 | 按 roadmap 使用确定性 HMAC 派生，数据库只存 hash |
| Connector | Node 主动连接 Cloud Relay 的长期 WebSocket | WebSocket 上承载 yamux，不指浏览器 WebSocket |
| Relay Session | Cloud Relay 为一个在线 Node 持有的 yamux.Server session | 一个 node ID 同时只允许一个 active session |
| Gateway Stream | Cloud Relay 为一次公网 HTTP 或 WebSocket 请求打开的 yamux stream | 与 Connector 长连接区分 |
| Public Node Route | /n/{nodeId}/... 公网入口 | 前缀只存在于公网侧，转给 Node 前必须去除 |

术语来源已核对：

- server/internal/relay/manager.go:61-75、127-147、336-383 定义 Relay base、pending code 与绑定轮询状态。
- server/internal/relay/service.go:331-377 定义 Connector WebSocket 与 yamux.Client。
- server/internal/relay/service.go:379-495 定义 Node 接收 Gateway Stream 后的 HTTP/WS 分流。
- server/internal/relay/wsconn.go:17-252 定义 WebSocket net.Conn 和 WS stream framing。
- web/src/services/base.ts:3-62 定义 /n/{nodeId} 前缀传播。
- web/src/services/e2ee.ts:607-615 定义 E2EE proof 使用去前缀后的 canonical path。

## 1. 决策与约束

### 需求摘要

为希望完全自托管的 MindFS 用户提供最小可用 Cloud Relay。用户只配置 MINDFS_RELAY_BASE_URL，不修改客户端，即可完成管理员确认绑定、Node 主动连接、浏览器经公网入口访问本地 MindFS 的 HTTP 与 WebSocket。

成功标准：

- 未修改 Node 能取得客户端当前识别的 confirmed 凭据并自动建立 Connector。
- 浏览器访问 /n/{nodeId}/ 后，静态页面、HTTP API 和 Agent WebSocket 可通过同一 Connector 工作。
- E2EE headers、密文和 proof path 语义不被 Cloud Relay 改坏。
- Cloud Relay 重启后保留绑定和 Token，Node 重连后恢复公网访问。
- 删除整个 cloud 目录和运行配置后，现有 MindFS 仓库行为完全不受影响。

### 明确不做

- 不修改 server、web、cli、android、harmony、根 go.mod、Makefile 或 agents.json。
- 不实现 Token Station、/api/agents、/api/tips、版本下载和本地附加服务。
- 不实现节点管理 API、Token 轮换、多用户、OIDC、共享、配额、PostgreSQL、Redis 或多实例。
- 不实现 Docker、生产反向代理模板、readyz、metrics 和备份流程；这些属于后续 V0 Feature。
- 不解密、解析或记录 E2EE payload、Agent 消息、文件正文和 WebSocket ciphertext。
- 不在本 feature 建立完整跨版本黑盒测试矩阵；这里只覆盖核心模块自身的协议测试。

### 复杂度档位

这是对公网暴露且承载长连接的服务，采用对外发布服务默认档位，并明确以下偏离或补充：

- 安全性 = hardened（偏离默认 validated：绑定、Bearer Token、Header 注入和公网代理处于对抗性环境）。
- 并发 = thread-safe（单进程内同时服务多个 Node 和多个 Gateway Stream）。
- 兼容性 = cross-version（只依赖现有客户端可观察协议，不 import 客户端内部包）。
- 幂等性 = idempotent（同一 Binding Challenge 和 device ID 的 confirmed 轮询必须返回相同凭据）。
- 可观测性 = logged（偏离默认 traced：V0 先提供结构化关键路径日志，完整 metrics/traces 留给后续 Feature）。

### 关键决策

1. **同仓库独立 Go module**
   Cloud Relay 只存在于 cloud/，使用 cloud/go.mod。拒绝在根 module 内新增包，因为这会污染上游更新边界。

2. **单实例不是单节点**
   V0 只有一个进程和一个 bootstrap 管理员，但可绑定多个 Node。拒绝只支持一个 Node，因为 node ID 路由和 Session Registry 本身就是多节点模型，限制为单节点反而制造后续迁移。

3. **SQLite 保存控制面，内存保存在线连接**
   Binding、Node、Token hash 和管理员 Session 需要跨重启保存；yamux session 只能存在于当前进程内。拒绝把在线连接状态写入 SQLite。

4. **Device Token 使用确定性 HMAC 派生**
   按 roadmap 方案 1，从独立 Token Key 和 challenge 元数据派生，持久层只保存 hash。拒绝一次性随机 Token，因为响应丢失会迫使重新绑定。

5. **公网路由默认 node_auth**
   Cloud Relay 不要求浏览器登录，内容安全由本地 MindFS 认证和可选 E2EE 负责。拒绝在 V0 增加云账号 ACL，因为现有客户端没有该前置流程。

6. **TLS 可由外部终止，协议由 Public URL 决定**
   核心进程允许本地 HTTP 测试；返回客户端的 endpoint 必须根据 MINDFS_CLOUD_PUBLIC_URL 生成 ws 或 wss。生产 HTTPS/WSS 部署模板属于 relay-deployment-baseline。

7. **绑定确认页属于 Cloud Relay 自身**
   /bind 提供最小服务器页面和登录/确认交互，不复用或修改 web/。页面只是现有客户端跳转目标，不发展为管理前端。

### 假设

- MINDFS_CLOUD_PUBLIC_URL 是管理员配置的可信绝对 URL，不根据未经信任的 Host Header 签发 Connector endpoint。
- bootstrap 管理员登录 Session 默认有效 12 小时；具体时长可配置。
- Binding Challenge 默认有效 10 分钟。
- 单条 WebSocket data frame 默认最大 32 MiB，超过时关闭公网 WebSocket 并返回 close code 1009。
- Gateway 打开 stream 默认等待 10 秒，读取节点 response headers 默认等待 30 秒；流式 body 不设置整个请求总时长。

## 2. 名词与编排

### 2.1 名词层

#### 现状

现有仓库只有 Cloud Relay 的消费者：

- server/internal/relay/service.go:43-62 定义客户端接受的 bind confirmed JSON。
- server/internal/relay/device.go 定义稳定 X-MindFS-Device-ID。
- server/internal/relay/credentials.go:14-28 定义客户端持久化的 device_token、node_id、node_name、endpoint。
- server/internal/relay/service.go:331-377 定义 Connector Bearer WebSocket 和 Node 作为 yamux.Client。
- server/internal/relay/wsconn.go:160-243 定义 WS data/close frame。

Cloud Relay 本身无现状，是全新、独立模块。

#### 变化

新增以下 V0 实体和值对象：

~~~go
type CloudConfig struct {
    Addr              string
    PublicURL         string
    DataDir           string
    AdminUsername     string
    AdminPassword     Secret
    TokenKey          [32]byte
    BindTTL           time.Duration
    AdminSessionTTL   time.Duration
    StreamOpenTimeout time.Duration
    HeaderTimeout     time.Duration
    MaxWSMessageBytes int64
}

type BindStatus string

const (
    BindPending   BindStatus = "pending"
    BindConfirmed BindStatus = "confirmed"
    BindClaimed   BindStatus = "claimed"
    BindExpired   BindStatus = "expired"
    BindRevoked   BindStatus = "revoked"
)

type BindChallenge struct {
    CodeHash               []byte
    DeviceID               string
    RequestedNodeName      string
    RootHint               string
    Status                 BindStatus
    NodeID                 string
    TokenDerivationVersion int
    ExpiresAt              time.Time
    ConfirmedAt            *time.Time
}

type Node struct {
    ID         string
    DeviceID   string
    Name       string
    Status     string
    AccessMode string
    CreatedAt  time.Time
    LastSeenAt *time.Time
}

type DeviceTokenRecord struct {
    ID        string
    NodeID    string
    TokenHash []byte
    Status    string
    CreatedAt time.Time
}

type NodePresence struct {
    NodeID       string
    ConnectionID string
    Online       bool
    ConnectedAt  time.Time
}
~~~

核心模块契约：

~~~go
type BindingService interface {
    Poll(ctx context.Context, code, deviceID string) (BindPollResponse, error)
    Status(ctx context.Context, code string) (BindPageStatus, error)
    Confirm(ctx context.Context, code, nodeName string) (BindConfirmation, error)
    Revoke(ctx context.Context, code string) error
}

type DeviceTokenService interface {
    Derive(challenge BindChallenge) (plainToken string, tokenHash []byte, err error)
    Authenticate(ctx context.Context, plainToken string) (Node, error)
}

type SessionRegistry interface {
    Register(nodeID, connectionID string, session RelaySession) (replaced RelaySession)
    Unregister(nodeID, connectionID string)
    OpenStream(ctx context.Context, nodeID string) (net.Conn, error)
    Status(nodeID string) NodePresence
}
~~~

绑定接口示例，来源为 server/internal/relay/service.go:180-223：

~~~http
GET /api/bind/poll?code=pc_live
X-MindFS-Device-ID: md_device

200
{"status":"pending","next_poll_after_ms":3000}
~~~

确认后同一设备重复轮询：

~~~http
200
{
  "status":"confirmed",
  "device_token":"dt_same_value",
  "node_id":"nabc...",
  "node_name":"Office Mac",
  "endpoint":"wss://cloud.example.com/ws/connector"
}
~~~

不同 device ID 使用同一已绑定 code：

~~~http
200
{"status":"claimed"}
~~~

Connector 错误示例，来源为 server/internal/relay/service.go:699-738 和 service_test.go:547-574：

~~~http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"error":"device_token_invalid"}
~~~

管理员登录与绑定页内部接口：

~~~http
POST /api/cloud/v1/auth/login
Content-Type: application/json

{"username":"admin","password":"configured-secret"}

200
{"user":{"id":"usr_bootstrap","tenant_id":"ten_bootstrap","username":"admin","roles":["owner"]},"csrf_token":"csrf_xxx"}
Set-Cookie: mindfs_cloud_session=...; HttpOnly; Secure; SameSite=Lax
~~~

凭据错误返回 401 auth_required，不返回“用户名不存在”或“密码错误”的差异信息。

~~~http
GET /api/bind/status?code=pc_live

200
{"status":"pending","device_seen":true,"node_name":"Office Mac"}
~~~

该接口只供 /bind 页面使用，要求有效管理员 Session。页面早于 Node poll 打开时返回 waiting_for_device。

~~~http
POST /api/bind/confirm
X-CSRF-Token: csrf_xxx
Content-Type: application/json

{"code":"pc_live","action":"confirm","node_name":"Office Mac"}

200
{"status":"confirmed","node_id":"nabc...","node_url":"https://cloud.example.com/n/nabc.../"}
~~~

action=reject 时把 challenge 置为 revoked；无 Session 返回 401 auth_required，CSRF 无效返回 403 access_denied。

V0 SQLite schema 只包含：

~~~text
admin_sessions
- session_hash, csrf_hash, expires_at, created_at, last_seen_at

bind_challenges
- code_hash, device_id, requested_node_name, root_hint
- status, node_id, token_derivation_version
- expires_at, created_at, confirmed_at

nodes
- id, device_id, name, status, access_mode
- created_at, last_seen_at

device_tokens
- id, node_id, token_hash, status, created_at, last_used_at
~~~

不预建 V1/V2/V3 空表。

### 2.2 编排层

~~~mermaid
sequenceDiagram
    participant Admin as Admin Browser
    participant Cloud as Cloud Relay
    participant DB as SQLite
    participant Node as MindFS Node
    participant Remote as Remote Browser

    Node->>Cloud: GET /api/bind/poll + X-MindFS-Device-ID
    Cloud->>DB: observe/upsert pending challenge
    Cloud-->>Node: pending
    Admin->>Cloud: GET /bind?code=... + login
    Admin->>Cloud: POST /api/bind/confirm
    Cloud->>DB: create node + token hash + confirm challenge
    Node->>Cloud: repeat bind poll
    Cloud->>Cloud: deterministically derive same device token
    Cloud-->>Node: confirmed credentials
    Node->>Cloud: GET /ws/connector + Bearer token
    Cloud->>DB: lookup token hash and node
    Cloud->>Cloud: create yamux.Server and register session
    Remote->>Cloud: /n/{nodeId}/api or /ws
    Cloud->>Cloud: open yamux stream and strip node prefix
    Cloud->>Node: raw HTTP request
    Node-->>Cloud: HTTP response or framed WS messages
    Cloud-->>Remote: streamed HTTP response or WebSocket
~~~

#### 现状

现有 Node 的控制流是：

1. server/internal/relay/manager.go:127-147 生成 pending code 并启动轮询。
2. manager.go:336-383 处理 pending、confirmed、claimed、expired、revoked。
3. service.go:331-377 以 Bearer Token 连接 endpoint，并把 WebSocket 包成 net.Conn 后创建 yamux.Client。
4. service.go:379-397 接收 Cloud 打开的 stream，将开头解析为 HTTP request。
5. service.go:416-495 将普通 HTTP 或 WebSocket 转给本地 MindFS。

浏览器通过 web/src/services/base.ts:3-62 自动为 HTTP/WS 添加 /n/{nodeId} 前缀；E2EE proof 在 web/src/services/e2ee.ts:607-615 主动去掉该前缀。

#### 变化

Cloud Relay 新增五段编排：

1. **启动**
   校验配置和 Token Key，打开 SQLite，执行 V0 schema，创建模块并注册路由。配置或数据库不可用时进程启动失败，不以降级模式继续。

2. **绑定**
   Node 首次 poll 原子观察 challenge；/bind 页面要求管理员 Session；确认事务同时创建 Node、DeviceToken hash 并把 challenge 标为 confirmed。相同 code/device ID 的后续 poll 重新派生同一 Token。

3. **Connector**
   握手前验证 Bearer Token hash；升级后包装 WebSocket 并创建 yamux.Server。注册新 session 时原子替换旧 session；旧连接关闭但旧连接的延迟 Unregister 不得删除新 session。

4. **HTTP Gateway**
   根据 node ID 查 Node 与在线 session，打开 stream，去除公网前缀，清洗内部 Header，设置 X-MindFS-Relayed、X-Forwarded-Host 和 X-Forwarded-Proto，然后流式写入 request、读取 response。

5. **WebSocket Gateway**
   先通过 yamux 向 Node 发原始 Upgrade request并读取 Node 101；只有 Node 接受后才升级公网侧。双方升级成功后，在公网 WebSocket 消息与 roadmap 定义的 data/close frame 之间双向桥接。

#### 流程级约束

- **绑定幂等**：同一 code/device ID 在 confirmed 有效期内返回完全相同的 node ID、Token、名称和 endpoint。
- **绑定归属**：challenge 首次观察后绑定 device ID；其他 device ID 只得到 claimed，不泄露节点信息。
- **事务边界**：确认绑定的 Node、Token hash 和 challenge 状态必须同一事务提交，禁止部分成功。
- **并发连接**：每个 node ID 一个 active session；新连接优先，旧连接清理使用 connection ID 做 compare-and-delete。
- **断线语义**：Connector 断开后新请求返回 503 node_offline；已打开 stream 随 session 关闭而失败。
- **HTTP 流式性**：不缓冲完整 request/response body；客户端取消时关闭对应 yamux stream。
- **Header 边界**：外部 X-MindFS-Relayed 和 X-MindFS-Relay-Service-Slug 必须先删除；E2EE headers 必须保持值不变。
- **WebSocket 顺序**：单方向消息顺序保持；任一方向结束后只发送一次 close 并释放 stream。
- **Token 安全**：只在 confirmed JSON 中返回明文；日志和数据库不得出现明文、Token Key 或 Authorization。
- **可观测点**：记录 request ID、node ID、binding 状态变化、Connector 上下线、stream 类型、耗时和错误码；不记录 URL query 中的敏感值、body 或 WS payload。
- **扩展点**：持久化通过 Store 接口、在线连接通过 SessionRegistry；后续 PostgreSQL/Redis Feature 替换实现，不改变客户端 API。

### 2.3 挂载点清单

1. **独立构建入口：cloud/go.mod 与 cloud/cmd/mindfs-relay** — 新增，可独立构建和删除。
2. **Cloud HTTP 路由表** — 新增 /bind、/api/cloud/v1/auth/login、/api/bind/poll、/api/bind/status、/api/bind/confirm、/ws/connector、/n/{nodeId}/*、/healthz。
3. **Cloud 配置键** — 新增 MINDFS_CLOUD_ADDR、MINDFS_CLOUD_PUBLIC_URL、MINDFS_CLOUD_DATA_DIR、MINDFS_CLOUD_ADMIN_USERNAME、MINDFS_CLOUD_ADMIN_PASSWORD、MINDFS_CLOUD_TOKEN_KEY。
4. **Cloud SQLite schema** — 新增 admin_sessions、bind_challenges、nodes、device_tokens。
5. **外部运行配置** — 运行未修改 MindFS 时将 MINDFS_RELAY_BASE_URL 指向 Cloud Relay。

删除以上五项后，本 feature 在系统和用户视角完全消失；现有 MindFS 源码无需回滚。

### 2.4 推进策略

1. **编排骨架**：建立独立 module、配置加载、Store/Service 接口和路由，业务节点先返回协议兼容 stub。
   退出信号：cloud 可独立构建启动，healthz 成功，全部目标路由可达且未修改根 module。

2. **绑定与凭据计算节点**：接通管理员 Session、challenge 状态机、HMAC Token 派生和确认事务。
   退出信号：pending → confirmed → claimed/expired/revoked 场景可观察，confirmed 重试返回相同 Token。

3. **Connector 计算节点**：接通 Bearer 鉴权、WebSocket net.Conn、yamux.Server 和 SessionRegistry。
   退出信号：合法 Node 上线、无效 Token 返回指定 401、新连接可靠替换旧连接。

4. **HTTP Gateway 计算节点**：接通 node 路由、stream 打开、路径/Header 改写和流式 response。
   退出信号：普通 HTTP、静态资源、API body 和错误响应均能经 yamux 往返。

5. **WebSocket Gateway 计算节点**：接通 101 协调、子协议和 data/close framing 双向桥接。
   退出信号：text、binary、正常 close、异常 close 和超限消息均符合契约。

6. **持久化与生命周期**：完成 SQLite repository、启动恢复、challenge 过期和 Connector/stream 清理。
   退出信号：Cloud 重启后绑定数据仍在，Node 重连恢复，过期和断线无残留 session。

7. **Feature 内协议验证**：覆盖名词、编排、错误、并发和只读边界。
   退出信号：第 3 节全部场景有自动或手工证据，git 变更只包含 cloud/** 与 .codestable/**。

### 2.5 结构健康度与微重构

#### 评估

- 文件级 — 现有源码：本 feature 不修改任何现有源码文件，因此不存在待改胖文件、职责混杂或高密度插入点。
- 目录级 — cloud/：当前不存在，是全新独立 module；按 roadmap 预先划分 config、binding、connector、gateway、store 等职责，不会向已有摊平目录追加文件。
- Compound convention 检索：未找到目录组织或命名 convention；现有 self-hosted Relay explore 只提供协议证据，不规定代码目录。

#### 结论：不做微重构

原因：没有既有 cloud 代码可搬移；任何对 server/internal/relay 的拆分或抽共用都会违反现有代码只读边界。实现直接在新 module 建立清晰职责，不做“先搬再加”的前置动作。

## 3. 验收契约

### 正常场景

1. 配置完整且 SQLite 可写 → Cloud Relay 启动成功，/healthz 返回 200。
2. 未绑定 Node 首次 poll 合法 code 和 device ID → 返回 pending 与正数 next_poll_after_ms。
3. 管理员登录并确认 challenge → 同一 Node 下次 poll 获得客户端可解析的 confirmed JSON。
4. 同一 code/device ID 重复 poll → device_token、node_id、node_name、endpoint 完全一致。
5. Node 使用 confirmed Token 建立 Connector → SessionRegistry 显示 online，并可接受 Cloud 打开的 yamux stream。
6. 浏览器访问 /n/{nodeId}/ → Node 收到去前缀的 / 请求并返回 MindFS 页面。
7. 浏览器访问 /n/{nodeId}/api/... 并发送 request body → Node 收到相同 method、query 和 body，浏览器流式收到响应。
8. 浏览器访问 /n/{nodeId}/ws → WebSocket text、binary 和 close 在公网与 Node 之间双向保持顺序。
9. 带 E2EE headers 的 HTTP/WS 请求 → Node 收到原值，Cloud 日志中没有密文 payload，canonical path 不含 /n/{nodeId}。
10. Cloud 重启且 Node 自动重连 → 不重新绑定即可恢复公网访问。

### 边界场景

11. /bind 页面先于 Node 首次 poll 打开 → 页面保持等待状态，不自动确认或创建可领取凭据。
12. 同一 code 被不同 device ID 轮询 → 返回 claimed，响应不含 Token、node ID 或 endpoint。
13. challenge 超过 10 分钟未确认 → 返回 expired，之后不能确认。
14. 管理员拒绝 challenge → Node poll 返回 revoked。
15. 同一 node ID 建立第二条 Connector → 新连接成为 active，旧连接关闭，旧连接清理不影响新连接。
16. 外部请求伪造 X-MindFS-Relayed 或 X-MindFS-Relay-Service-Slug → Node 只看到 Cloud 生成的合法内部 Header。
17. Public Node Route 恰好为 /n/{nodeId} 或 /n/{nodeId}/ → Node 路径统一为 /。
18. WebSocket data frame 超过 32 MiB → 公网连接以 1009 关闭，Cloud 释放对应 stream。
19. 公网 HTTP 客户端中途取消 → 对应 yamux stream 关闭，其他 stream 和 Connector 保持可用。

### 错误场景

20. MINDFS_CLOUD_TOKEN_KEY 缺失、长度错误或无法解码 → 进程拒绝启动且不监听公网端口。
21. SQLite 无法打开或迁移失败 → 进程拒绝启动，不使用内存降级。
22. 管理员用户名或密码错误 → 返回 401 auth_required，不创建 Session。
23. /api/bind/confirm 缺少有效 Session 或 CSRF → 分别返回 401 auth_required 或 403 access_denied，challenge 状态不变。
24. Connector 缺少或携带无效 Bearer Token → 握手返回 401 和 {"error":"device_token_invalid"}。
25. node ID 不存在 → Gateway 返回 404 node_not_found。
26. node 已绑定但 Connector 不在线 → Gateway 返回 503 node_offline。
27. stream 打开超时 → Gateway 返回 504 node_timeout。
28. Node 对 WebSocket Upgrade 返回非 101 → Cloud 将该 HTTP 状态和 body 返回公网客户端，不建立公网 WebSocket。
29. Connector 发送非 binary transport message → Cloud 关闭 Connector，节点变为 offline。
30. WS stream 出现未知 frame type 或非法 opcode → 公网连接以 1002 关闭并释放 stream。

### 明确不做的反向核对

31. git diff 不应出现 server/、web/、cli/、android/、harmony/、根 go.mod、Makefile 或 agents.json。
32. cloud/go.mod 不应 replace 或 import mindfs/server/internal 下的包。
33. Cloud 路由中不应出现 Token Station、/api/agents、/api/tips、版本下载或本地附加服务 API。
34. SQLite schema 不应出现 tenants、shares、quotas、billing、hosted agents 或 release tables。
35. 日志和数据库搜索不应找到实际 device token、Token Key、Authorization、E2EE body 或 WebSocket payload。
36. 本 feature 不应新增 Dockerfile、生产反向代理模板、PostgreSQL 或 Redis 依赖。

## 4. 与项目级架构文档的关系

本 feature 完成后将产生第一个真实 Cloud Relay 子系统。Acceptance 阶段应：

- 新增 architecture/cloud-relay-core.md，记录独立 module、Binding、Connector、Gateway、SQLite 和内存 Session Registry 的现状。
- 在 ARCHITECTURE.md 增加 Cloud Relay 子系统入口，并明确现有 MindFS Node 仍是只读上游。
- 将 Binding Challenge、Node、Device Token、Relay Session 和 Gateway Stream 提炼为系统级名词。
- 将 Node 主动 Connector、Cloud 打开 yamux stream、HTTP/WS 反向转发提炼为跨模块动词骨架。
- 记录确定性 HMAC Token、一个 Node 一个 active session、E2EE 透明传输和现有代码只读为稳定约束。
- 将 requirement mindfs-compatible-cloud-backend 的 implemented_by 指向 cloud-relay-core，并在验收通过后评估 draft → current。

Design 阶段不修改 architecture；上述动作由 acceptance 按实际实现落档。
