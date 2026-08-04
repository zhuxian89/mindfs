---
doc_type: audit-index
audit: 2026-08-04-node-upgrade-recovery
scope: Relay 节点资源失败、页面内自动升级入口、Cloud 共享 assets 与 readiness
created: 2026-08-04
status: remediated
total_findings: 5
---

# Node Upgrade Recovery 审计报告

## 范围

扫描 `web/index.html`、`web/src/App.tsx`、`web/src/services/update.ts`、`server/internal/api/http.go`、`server/internal/update/service.go`、`cli/cmd/mindfs.go`、`cloud/app/operations_handlers.go`、`cloud/internal/config/config.go`、`cloud/Dockerfile` 及当前 Cloud Relay 架构与部署设计。结合官方 `v0.4.6` release、一次仅构建不启动服务的 Docker Web stage，以及线上 `relay.20260310.best` 的节点页面、hashed JS/CSS 和 `/readyz` 响应做只读验证。

## 总评

共发现 5 条：3 条 P1、2 条 P2。当前故障不是 Node 版本太老：节点页面引用的 `index-DeNebQ9q.js` 属于官方最新 `v0.4.6`，官方安装包和当前 Cloud Docker 构建都包含该文件，但线上 Cloud 返回 404，说明 VPS 正在运行的资源包与当前 release 不一致。页面兜底又把任意主资源加载失败都表述为“版本太老”，导致原有页面内升级入口不可达。应先完整重建并强制重建 VPS Cloud 容器，而不是升级该 Node；随后再修 readiness、404 缓存和历史版本 assets 兼容问题。

## 修复状态

Finding 02-05 已在 Cloud 后端代码中解决，Finding 01 在客户端零修改约束下通过保证资源完整性标记为 mitigated。最终实现保留官方 `/mindfs-assets/` 路径，并以持久化多 release 资源并集复刻官方后端：真实导入 25 个正式 release、2166 个 asset 文件，约 110.8 MiB；重复同步不新增文件。生产环境仍需部署当前 Cloud 镜像后生效。

## 发现清单

| # | 性质 | 严重度 | 置信度 | 标题 | 文件 |
|---|---|---|---|---|---|
| 1 | bug | P1 | high | 主前端资源失败时页面内升级入口不可达 | [finding-01.md](finding-01.md) |
| 2 | bug | P1 | high | 线上 Cloud 资源包与当前 v0.4.6 release 不一致 | [finding-02.md](finding-02.md) |
| 3 | arch-drift | P2 | high | readiness 只检查目录存在，损坏的资源闭环仍显示 ready | [finding-03.md](finding-03.md) |
| 4 | bug | P2 | medium | 缺失 asset 的 404 可被边缘缓存，延长恢复时间 | [finding-04.md](finding-04.md) |
| 5 | arch-drift | P1 | high | Cloud 的单一 asset 命名空间无法兼容历史 Node bundle | [finding-05.md](finding-05.md) |

## 按维度分布

| 性质 | P0 | P1 | P2 | 合计 |
|---|---|---|---|---|
| bug | 0 | 2 | 1 | 3 |
| security | 0 | 0 | 0 | 0 |
| performance | 0 | 0 | 0 | 0 |
| maintainability | 0 | 0 | 0 | 0 |
| arch-drift | 0 | 1 | 1 | 2 |
| **合计** | **0** | **3** | **2** | **5** |

## 下一步建议

- 在 VPS 构建并部署当前 Cloud 镜像，等待 `asset-sync` 成功后再启动 Relay。
- 部署后确认当前与历史 hash 均返回 `200` 和一年 immutable 缓存，并确认任意不存在的 hash 返回 `404` 与 `Cache-Control: no-store`。
- 保留 `relay-assets` volume，升级时不得删除或重建该 volume。
