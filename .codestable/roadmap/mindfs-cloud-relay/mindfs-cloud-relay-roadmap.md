---
doc_type: roadmap
slug: mindfs-cloud-relay
status: active
created: 2026-08-03
last_reviewed: 2026-08-03
tags: [mindfs, cloud, relay, yamux, websocket, self-hosted, backend]
related_requirements: [mindfs-compatible-cloud-backend]
related_architecture: [cloud-relay-core]
---

# MindFS 兼容云后端

## 1. 背景

MindFS 仓库包含本地节点、Web 前端、CLI 和 Relay 客户端，但不包含官方云端实现。现有客户端已经公开了绑定、设备凭据、Connector WebSocket、yamux、HTTP/WebSocket 转发、Token Station、托管 Agent 配置、本地服务域名、Tips 和版本分发等云端协议。

本 roadmap 在仓库中新增一个完全独立的 cloud Go module，实现兼容 MINDFS_RELAY_BASE_URL 的后端。目标不是改造 MindFS，也不是让客户端适配新后端，而是让新后端持续兼容未修改的现有客户端。

第一版本只交付远程访问的最小闭环，但完整规划保留生产化、多租户、Token Station 和内容分发能力，避免 V0 形成无法演进的临时代码。

## 2. 范围与明确不做

### 本 roadmap 覆盖

- cloud 独立 Go module、配置、迁移、部署和健康检查。
- 管理员及后续多用户认证后端。
- 设备绑定、节点注册、凭据签发、轮换和撤销。
- Connector WebSocket、WebSocket-as-net.Conn 和 yamux Server。
- /n/{nodeId} HTTP/WebSocket Relay。
- E2EE headers、密文 body 和 WebSocket ciphertext 透明转发。
- 节点管理、共享、访问策略和配额后端 API。
- 本地服务注册、wildcard 域名和自定义域名。
- PostgreSQL、Redis、多实例 Connector 路由。
- Token Station 余额、API Key、模型网关和充值后端。
- /api/agents、/api/tips、移动端版本和下载制品接口。
- 审计、指标、追踪、告警、备份和 API 生命周期。

### 现有代码只读硬约束

以下路径和文件永久视为上游只读内容：

~~~text
server/
web/
cli/
android/
harmony/
plugins/
scripts/
go.mod
go.sum
Makefile
agents.json
config.json
README.md
README.zh.md
~~~

所有实现代码只能写入：

~~~text
cloud/**
~~~

规划与流程文档只能写入：

~~~text
.codestable/**
~~~

进一步约束：

- cloud 使用独立 cloud/go.mod，不引用或修改根 go.mod。
- cloud 不 import mindfs/server/internal 下的任何包。
- 不在现有 server 或 web 目录增加兼容测试。
- 兼容测试从 cloud 侧编译或启动未修改的 MindFS 客户端做黑盒测试。
- 后续 git pull 更新上游代码；上游协议变化时只修改 cloud 跟进。
- 如果客户端存在无法由后端兼容的问题，记录观察项并由用户决定，默认不修客户端。
- 管理能力默认只提供后端 API，不开发或修改现有 MindFS 前端。

### 明确不做

- 不重写本地 Agent、会话、文件、Git、任务或 E2EE 实现。
- Relay 不参与浏览器与节点间的 E2EE 密钥协商或解密。
- 不在 Relay 内运行模型推理；Token Station 只代理外部模型供应商。
- 不自行实现银行卡、微信、支付宝等支付网络，只定义支付适配器。
- 不负责 Android/Harmony 应用签名私钥和应用商店发布。
- 不复制官方私有账号数据、计费策略和运营后台行为。
- V0 不支持多用户、横向扩容、Token Station 和 wildcard 本地服务域名。

## 3. 模块拆分（概设）

~~~text
cloud/
├── go.mod
├── go.sum
├── cmd/
│   └── mindfs-relay/
├── app/
└── internal/
    ├── config
    ├── identity
    ├── binding
    ├── nodes
    ├── connector
    ├── gateway
    ├── services
    ├── tokenstation
    ├── content
    ├── store
    └── ops
~~~

### Bootstrap 与 Config

- **职责**：独立进程启动、配置校验、模块装配、迁移、健康检查和优雅关闭。
- **承载的子 feature**：relay-core-single-instance、relay-deployment-baseline。
- **触碰的现有代码**：无，只新增 cloud。

### Identity

- **职责**：管理员、用户、租户、登录会话、OIDC 和 RBAC；不处理节点数据转发。
- **承载的子 feature**：cloud-account-tenancy、cloud-node-sharing。
- **触碰的现有代码**：无。

### Binding 与 Node Registry

- **职责**：pending code、设备身份、节点、凭据和节点生命周期；不持有活跃网络连接。
- **承载的子 feature**：relay-core-single-instance、cloud-node-management、relay-security-audit。
- **触碰的现有代码**：无。

### Connector

- **职责**：验证 device token、接受 Connector WebSocket、运行 yamux.Server 和管理活跃连接。
- **承载的子 feature**：relay-core-single-instance、relay-distributed-routing。
- **触碰的现有代码**：无。

### Gateway

- **职责**：/n/{nodeId} HTTP/WS 转发、路径改写、E2EE 透明传输、超时和流量统计。
- **承载的子 feature**：relay-core-single-instance、relay-security-audit、relay-usage-quotas。
- **触碰的现有代码**：无。

### Local Services 与 Domains

- **职责**：附加服务注册、wildcard hostname 解析和自定义域名。
- **承载的子 feature**：relay-local-service-domains、relay-custom-domains。
- **触碰的现有代码**：无。

### Management API

- **职责**：节点管理、共享、在线状态、运营管理和审计查询 API；不提供新的 MindFS 前端。
- **承载的子 feature**：cloud-node-management、cloud-node-sharing、cloud-operations-observability。
- **触碰的现有代码**：无。

### Token Station

- **职责**：余额、额度台账、API Key、模型供应商代理和充值后端。
- **承载的子 feature**：token-station-account-ledger、token-station-model-gateway、token-station-billing。
- **触碰的现有代码**：无。

### Hosted Content

- **职责**：托管 Agent 配置、Tips、移动端版本和制品元数据。
- **承载的子 feature**：hosted-agent-config、cloud-tips-content、release-distribution。
- **触碰的现有代码**：无。

### Store 与 Operations

- **职责**：SQLite/PostgreSQL、Redis、多实例路由、备份、指标、告警和 API 生命周期。
- **承载的子 feature**：生产化和运维类子 feature。
- **触碰的现有代码**：无。

## 4. 模块间接口契约 / 共享协议

本节是所有后续 feature-design 的硬约束。若现有客户端源码和本文发生冲突，以客户端可观察协议为准，并先通过 roadmap update 修订本文。

### 4.1 标识符与错误格式

~~~text
binding code:  pc_{base64url-random}
device token:  dt_{base64url-32-bytes}
node id:       n{lowercase-base32-random}
station token: st_{base64url-32-bytes}
api key:       sk-{base64url-random}
request id:    rq_{base64url-random}
~~~

node_id 必须同时满足 URL path 和 DNS label 安全要求。所有 Token 只存 hash。

#### Device Token 派生与幂等重放

device token 使用确定性 HMAC 派生，不生成需要恢复的随机明文：

~~~text
context = "mindfs-device-token:v1"
message = context || 0x00 || bind_code_hash || 0x00 || device_id || 0x00 || node_id
raw_token = HMAC-SHA256(token_key, message)
device_token = "dt_" + base64url_no_padding(raw_token)
token_hash = SHA-256(UTF-8(device_token))
~~~

约束：

- token_key 来自独立配置 MINDFS_CLOUD_TOKEN_KEY，不得复用管理员密码、Session Key 或内部实例鉴权密钥。
- MINDFS_CLOUD_TOKEN_KEY 必须是 base64url 编码的 32 字节随机值；缺失或格式错误时服务拒绝启动。
- bind_challenge 保存 token_derivation_version、bind_code_hash、device_id 和 node_id，不保存 device token 明文或可逆密文。
- 同一 code 与 device_id 在 confirmed 有效期内重复轮询时，服务重新派生并返回完全相同的 device token。
- Connector 鉴权只计算请求 token_hash 并查询 device_tokens；鉴权不依赖 token_key，因此现有已签发 token 在 token_key 变更后仍可验证。
- token_key 变更会影响尚未 claimed 的 confirmed challenge 重放。V0 不提供在线轮换；运维必须在没有待领取 confirmed challenge 时更换。
- device token、raw_token、token_key 和完整 Authorization header 不得写入日志。

cloud 自身产生的错误统一为：

~~~json
{
  "error": "node_offline",
  "message": "node is not connected",
  "request_id": "rq_xxx"
}
~~~

固定错误码：

~~~text
invalid_request
auth_required
access_denied
invalid_bind_code
bind_claimed
bind_expired
bind_revoked
device_token_invalid
node_not_found
node_offline
node_disabled
service_not_found
service_disabled
rate_limited
quota_exceeded
relay_stream_failed
node_timeout
internal_error
~~~

### 4.2 管理认证

~~~http
POST /api/cloud/v1/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "..."
}
~~~

~~~json
{
  "user": {
    "id": "usr_xxx",
    "tenant_id": "ten_xxx",
    "username": "admin",
    "roles": ["owner"]
  },
  "csrf_token": "..."
}
~~~

使用 mindfs_cloud_session HttpOnly、Secure、SameSite=Lax Cookie；所有管理写请求携带 X-CSRF-Token。V0 只提供 bootstrap 管理员，但沿用同一接口。

### 4.3 绑定协议

现有客户端打开：

~~~http
GET /bind?code=pc_xxx&root=...&node_name=...&purpose=token_station
~~~

/bind 是 cloud 自己提供的最小绑定确认页，不修改或复用 web/ 源码。后续可以替换为外部管理系统调用相同后端 API。

节点轮询：

~~~http
GET /api/bind/poll?code=pc_xxx&purpose=...
X-MindFS-Device-ID: md_xxx
~~~

Pending：

~~~json
{
  "status": "pending",
  "next_poll_after_ms": 3000
}
~~~

确认：

~~~http
POST /api/bind/confirm

{
  "code": "pc_xxx",
  "action": "confirm",
  "node_name": "Office Mac"
}
~~~

Relay confirmed：

~~~json
{
  "status": "confirmed",
  "device_token": "dt_xxx",
  "node_id": "nxxx",
  "node_name": "Office Mac",
  "endpoint": "wss://cloud.example.com/ws/connector"
}
~~~

Token Station confirmed：

~~~json
{
  "status": "confirmed",
  "token_station_token": "st_xxx"
}
~~~

状态机：

~~~text
pending -> confirmed
pending -> revoked
pending -> expired
confirmed -> claimed
~~~

同一 device_id 与 code 的重试必须幂等；不同设备使用已认领 code 返回 claimed；默认 TTL 10 分钟。

### 4.4 Connector

~~~http
GET /ws/connector
Upgrade: websocket
Authorization: Bearer dt_xxx
~~~

Token 无效：

~~~http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"error":"device_token_invalid"}
~~~

成功握手可以返回：

~~~http
X-MindFS-Relay-Node-Name: Office Mac
~~~

约束：

- Connector WebSocket 只接受 binary message。
- WebSocket 包装为连续可靠 net.Conn。
- 节点是 yamux.Client，cloud 必须是 yamux.Server。
- 一个 node ID 只保留一个 active connector，新连接替换旧连接。
- Connector 断开后节点立即标记 offline。
- keepalive 30 秒，presence lease 90 秒。

内部接口：

~~~go
type SessionRegistry interface {
    Register(nodeID, connectionID string, session *yamux.Session) error
    Unregister(nodeID, connectionID string)
    OpenStream(ctx context.Context, nodeID string) (net.Conn, error)
    Status(nodeID string) NodePresence
}
~~~

### 4.5 HTTP Gateway

~~~http
ANY /n/{nodeId}/{path...}
~~~

处理约束：

1. 去掉 /n/{nodeId} 后转发节点真实路径，并保留 query。
2. 删除外部伪造的 X-MindFS-Relayed 和 X-MindFS-Relay-Service-Slug。
3. cloud 设置 X-MindFS-Relayed: 1。
4. 原样保留 X-MindFS-E2EE、X-MindFS-Client-ID、X-MindFS-Proof、X-MindFS-TS 和加密 body。
5. 使用 http.Request.Write 写入 yamux stream，使用 http.ReadResponse 读取响应。
6. body 必须流式转发，不允许整包缓冲。
7. 删除 hop-by-hop headers。
8. E2EE canonical path 使用去掉 Relay 前缀后的路径。

cloud 错误：

~~~text
404 node_not_found
403 node_disabled
429 quota_exceeded
503 node_offline
502 relay_stream_failed
504 node_timeout
~~~

节点响应原样转发，不包装 cloud 错误结构。

### 4.6 WebSocket Gateway

公网 Upgrade request 去掉 node 前缀后写入 yamux stream。节点返回 101 后切换为以下 framing。

Data frame：

~~~text
[frame_type=1:1 byte]
[opcode:1 byte]
[payload_length:uint32 big-endian]
[payload]
~~~

Close frame：

~~~text
[frame_type=2:1 byte]
[close_code:uint16 big-endian]
[reason_length:uint32 big-endian]
[reason]
~~~

约束：

- data opcode 只允许 text 1 或 binary 2。
- 公网 ping/pong 在 Gateway WebSocket 层处理。
- Sec-WebSocket-Protocol 必须同步。
- close code 1005、1006、1015 转成 1000。
- WS ciphertext 不得进入日志。

### 4.7 本地附加服务

~~~http
PUT /api/device/nodes/{nodeId}/services/{slug}
Authorization: Bearer dt_xxx

{
  "name": "Dev Server",
  "enabled": true
}
~~~

~~~http
DELETE /api/device/nodes/{nodeId}/services/{slug}
Authorization: Bearer dt_xxx
~~~

公网域名：

~~~text
https://{slug}-{nodeId}-relay.{baseDomain}/{path}
~~~

转发时添加：

~~~http
X-MindFS-Relay-Service-Slug: {slug}
X-MindFS-Relayed: 1
~~~

slug 必须符合：

~~~regex
^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])$
~~~

### 4.8 后端管理 API 与访问策略

~~~http
GET    /api/cloud/v1/nodes
GET    /api/cloud/v1/nodes/{nodeId}
PATCH  /api/cloud/v1/nodes/{nodeId}
DELETE /api/cloud/v1/nodes/{nodeId}
POST   /api/cloud/v1/nodes/{nodeId}/tokens/rotate
POST   /api/cloud/v1/nodes/{nodeId}/shares
DELETE /api/cloud/v1/nodes/{nodeId}/shares/{shareId}
~~~

访问模式：

~~~text
node_auth      本地 MindFS 认证/E2EE 保护，cloud 不要求登录
cloud_account  Gateway 前必须登录并通过节点 ACL
disabled       所有公网访问返回 403
~~~

V0 只实现 node_auth 和 disabled。删除节点必须撤销 Token、关闭 Connector 并禁用所有服务域名。

### 4.9 核心数据结构

~~~text
tenants
- id, slug, name, status, created_at

users
- id, tenant_id, username, password_hash, status, created_at

cloud_sessions
- id_hash, user_id, csrf_hash, expires_at, last_seen_at

bind_challenges
- code_hash, purpose, device_id, requested_node_name
- status, expires_at, claimed_by_user_id
- node_id, issued_token_id, token_derivation_version
- created_at, confirmed_at

nodes
- id, tenant_id, owner_user_id, device_id
- name, status, access_mode, created_at, last_seen_at

device_tokens
- id, node_id, token_hash, status
- created_at, last_used_at, revoked_at

node_services
- node_id, slug, name, enabled, created_at, updated_at

node_shares
- id, node_id, subject_type, subject_id, role, expires_at

audit_events
- id, tenant_id, actor_type, actor_id, action
- resource_type, resource_id, request_id, metadata_json, created_at

usage_counters
- tenant_id, node_id, period
- ingress_bytes, egress_bytes, streams, websocket_seconds

station_accounts
station_ledger
station_api_keys
hosted_agent_revisions
tips
releases
release_artifacts
custom_domains
~~~

### 4.10 多实例路由

Redis presence：

~~~json
{
  "instance_id": "relay-01",
  "connection_id": "conn_xxx",
  "expires_at": "RFC3339"
}
~~~

Key 为 relay:node:{nodeId}，TTL 90 秒。

非 owner 实例打开远端 stream：

~~~http
GET /internal/v1/nodes/{nodeId}/streams
Upgrade: websocket
X-MindFS-Instance-ID: relay-02
Authorization: Bearer {internal-secret}
~~~

内部 WebSocket 只传二进制字节流；owner 为每条内部连接打开一条本地 yamux stream 并双向复制。

### 4.11 Token Station

~~~http
GET /api/token-station/userinfo?purpose=balance|apply
Authorization: Bearer dt_xxx|st_xxx
~~~

~~~json
{
  "success": true,
  "data": {
    "quota": 100,
    "used_quota": 12.5,
    "balance": 87.5,
    "balance_text": "87.50",
    "used_quota_text": "12.50",
    "quota_display_text": "87.50",
    "topup_url": "https://cloud.example.com/token-station",
    "api_keys": [
      {
        "id": 1,
        "name": "MindFS Cloud",
        "api_key": "sk-xxx",
        "status": 1,
        "group": "default",
        "created_at": 1785686400
      }
    ]
  }
}
~~~

模型网关：

~~~text
POST /token-station/v1/chat/completions
POST /token-station/v1/responses
POST /token-station/v1/messages
GET  /token-station/v1/models
POST /token-station/v1beta/models/{model}:generateContent
GET  /token-station/wallet
~~~

流式响应原样传递，使用量入账必须幂等，余额不足返回 402 quota_exceeded，日志不得记录 prompt、response 或 API Key。

### 4.12 托管内容

~~~http
GET /api/agents
GET /api/tips?node_id={nodeId}
GET /api/versions/android
GET /api/versions/harmony
~~~

/api/agents 必须兼容现有 agents.json，最大 2 MiB，并支持 ETag。客户端当前不携带云端用户凭据，因此只能返回公共配置。

/api/tips 返回现有 Tip 字段的单条对象或数组，不承载敏感内容。

版本响应：

~~~json
{
  "version": "0.5.0",
  "notes": "...",
  "downloads": [
    {
      "os": "android",
      "arch": "arm64",
      "filename": "mindfs.apk",
      "url": "https://cloud.example.com/mindfs-downloads/mindfs.apk"
    }
  ]
}
~~~

制品必须记录 SHA-256、大小、签名元数据和发布时间。

### 4.13 独立模块与部署契约

cloud 独立构建：

~~~text
cd cloud
go test ./...
go build ./cmd/mindfs-relay
~~~

不得从根模块执行 go get 或修改根 go.mod。

固定配置：

~~~text
MINDFS_CLOUD_ADDR
MINDFS_CLOUD_PUBLIC_URL
MINDFS_CLOUD_DATA_DIR
MINDFS_CLOUD_ADMIN_USERNAME
MINDFS_CLOUD_ADMIN_PASSWORD
MINDFS_CLOUD_TOKEN_KEY
MINDFS_CLOUD_ASSETS_DIR
MINDFS_CLOUD_BASE_DOMAIN
MINDFS_CLOUD_DATABASE_URL
MINDFS_CLOUD_REDIS_URL
MINDFS_CLOUD_INTERNAL_SECRET
MINDFS_CLOUD_TRUSTED_PROXIES
~~~

健康接口：

~~~text
GET /healthz
GET /readyz
GET /metrics
~~~

V0 deployment 必须把当前 checkout 的 `web/dist` 作为只读资源放入 `MINDFS_CLOUD_ASSETS_DIR`，并由 Cloud 暴露：

~~~text
GET /mindfs-assets/{path}
~~~

该路由只提供 `web/dist/assets/` 下的文件，拒绝目录遍历和不存在文件。容器构建可以读取并构建现有 `web/`，但不得修改或向 `web/` 写入产物；构建产物只能进入容器层或 `cloud/**` 下的临时输出。

### 4.14 黑盒兼容验收契约

兼容测试只能从 cloud 侧进行：

1. 构建或使用未修改的 MindFS 客户端。
2. 通过 MINDFS_RELAY_BASE_URL 指向测试 cloud。
3. 调用现有 /api/relay/bind/start 获得 code。
4. 在 cloud 侧确认绑定。
5. 等待现有客户端建立 Connector。
6. 从公网入口验证静态页面、HTTP API、Agent WebSocket、断线重连和 E2EE。
7. 测试不得写入 server、web、cli 或根模块文件。

## 5. 子 feature 清单

### V0：核心兼容后端

1. **relay-core-single-instance** — 在独立 cloud module 中实现单实例绑定、Connector、yamux、HTTP/WS 和 E2EE 透传。
   - 所属模块：Bootstrap、Binding、Connector、Gateway、Store
   - 依赖：无
   - 状态：done
   - 对应 feature：2026-08-03-relay-core-single-instance

2. **relay-compatibility-suite** — 从 cloud 侧对未修改客户端建立绑定、HTTP、WS、重连和 E2EE 黑盒测试。
   - 所属模块：跨模块
   - 依赖：relay-core-single-instance，因为需要可运行的数据面
   - 状态：done
   - 对应 feature：2026-08-03-relay-compatibility-suite

3. **relay-deployment-baseline** — 提供 cloud 独立容器、配置校验、迁移、健康检查、TLS 反代示例和 SQLite 备份。
   - 所属模块：Bootstrap、Store、Operations
   - 依赖：relay-core-single-instance、relay-compatibility-suite
   - 状态：done
   - 对应 feature：2026-08-03-relay-deployment-baseline

### V1：完整自托管 Relay 后端

4. **cloud-node-management** — 提供节点列表、在线状态、重命名、Token 轮换、撤销和删除 API。
5. **relay-security-audit** — 完善认证、CSRF、限流、请求上限、Token 安全、审计和日志清洗。
6. **relay-local-service-domains** — 实现本地服务注册、wildcard DNS/TLS 和 service hostname 转发。

### V2：多用户生产平台后端

7. **cloud-account-tenancy** — 实现用户、租户、OIDC、本地登录和 RBAC API。
8. **cloud-node-sharing** — 实现节点共享、邀请、ACL 和访问模式 API。
9. **relay-postgres-control-plane** — 将控制面持久化迁移至 PostgreSQL。
10. **relay-distributed-routing** — 实现 Redis presence、多实例 Connector owner 和内部 stream 转发。
11. **relay-usage-quotas** — 实现流量、连接、节点数、并发和租户配额。
12. **cloud-operations-observability** — 提供指标、追踪、告警、SLO 和运营管理 API，不开发管理前端。
13. **relay-custom-domains** — 实现节点及附加服务自定义域名、验证和证书生命周期。

### V3：完整云生态后端

14. **token-station-account-ledger** — 实现账户、余额、额度、台账和 API Key 生命周期。
15. **token-station-model-gateway** — 实现 OpenAI、Anthropic、Gemini 兼容模型网关和流式计量。
16. **token-station-billing** — 实现钱包、充值订单、支付适配器和幂等回调。
17. **hosted-agent-config** — 实现配置修订、发布、回滚和 /api/agents。
18. **cloud-tips-content** — 实现 Tips 内容存储、投放规则和 /api/tips。
19. **release-distribution** — 实现移动版本、制品元数据、校验和和下载镜像。
20. **cloud-backup-recovery** — 实现数据库、Redis 元数据、内容和制品的备份恢复。
21. **cloud-api-lifecycle** — 建立客户端兼容矩阵、API 版本、弃用窗口和跨版本回归测试。

**最小闭环**：relay-core-single-instance 完成后，未修改的 MindFS 客户端能够通过 MINDFS_RELAY_BASE_URL 完成绑定、建立 Connector，并从 cloud 的 /n/{nodeId} 入口正常使用 HTTP 和 WebSocket。

**V0 状态**：核心实现、真实客户端兼容套件和部署基线均已完成；V0 核心兼容后端闭环结束。

## 6. 排期思路

V0 采用跨模块垂直切片，因为只做绑定或 Connector 都不能产生可验证价值。第一条必须直接跑通未修改客户端。第二条把兼容性固化成测试，第三条才形成可重复部署版本。

V1 补齐完整自托管 Relay 所需的管理、安全和本地服务能力。V2 再引入账号、PostgreSQL、Redis 和多实例。V3 的 Token Station 与内容分发不阻塞 Relay 核心价值。

所有后续 feature 都必须遵守现有代码只读边界；任何需要修改客户端才能完成的设计必须退回 roadmap review。

## 7. 观察项

- architecture/ARCHITECTURE.md 当前仍是骨架；cloud 真正落地后只能记录 cloud 的现状，不回写或重构既有客户端模块。
- 对应 requirement 为 mindfs-compatible-cloud-backend；V0 acceptance 后应按实际能力将其从 draft 更新为 current。
- 官方 Relay 源码不可见，私有行为只能通过未修改客户端进行兼容验证。
- 仓库使用 AGPL-3.0，云服务通过网络提供时需要遵守相应源码义务。
- web 启动器中的官方 /nodes 地址是硬编码；本 roadmap 不修改它，自建云入口通过 MINDFS_RELAY_BASE_URL、直接 URL 或外部管理系统提供。
- 如果上游未来新增 cloud/ 同名目录，需要由用户决定迁移新后端目录，不能直接覆盖上游文件。

## 8. 变更日志

- 2026-08-03：关联 mindfs-compatible-cloud-backend requirement；补充确定性 HMAC device token 派生、只存 hash、confirmed 幂等重放和独立 MINDFS_CLOUD_TOKEN_KEY 契约。
- 2026-08-03：部署契约补充 MINDFS_CLOUD_ASSETS_DIR 与 /mindfs-assets/{path}，确保未修改 Node 的 release 静态路径重写在自建 Cloud 中可实际加载。
