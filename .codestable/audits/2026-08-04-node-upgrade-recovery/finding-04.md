---
doc_type: audit-finding
audit: 2026-08-04-node-upgrade-recovery
finding_id: "bug-04"
nature: bug
severity: P2
confidence: medium
suggested_action: cs-issue
status: resolved
---

# Finding 04：缺失 asset 的 404 可被边缘缓存，延长恢复时间

## 速答

Cloud 对不存在的 asset 直接返回默认 404，没有显式禁止缓存；当前线上边缘层把该 404 缓存 4 小时。

## 关键证据

- `cloud/app/operations_handlers.go:31-40` — asset 打开失败直接 `http.NotFound`，成功响应才在 48 行设置 immutable cache，404 没有缓存策略。
- 线上 `HEAD /mindfs-assets/index-DeNebQ9q.js` 返回 404，响应带 `Cache-Control: max-age=14400`。

## 影响

即使随后部署补回同名 hashed asset，部分用户仍可能继续命中边缘缓存的 404，造成“已经修复但浏览器仍报旧版本”的假象。

## 修复方向

缺失或非法 asset 响应显式设置 `Cache-Control: no-store`；只有成功读取的 content-hashed 文件使用 immutable cache。

## 建议动作

`cs-issue`，因为需要定点修改错误响应缓存语义并补 HTTP 回归测试。

## 修复结果

`/mindfs-assets/` 的空路径、非法路径、缺失文件和非普通文件统一先设置 `Cache-Control: no-store` 再返回 404；成功文件继续使用一年 immutable 缓存。HTTP 回归测试已覆盖两种缓存语义。
