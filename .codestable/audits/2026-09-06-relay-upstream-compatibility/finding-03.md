---
doc_type: audit-finding
audit: 2026-09-06-relay-upstream-compatibility
finding_id: bug-03
nature: bug
severity: P2
confidence: high
suggested_action: cs-issue
status: fixed
resolved_in: working-tree
regression: pre-existing-at-2d76ff9
---

# Finding 03：Gateway 丢失部分转义路径，E2EE 请求签名失配

## 2026-09-06 修复状态

已修复并验证，尚未部署。HTTP/WS 按 EscapedPath 剥前缀并保存对应 Path/RawPath；当前源码 Node 与官方 v0.5.0 二进制的 `%3A`、`%3a`、`%2F`、`%252F` 加密路径均通过回归，响应与直连 Node 一致。见 [修复记录](../../issues/2026-09-06-relay-release-compatibility/relay-release-compatibility-fix-note.md)。以下是修复前保留的缺陷证据，行号对应当次审核。

## 速答

含冒号的 toolcall ID 通过路径式 Relay 请求详情时，浏览器按 `%3A` 签名，但 Gateway 把它转回 `:`。节点看到的 URI 与浏览器签名 URI 不一致，启用 E2EE 后请求会被判为 `e2ee_proof_invalid`。

**这是旧版已经存在的问题，不是本次新增回归。** 上游本次将节点签名路径改成 `EscapedPath()`，修复直接连接时的编码问题，但无法恢复 Gateway 已丢弃的原始编码。

## 关键证据

- `web/src/components/stream/ToolCallCard.tsx:579-605`：展开缺少详情的工具卡片会请求 toolcall。
- `web/src/services/session.ts:1256-1260`：将 sessionKey/callId 作为 URL 编码后的路径段：

  ```ts
  `/api/sessions/${encodeURIComponent(sessionKey)}/toolcalls/${encodeURIComponent(callId)}`
  ```

- `web/src/services/e2ee.ts:607-614`：浏览器证明使用 URL pathname，保留 `%3A` 并仅剥除 `/n/{nodeId}`：

  ```ts
  const pathname = target.pathname.replace(/^\/n\/[^/]+/, "") || "/";
  return target.search ? `${pathname}${target.search}` : pathname;
  ```

- `cloud/internal/gateway/http.go:53,156-157`（当前工作区）：使用已解码的 Path 剥节点前缀，然后显式丢弃 RawPath：

  ```go
  nodeID, path, err := parseNodeRoute(r.URL.Path)
  // ...
  outbound.URL.Path = path
  outbound.URL.RawPath = ""
  ```

- 同文件 `:67`：`outbound.Write(stream)` 重新编码 URI。Go 的路径序列化允许原样保留冒号，因此不会还原原先的 `%3A`。
- `server/internal/api/http.go:119-121,126-134`：最新节点对收到的 `URL.EscapedPath()` 计算证明，不一致返回 `e2ee_proof_invalid`。

## 隔离定向复现

通过 Go overlay 向 `cloud/internal/gateway` 虚拟注入测试；未创建业务目录文件、启动网络服务或修改 Gateway。

测试直接调用当前 `parseNodeRoute`、`cloneRequest`，将请求写入内存并模拟节点重新读取，结果：

```text
--- FAIL: TestAuditRelayPreservesEscapedProofPath
browser="/api/sessions/session-1/toolcalls/claude-task-list%3A1?root=mindfs"
node="/api/sessions/session-1/toolcalls/claude-task-list:1?root=mindfs"
```

这两个不同的字符串用于同一个 E2EE HMAC proof，无法通过签名校验。复现文件与 overlay 位于本次会话临时目录 `/tmp/mindfs-relay-audit.L1Pbt6/`。

### 正式发布包追加验证

2026-09-06 后续使用校验过 GitHub SHA-256 的官方 v0.5.0 二进制，在隔离 Node/Cloud 进程中复现：相同 session 和 `%3A` 路径，直连 Node 得到加密 400（签名通过，测试业务数据不存在），经当前 Cloud 得到 401 `e2ee_proof_invalid`。当前 Gateway 的内存序列化复现也再次按预期失败。完整方法与测试边界见 [发布补充核验](release-followup.md)，日志见 `release-evidence/release-features.log` 和 `release-evidence/escaped-path-reproduction.log`。此项尚未修复。

## 新旧区分

- Cloud gateway 在 `2d76ff9` 与 `3471043` 完全相同。
- 旧 `web/src/services/session.ts:1213` 已用同样的 `encodeURIComponent(callId)` 路径。
- 旧节点签名使用解码后的 `URL.Path`，对这里的 `%3A` 同样失配；因此不归因于本次更新。
- 新节点 `http_test.go:48-59` 检验了直达请求保留 `%3A`，未涵盖 Relay 提前丢弃 RawPath 的链路。

## 影响与边界

- 触发条件：通过 Relay、启用 E2EE、路径参数含冒号等会被 Gateway 改写的编码、需要向节点加载工具详情。
- 可能表现为卡片详情加载失败、重复提示输入配对密钥或 `e2ee_proof_invalid`；不代表整个连接或全部聊天不可用。
- 普通 ASCII 固定路由不受影响；文件参数放在 RawQuery 中的常规请求不等于本问题。
- 不应扩大为所有中文/空格路径都失效：常规 Unicode/空格经过规范重新编码通常可保持一致。
- 现有真实节点兼容测试验证固定 ASCII 路由，未覆盖本场景。

## 修复方向与建议动作

建议 `cs-issue`：让 Relay 剥节点前缀时保留业务路径原始编码语义，并以实际前端证明串做端到端回归。按照 attention，不能以修改客户端签名或偏离官方路径契约来规避。本审计不修改 Gateway。
