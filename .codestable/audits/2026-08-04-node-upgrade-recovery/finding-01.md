---
doc_type: audit-finding
audit: 2026-08-04-node-upgrade-recovery
finding_id: "bug-01"
nature: bug
severity: P1
confidence: high
suggested_action: cs-issue
status: mitigated
---

# Finding 01：主前端资源失败时页面内升级入口不可达

## 速答

节点的主 JS 加载失败时只弹出“版本太老”，而原有后端自动升级按钮和请求逻辑都在这个 JS 中，用户无法通过页面完成恢复。

## 关键证据

- `web/index.html:47-80` — 主 asset 加载失败时只执行 `window.alert(notice)`，没有独立于 SPA bundle 的升级或恢复动作。
- `web/src/App.tsx:14021-14028` — 左下角更新按钮和 `handleStartUpdate` 由 React 主应用渲染；主 JS 未加载时这些代码不可执行。
- `web/src/services/update.ts:18-25` — 页面内升级依赖 `GET/POST /api/app/update`，调用封装同样属于主 bundle。
- 线上 `https://relay.20260310.best/n/nvyslm4tgeasawkyz5gmc6cm5le/` 返回 200，但其 `index-DeNebQ9q.js` 返回 404，截图中的 alert 已实际触发。

## 影响

Cloud 缺少 Node 页面引用的主资源时，用户既进不了节点 UI，也无法使用之前位于左下角的自动升级入口。当前故障即使 Node 已是最新版本也会触发，因此 CLI 更新并不是这次事件的正确首选恢复动作。

## 修复方向

提供不依赖主 SPA bundle 的最小恢复入口；如果客户端保持零修改，则必须由 Cloud 的完整性校验和多版本 asset 兼容策略保证所需主 JS 始终可加载。

## 建议动作

`cs-issue`，因为这是已确认可复现的升级恢复死锁。

## 修复结果

Cloud 现已通过持久化多 release asset repository、入口资源 readiness 校验和 asset 404 `no-store` 阻断该死锁的已知触发路径。未修改客户端 `index.html` 的 alert 兜底，因此本 finding 标记为 `mitigated` 而不是彻底消除；客户端零修改仍是硬约束。
