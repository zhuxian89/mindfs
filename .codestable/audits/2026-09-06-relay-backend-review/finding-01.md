---
doc_type: audit-finding
audit: 2026-09-06-relay-backend-review
finding_id: security-01
nature: security
severity: P0
confidence: high
suggested_action: cs-issue
status: partially-fixed
remaining_action: declined-by-user
resolved_in: working-tree
---

# Finding 01：Cloud 登录会话被转发给不可信节点

## 2026-09-06 处理进度

已封堵 Cloud Cookie 的 HTTP/WS 转发与节点 Set-Cookie 覆盖，并约束节点 Cookie 路径。2026-09-06 用户了解剩余同源风险后，明确决定不继续修复节点脚本访问控制接口的问题；保留已完成的防护，不实施独立管理域名。此项保持部分修复的技术状态，剩余行动按用户决定结束，见 [修复记录](../../issues/2026-09-06-relay-backend-hardening/relay-backend-hardening-fix-note.md)。以下保留审计时的证据。

## 速答

登录 Cloud 的用户打开另一个用户控制的 `/n/{nodeId}/` 地址时，Relay 把其 `mindfs_cloud_session` Cookie 一并交给该节点。节点可以重放这个 Cookie，读取访问者的 Cloud 账号和节点列表，或以其身份执行节点管理操作。

## 关键证据

- `cloud/app/identity_handlers.go:148` 设置 Cloud Cookie 的 `Path: "/"`，浏览器会在 `/n/...` 请求中自动携带它：

  ```go
  Name:     userSessionCookie,
  Value:    token,
  Path:     "/",
  HttpOnly: true,
  ```

- `cloud/internal/gateway/http.go:148` 完整复制 Header，后续仅移除 hop-by-hop 和特定内部头，没有移除 Cloud Cookie：

  ```go
  outbound.Header = r.Header.Clone()
  removeHopHeaders(outbound.Header)
  ```

- `cloud/internal/gateway/http.go:65` 把这个请求写进节点 stream；WebSocket 的 `cloud/internal/gateway/websocket.go:47` 也使用同一 `cloneRequest`。
- `cloud/internal/gateway/http.go:88` 只校验节点存在、active 和在线，没有要求访问者是 owner。按当前架构，数据面本来就不依赖 Cloud Session，这不是遗漏一个既有 owner 判断，而是凭据跨越了信任边界。
- `cloud/app/identity_handlers.go:139` 从 Cookie 验证 Cloud 身份；同源检查只能限制浏览器跨站请求，不能阻止取得凭据的节点自行构造请求。

## 定向复现

`TestAuditRelaySessionCrossesNodeBoundary` 使用真实 App、临时 SQLite、两个合成 QQ 用户和注册在 Registry 中的模拟节点：

1. 账号 A 绑定节点；账号 B 获得 Cloud 登录会话。
2. B 携带会话请求 A 的 `/n/{id}/`。
3. 模拟节点从收到的 HTTP 请求中取得 B 的 Cloud Cookie。
4. 使用收到的 Cookie 请求真实 App 的 `/api/auth/me`，返回 200 且身份为 B。

测试没有修改业务源码，没有读取或打印真实 Cookie，也没有执行删除操作。结果证明凭据泄露和身份重放；写操作的权限影响由同一鉴权路径可确认。

## 影响与边界

- 需要访问者已登录，且访问了不可信或被攻陷的在线节点。节点 owner 可以控制自己发送的 Connector 数据，不能把所有节点当作可信后端。
- `HttpOnly` 只限制网页 JavaScript 直接读取 Cookie，无法阻止服务器把 Cookie 发给另一个后端；`Secure` 和 `SameSite` 也不阻止这个同源请求。
- 如果线上只有自己且所有节点都可信，暂时不具备跨用户攻击前提；本次未核验生产用户构成。
- 节点内容与 Cloud 控制台共享 origin 还需要整体检查。节点返回的脚本可能以同源身份访问控制 API，所以仅过滤 Cookie 不足以宣告多用户浏览器隔离完成。

## 修复方向与建议动作

`cs-issue`：先阻止 Cloud 专属 Cookie 进入 HTTP/WS 节点请求，并检查节点响应对控制面 Cookie 的覆盖；进一步明确节点内容与 Cloud 控制面的浏览器信任边界。方案必须遵守现有公开路径兼容约束，不修改客户端来掩盖后端问题。本审计未实施修复。
