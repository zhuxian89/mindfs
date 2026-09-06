---
doc_type: audit-finding
audit: 2026-09-06-relay-upstream-compatibility
finding_id: bug-02
nature: bug
severity: P1
confidence: medium
suggested_action: cs-issue
status: open
regression: introduced-in-2d76ff9-to-3471043
---

# Finding 02：同域名路径式多节点串用会话列表缓存

## 速答

新版新增的持久化会话列表缓存没有按节点隔离。同一个浏览器访问同 Relay origin 的 `/n/A/` 和 `/n/B/` 时，B 在网络结果返回前会把 A 缓存的项目与会话列表显示出来。跨项目列表使用固定 key，不要求两个节点的 root ID 或 session ID 相同。

这是前端显示与数据隔离回归，不是 Cloud 鉴权绕过或协议失配。静态调用链明确；本次没有运行双节点浏览器复现，所以置信度为 medium。

## 关键证据

- `web/src/services/base.ts:3-15`：Relay 节点位于同 origin 的 `/n/{nodeId}` 路径下。IndexedDB 按 origin 隔离，不按路径隔离。
- `web/src/services/session.ts:1530-1534`：

  ```ts
  const SESSION_CACHE_DB = "mindfs-session-cache";
  const SESSION_LIST_CACHE_STORE = "session-lists";
  const MULTI_ROOT_SESSION_LIST_CACHE_KEY = "multi-root";
  ```

- `web/src/services/session.ts:1664-1669`：所有节点读写同一固定 key，没有 node/user 范围：

  ```ts
  return readCachedSessionList<MultiRootSessionGroup[]>(MULTI_ROOT_SESSION_LIST_CACHE_KEY);
  // saveCachedMultiRootSessionList 也使用相同 key
  ```

- `web/src/App.tsx:4917-4941`：先读缓存并调用 `setMultiProjectSessionGroups(...)`，未检查缓存属于哪个节点，也未要求缓存 root 存在于当前节点，然后才执行：

  ```ts
  const groups = await sessionService.fetchMultiRootSessions(MULTI_PROJECT_SESSION_LIMIT);
  ```

- `web/src/components/SessionList.tsx:1009-1017`：只有 loading 且列表为空才显示加载提示；非空缓存照常渲染：

  ```tsx
  {loading && groups.length === 0 ? (...) : ...}
  ```

- 同文件 `:1155-1168`：缓存渲染的条目仍有选择、重命名、删除等交互。不能据此断言删除了 A 的远程数据：后续请求发向当前节点 B，是否成功还受 B 的实际 root/session 和服务端校验约束。

## 触发链

1. 浏览器在 `/n/A/` 启用跨项目会话，并成功保存 A 的列表。
2. 同一浏览器进入相同 origin 的 `/n/B/`，同样启用跨项目列表。
3. B 的 `getCachedMultiRootSessionList()` 读取 A 的 `multi-root` 记录。
4. 页面先显示 A 的项目、会话标题/摘要，之后才请求 B 的列表。
5. B 请求完成后替换显示；慢连接会扩大错误列表显示及可点击窗口。

## 新旧区分

- `2d76ff9` 的 `App.tsx:4720-4726` 直接获取当前节点列表，没有这条持久化列表读取路径。
- 旧详情缓存已使用缺少节点范围的 `rootId::sessionKey`，但需要 ID 碰撞才能串用；本次固定 `multi-root` 缓存新增了无需任何 root/session ID 碰撞的场景。
- 新单项目列表的 `root::${rootId}`（`session.ts:1625-1626`）同样没有节点范围，但本发现以更确定的跨项目场景为主要证据。

## 影响与边界

- 单节点使用不触发跨节点串用；不同 origin 的节点不共享该 IndexedDB。
- 网络成功后会用 B 的数据替换；网络错误时底层服务返回空数组也会替换。不要描述为永久串数据。
- 不宣称 E2EE 被破解、服务器越权或跨账户远程数据可任意读取；这是当前浏览器已持有数据的缓存范围问题。
- 现有前端/compat 测试不覆盖同 origin 双节点 IndexedDB 隔离。

## 修复方向与建议动作

建议 `cs-issue`：以稳定的节点范围（必要时加账户范围）区分列表缓存，处理旧缓存迁移/失效，并添加同 origin 双节点切换的浏览器验证。本审计不改前端或要求修改 Cloud 协议。
