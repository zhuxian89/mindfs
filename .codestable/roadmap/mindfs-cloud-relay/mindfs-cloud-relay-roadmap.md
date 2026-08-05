---
doc_type: roadmap
slug: mindfs-cloud-relay
status: active
created: 2026-08-03
last_reviewed: 2026-08-05
tags: [mindfs, cloud, relay, yamux, websocket, self-hosted, backend]
related_requirements: [mindfs-compatible-cloud-backend]
related_architecture: [cloud-relay-core]
---

# MindFS 兼容云后端

## 1. 背景

MindFS 仓库包含本地节点、Web 前端、CLI 和 Relay 客户端，但不包含官方云端实现。现有客户端已经公开了绑定、设备凭据、Connector WebSocket、yamux、HTTP/WebSocket 转发、Token Station、托管 Agent 配置、本地服务域名、Tips 和版本分发等云端协议。

本 roadmap 在仓库中新增一个完全独立的 cloud Go module，实现兼容 MINDFS_RELAY_BASE_URL 的后端。目标不是改造 MindFS，也不是让客户端适配新后端，而是让新后端持续兼容未修改的现有客户端。

本 roadmap 以**未修改客户端/节点的可观察协议为唯一需求来源**：客户端或节点实际调用的接口必须兼容（兼容义务）；客户端不调用的能力一律视为可选愿景，不承诺实现。第一版本交付远程访问最小闭环；其后只补齐剩余客户端兼容缺口，不主动创造协议或平台能力。

## 2. 范围与明确不做

### 本 roadmap 覆盖

- cloud 独立 Go module、配置、迁移、部署和健康检查。
- 仅 QQ 邮箱的验证码注册、邮箱密码登录、找回密码、多用户会话和节点归属隔离。
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
- 客户端/节点不调用的能力一律不做（Token 主动轮换、节点共享、tenant/RBAC、Token Station 云端版、自定义域名等均为可选愿景，非兼容义务；详见第 5 节「非兼容愿景」）。
- 账号体系只允许 `@qq.com`：注册使用邮箱验证码和用户自设密码，日常登录使用邮箱和该密码，验证码另用于找回密码；不实现 Google、GitHub、LinuxDo OAuth、OIDC 或其他邮箱域名。
- 注：本地服务子域名（`{slug}-{nodeId}-relay.{apex}`）是节点主动调用 `/api/device/nodes/{id}/services/{slug}` 的兼容缺口，须实现，不在「明确不做」之列。

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

- **职责**：QQ 邮箱准入、注册/重置验证码、密码凭据、用户、登录会话和当前用户身份；不处理 OAuth、tenant、RBAC 或节点数据转发。
- **承载的子 feature**：cloud-email-accounts、cloud-node-sharing。
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
POST /api/auth/register/request-code
Content-Type: application/json

{
  "email": "421690794@qq.com"
}
~~~

~~~json
{
  "resend_after_seconds": 60
}
~~~

~~~http
POST /api/auth/register
Content-Type: application/json

{
  "email": "421690794@qq.com",
  "password": "user-defined-password",
  "code": "123456"
}
~~~

~~~http
POST /api/auth/login
Content-Type: application/json

{
  "email": "421690794@qq.com",
  "password": "user-defined-password"
}
~~~

注册时验证邮箱验证码并保存用户自设密码的强哈希；日常登录不发送验证码。忘记密码通过 `POST /api/auth/password/request-code` 和 `POST /api/auth/password/reset` 完成，重置成功后撤销旧 Session。只接受规范化后域名严格等于 `qq.com` 的地址；其他域名在发送邮件前返回 `email_not_allowed`。验证码 10 分钟有效、单次使用、按用途隔离，服务端只存 keyed hash，并按邮箱与请求来源限流。key 在 Cloud 数据目录首次启动时自动生成并持久化，不增加人工密钥配置。

登录成功使用 `mindfs_cloud_session` HttpOnly、Secure、SameSite=Lax Cookie。`GET /api/auth/me` 返回当前用户的 `id`、`email` 和 `name`；`POST /api/auth/logout` 同时撤销服务端 Session 并清除 Cookie。官方页面公开的 Google、GitHub、LinuxDo OAuth 路由不实现。

V0 的 bootstrap 用户名/密码登录由本 feature 替换。已有节点在迁移时归属一个待认领 bootstrap QQ 用户；默认使用 SMTP 用户名，允许通过可选 `MINDFS_CLOUD_BOOTSTRAP_EMAIL` 覆盖。该邮箱完成验证码注册并设置自己的 Relay 密码后认领既有节点。

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
  "name": "Office Mac"
}
~~~

确认绑定必须有邮箱用户 Session，成功后把节点 `owner_user_id` 设为当前用户。为平滑升级，Cloud 同时接受 V0 的 `action=confirm` + `node_name` 请求体；两种格式进入同一确认编排。

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
users
- id, email, password_hash, status, created_at, password_changed_at, last_login_at

email_verification_codes
- email, purpose, code_hash, expires_at, attempts_remaining
- resend_available_at, created_at, consumed_at

cloud_sessions
- id_hash, user_id, expires_at, created_at, last_seen_at

bind_challenges
- code_hash, purpose, device_id, requested_node_name
- status, expires_at, claimed_by_user_id
- node_id, issued_token_id, token_derivation_version
- created_at, confirmed_at

nodes
- id, owner_user_id, device_id
- name, status, access_mode, created_at, last_seen_at

device_tokens
- id, node_id, token_hash, status
- created_at, last_used_at, revoked_at

node_services
- node_id, slug, name, enabled, created_at, updated_at

node_shares
- id, node_id, subject_type, subject_id, role, expires_at

audit_events
- id, actor_type, actor_id, action
- resource_type, resource_id, request_id, metadata_json, created_at

usage_counters
- user_id, node_id, period
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

4. **cloud-node-discovery**（V0 修正项，2026-08-04 回填）— 让 bootstrap 管理员通过客户端既有 Relay 控制台契约（/login、/nodes、GET/PATCH/DELETE /api/nodes、/api/auth/me、/api/auth/logout）列出、打开、重命名和删除服务端节点。
   - 所属模块：Identity、Binding、Management API
   - 依赖：relay-core-single-instance、relay-deployment-baseline
   - 状态：done
   - 对应 feature：2026-08-04-cloud-node-discovery
   - 备注：2026-08-04 `cloud-v0-completion` 探查发现原 V0 三项虽 done，但缺服务端节点发现闭环（换浏览器或清站点数据后无法找回节点），按探查建议回填为 V0 修正项，使单用户 V0 形成可用闭环；只复用客户端既有契约，不新增协议、不实现多用户与 Token 主动轮换。

### V1：兼容补完（客户端/节点驱动）

经逐项核对官方 Relay 的公开页面与未修改客户端/节点调用，V0 后有两项可观察兼容能力：邮箱验证码账号与本地附加服务域名。账号能力已完成；本地服务契约保留，但用户已明确暂停为 TODO。

5. **cloud-email-accounts** — 实现仅 `@qq.com` 的验证码注册、邮箱密码登录和找回密码，并以用户账号隔离节点绑定、列表、重命名和删除。
   - 所属模块：Identity、Binding、Management API、Store、Config
   - 依赖：relay-core-single-instance、relay-deployment-baseline、cloud-node-discovery（均已 done）
   - 状态：done
   - 对应 feature：2026-08-05-cloud-email-accounts
   - 兼容边界：未修改客户端只依赖 `/login`、`/api/auth/me`、logout 和认证后的节点页面；登录页内部采用自建的注册/密码登录流程
   - 范围：注册验证码、用户自设密码、邮箱密码登录、忘记/修改密码；仅 `@qq.com`；QQ SMTP 465 implicit TLS；现有节点由 bootstrap QQ 邮箱注册后认领；节点管理按 owner 隔离
   - 明确不做：日常验证码登录、QQ 邮箱密码采集、Google/GitHub/LinuxDo OAuth、OIDC、tenant、RBAC、节点共享、其他邮箱域名

6. **relay-local-service-domains** — 实现 device 鉴权的服务路由 API（`/api/device/nodes/{id}/services/{slug}`）与 `{slug}-{nodeId}-relay.{apex}` 公网子域名转发。
   - 所属模块：Gateway、Connector、Store
   - 依赖：relay-core-single-instance、relay-deployment-baseline（均已 done；不再依赖可选的 relay-security-audit）
   - 状态：planned / TODO（2026-08-05 用户明确暂停；未重新启用前不实现、不部署）
   - 契约来源：请求格式见 `server/internal/relay/services.go`；响应格式黑盒官方 relay 获取
   - 转发复用现有 gateway/yamux，带 `X-MindFS-Relay-Service-Slug` 头，节点侧代理到 `local_url`
   - 恢复该 TODO 后才进入部署确认；当前不要求 wildcard DNS/TLS/OpenResty 操作

### 非兼容愿景（不承诺，按目标启用）

以下各项**客户端/节点均不调用**，属"想从自托管升级为运营云平台"才需要的愿景，非兼容义务，未列入排期。按触发条件分组（细节见 items.yaml 各项 notes）：

- **扩展运营云平台才需要**：cloud-node-sharing（客户端零调用）、relay-postgres-control-plane（客户端不可见）、relay-distributed-routing（客户端不可见）、relay-usage-quotas、cloud-operations-observability、cloud-backup-recovery。
- **与节点已实现功能重复（cloud 不必做）**：token-station-account-ledger / model-gateway / billing、hosted-agent-config（`/api/agents`）、cloud-tips-content（`/api/relay/tips`）——客户端均经 `appPath` 走节点。
- **客户端不打自建云**：release-distribution（移动版本检查硬编码 `relay.a9gent.com`）。
- **运维可选（非兼容）**：cloud-node-management（Token 主动轮换；客户端零调用，删除即撤销已满足）、relay-security-audit（邮箱认证核心防护由 cloud-email-accounts 自带，本条只保留更广的运营安全治理）。
- **持续兼容维护纪律（已部分承载）**：cloud-api-lifecycle（兼容矩阵/回归；V0 compatibility-suite 与多版本资源已是其实例，按上游变更增量加强）。

**已移除**：~~relay-custom-domains~~——客户端零调用、用户无需求（单 relay 域名由 Cloudflare/Caddy 在部署侧解决），不属于 cloud 兼容义务，从 roadmap 删除。

**最小闭环**：relay-core-single-instance 完成后，未修改的 MindFS 客户端能够通过 MINDFS_RELAY_BASE_URL 完成绑定、建立 Connector，并从 cloud 的 /n/{nodeId} 入口正常使用 HTTP 和 WebSocket。

**V0 状态**：核心实现、真实客户端兼容套件、部署基线和 V0 修正项 cloud-node-discovery 均已完成；V0 核心兼容后端闭环结束。V1 的 cloud-email-accounts 已完成并部署验证；本地服务兼容特例 relay-local-service-domains 按用户决定继续暂停，不属于当前交付。

## 6. 排期思路

V0 采用跨模块垂直切片，因为只做绑定或 Connector 都不能产生可验证价值。第一条必须直接跑通未修改客户端。第二条把兼容性固化成测试，第三条才形成可重复部署版本。

V1 先补官方公开页面可观察的邮箱验证码账号与节点 owner 隔离；本地附加服务保持 TODO。PostgreSQL、Redis、多实例、共享和配额仍是按需愿景，不阻塞兼容 Relay 核心价值。

所有后续 feature 都必须遵守现有代码只读边界；任何需要修改客户端才能完成的设计必须退回 roadmap review。

## 7. 观察项

- architecture/ARCHITECTURE.md 当前仍是骨架；cloud 真正落地后只能记录 cloud 的现状，不回写或重构既有客户端模块。
- 对应 requirement 为 mindfs-compatible-cloud-backend；V0 acceptance 后应按实际能力将其从 draft 更新为 current。
- 官方 Relay 源码不可见，私有行为只能通过未修改客户端进行兼容验证。
- 仓库使用 AGPL-3.0，云服务通过网络提供时需要遵守相应源码义务。
- web 启动器中的官方 /nodes 地址是硬编码；本 roadmap 不修改它，自建云入口通过 MINDFS_RELAY_BASE_URL、直接 URL 或外部管理系统提供。
- 如果上游未来新增 cloud/ 同名目录，需要由用户决定迁移新后端目录，不能直接覆盖上游文件。

## 8. 变更日志

- 2026-08-05：根据用户最终确认，将 V1 `cloud-email-accounts` 收敛为仅 `@qq.com` 的验证码注册、邮箱密码登录、密码找回/修改、Session 与节点 owner 隔离；验证码不用于日常登录，明确排除邮箱密码采集、OAuth/OIDC/tenant/RBAC；现有节点由 bootstrap QQ 邮箱注册后认领。
- 2026-08-05：`cloud-email-accounts` 验收完成；线上注册由用户确认，登录页桌面/移动端、owner 隔离、V0 迁移和未修改客户端 compat 均通过。`relay-local-service-domains` 继续保持暂停 TODO。
- 2026-08-03：关联 mindfs-compatible-cloud-backend requirement；补充确定性 HMAC device token 派生、只存 hash、confirmed 幂等重放和独立 MINDFS_CLOUD_TOKEN_KEY 契约。
- 2026-08-03：部署契约补充 MINDFS_CLOUD_ASSETS_DIR 与 /mindfs-assets/{path}，确保未修改 Node 的 release 静态路径重写在自建 Cloud 中可实际加载。
