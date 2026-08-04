---
doc_type: issue-analysis
issue: 2026-08-04-relay-node-assets-unavailable
status: confirmed
root_cause_type: config
related: [relay-node-assets-unavailable-report.md]
tags:
  - cloud-relay
  - asset-compatibility
  - deployment
---

# Relay Node Assets Unavailable 根因分析

## 1. 问题定位

| 关键位置 | 说明 |
|---|---|
| `server/internal/api/http.go:1456-1493` | 未修改 Node 收到 `X-MindFS-Relayed: 1` 后，把 release 页面中的 `./assets/` 改成 Cloud 全局 `/mindfs-assets/`。这是客户端既有协议，只读。 |
| `cloud/internal/gateway/http.go:51-85,143-159` | Gateway 为所有 Public Node Route 请求设置 relayed header，并按官方协议把 Node 响应原样流回浏览器；全局 `/mindfs-assets/` 路径本身是正确契约。 |
| `cloud/Dockerfile:3-8,20` | Cloud 镜像只构建并携带当前 checkout 的一套 `web/dist`，没有像官方 Relay 一样形成只增不删的多 release asset 集合。 |
| `cloud/app/operations_handlers.go:31-50` | `/mindfs-assets/` 只读取 Cloud 本地 bundle；缺失文件直接 404，错误响应没有禁止缓存。 |
| `cloud/internal/config/config.go:98-113` | Asset readiness 只检查 `index.html` 与 `assets/` 目录存在，不检查入口文件引用的 JS/CSS。 |

## 2. 失败路径还原

**正常路径**：浏览器请求 `/n/{nodeId}/` → Cloud Gateway 转发给 Node → Node 返回 release `index.html` → 页面引用的 JS/CSS 成功加载 → React UI 启动并显示原有升级入口。

**失败路径**：浏览器请求 `/n/{nodeId}/` → Node 因 relayed header 把 `./assets/index-HASH.js` 改成 `/mindfs-assets/index-HASH.js` → 浏览器转向 Cloud 全局 assets handler → Cloud 运行镜像不含该 Node 对应 hash → 返回并缓存 404 → 主应用未启动，只剩 HTML 中的强制版本提示。

**分叉点**：`cloud/Dockerfile` 与 `cloud/app/operations_handlers.go` — 未修改客户端已经正确请求官方全局 asset URL，但我们的 Cloud 只部署当前 checkout 的单一 bundle，未实现官方 Relay 对历史 release 资源只增不删的存储语义。

## 3. 根因

**根因类型**：配置 / 环境。

**根因描述**：官方 Relay 把所有受支持 Node release 的 content-hashed Web 资源保存在同一个全局 `/mindfs-assets/` 命名空间，并以一年 immutable 缓存提供。我们的兼容后端只复制了 Cloud 构建时的一套 Web bundle，没有实现官方后端的持久化多 release 资源并集；Cloud 与 Node release 不同或部署产物不完整时，Node 页面因此引用 Cloud 不拥有的 hash。

**是否有多个根因**：是。主根因是缺少官方式持久化多 release asset 仓库；次要根因是 readiness 不校验入口资源，以及 404 可被边缘缓存，二者分别让错误部署通过健康检查并延长故障时间。

## 4. 影响面

- **影响范围**：所有通过 Public Node Route 打开的标准 release 页面；Cloud 先升级、Node 后升级、Node 先升级、Cloud 部署不完整三种场景都可能触发。
- **潜在受害模块**：节点首页、SPA 路由、动态 import、CSS、字体、Service Worker 和页面内升级入口。
- **数据完整性风险**：无；问题阻断 UI 加载，但不修改 Node 数据或 Cloud SQLite。
- **严重程度复核**：维持 P1；核心远程 UI 完全失效，但节点数据仍在且可通过运维恢复。

## 5. 修复方案

### 方案 A：持久化多 release immutable asset 仓库

- **做什么**：增加 Cloud `sync-assets` 运维命令和持久化 assets volume。每次部署先把当前镜像 bundle 合入 volume，再从官方 GitHub releases 回填自 `v0.1.8` 起的 release assets；content-hashed 文件只增不删，同名不同内容立即失败。Relay 继续按原路径提供 `/mindfs-assets/`。
- **优点**：与官方 Relay 已观测到的行为一致；保留全局 immutable/CDN 缓存；当前、历史版本资源可并存；后续 Cloud 升级自动追加新 bundle。
- **缺点 / 风险**：首次部署需要下载历史 release，耗时和磁盘占用增加；初始化阶段依赖 GitHub 可用。
- **影响面**：`cloud/internal/assetsync`、Cloud 命令入口、`cloud/Dockerfile`、Compose、readiness、assets handler、`cloud/go.mod`、测试、部署文档与 `.codestable/architecture/cloud-relay-core.md`。

### 方案 B：Relay 把页面资源重新绑定到来源 Node

- **做什么**：Gateway 对 Node 返回的 HTML 和 Service Worker 内容，把 `/mindfs-assets/` 改写为 `/n/{nodeId}/assets/`；浏览器随后通过现有 Public Node Route 从该 Node 获取完全匹配的资源。同时增强 readiness 和 404 缓存语义。
- **优点**：与任意当前、历史和未来客户端版本兼容；不需要版本清单、外部下载或客户端改动；资源路径继续走既有 Gateway 鉴权和缓存头。
- **缺点 / 风险**：每个资源首次加载需要经过 Node Connector，较全局 CDN 多一次 Relay 数据传输；浏览器 immutable cache 会限制重复成本。
- **影响面**：仅 `cloud/internal/gateway`、`cloud/internal/config`、`cloud/app`、`cloud/compat` 和 Cloud 部署文档。

### 方案 C：缺失全局 asset 时根据 Referer 回源 Node

- **做什么**：`/mindfs-assets/` 404 时从 Referer 推断 Node，并维护模块依赖 asset 到 Node 的运行时映射。
- **优点**：命中当前 Cloud bundle 时继续使用共享缓存。
- **缺点 / 风险**：依赖 Referer 和模块加载链，隐私策略、多标签页、嵌套动态 import 都会增加不确定性；状态复杂且难以证明完整兼容。
- **影响面**：assets handler、Gateway/Registry 耦合、运行时映射和并发控制。

### 推荐方案

**采用方案 A**。官方 `relay.a9gent.com` 对 `v0.4.4`、`v0.4.5`、`v0.4.6` 的不同 hashed JS/CSS 均返回 `200`，并统一设置一年 `immutable` 缓存，证明官方使用的是多 release 资源并集而非回源 Node。方案 A 精确复刻该协议和部署语义；用户已明确要求以后以客户端源码、release 产物和官方响应三方证据为准，因此方案状态确认为 approved。
