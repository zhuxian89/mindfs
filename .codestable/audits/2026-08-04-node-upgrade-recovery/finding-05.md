---
doc_type: audit-finding
audit: 2026-08-04-node-upgrade-recovery
finding_id: "arch-drift-05"
nature: arch-drift
severity: P1
confidence: high
suggested_action: cs-issue
status: resolved
---

# Finding 05：Cloud 的单一 asset 命名空间无法兼容历史 Node bundle

## 速答

标准 release Node 会把自身页面的 `./assets/index-HASH.js` 改写到 Cloud 全局 `/mindfs-assets/`，但 Cloud 镜像只包含当前 checkout 的一套 bundle；即使修复本次部署，未来 Cloud 与尚未升级的 Node 版本不一致时仍可能再次丢失 Web UI。

## 关键证据

- `server/internal/api/http.go:1456-1493` — 所有标准 release 的 relayed 页面统一把 `./assets/` 改写为无版本前缀的 `/mindfs-assets/`，路径中没有 release 版本维度。
- `cloud/Dockerfile:3-8,20` — Cloud 每次只构建当前 checkout 的 `web/dist` 并复制进最终镜像，没有保留受支持历史 release 的 assets。
- `.codestable/features/2026-08-03-relay-deployment-baseline/relay-deployment-baseline-design.md:23,67` — 设计把 Asset Bundle 定义为“当前 checkout”，但同时把兼容性标为 backward-compatible，二者在跨版本节点场景下冲突。
- 官方 `v0.4.5` 与 `v0.4.6` 安装包使用不同的 `index-HASH.js` 和 CSS hash，说明旧 Node 所需文件不会自然包含在当前 bundle 中。

## 影响

Cloud 先升级、Node 后升级的正常滚动窗口内，历史 Node 可能失去 Web UI，进而连页面内升级按钮也无法使用；多个 Node 版本并存时会形成批量兼容风险。

## 修复方向

Cloud 在同一 `/mindfs-assets/` content-hash 命名空间保留所有受支持 release 的 assets。hashed 文件天然可并存，这条方案不要求修改客户端路径、接口或任何客户端代码。

## 建议动作

`cs-issue`，因为这是可由现有路径契约稳定复现的 Cloud 兼容性缺陷。

## 修复结果

新增 `mindfs-relay sync-assets` 和持久化 `relay-assets` volume，从 `v0.1.8` 起合并官方正式 release 的 `web/assets/`。同步校验 release archive 的大小和 SHA-256，拒绝不安全 tar entry；content-hashed 文件同名不同内容立即失败且永不删除。真实同步 25 个官方 release 后，`v0.4.4`、`v0.4.5`、`v0.4.6` 三个主 JS hash 可同时读取。
