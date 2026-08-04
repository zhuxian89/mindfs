---
doc_type: audit-finding
audit: 2026-08-04-node-upgrade-recovery
finding_id: "bug-02"
nature: bug
severity: P1
confidence: high
suggested_action: cs-issue
status: resolved
---

# Finding 02：线上 Cloud 资源包与当前 v0.4.6 release 不一致

## 速答

线上 Node 返回的页面引用官方最新 `v0.4.6` 主文件 `index-DeNebQ9q.js`，但线上 Cloud 对该文件返回 404；官方安装包和当前源码构建出的 Cloud Web stage 都包含该文件，因此故障点在 VPS Cloud 运行中的资源包，而不是 Node 版本。

## 关键证据

- `cloud/Dockerfile:3-8,20` — 标准 Cloud 镜像会执行完整 Web build，并把整个 `/src/web/dist` 复制到 `/opt/mindfs/web`；按当前源码构建时 `index-DeNebQ9q.js` 确实存在。
- `cloud/deploy/docker-compose.yml:3-6,15-17` — Compose 从仓库根目录构建该 Dockerfile，且 assets 目录没有被 volume 覆盖；正确重建的容器应直接携带完整 bundle。
- 官方最新 release 是 `v0.4.6`（tag commit `391612cc3e15aa64ab6e433843f8bd91a1698e9a`），其 Windows 安装包同时包含 `web/index.html`、`index-DeNebQ9q.js` 和 `index-CnvDg9WU.css`；`v0.4.6..HEAD` 的 `web/` 无源码差异。
- 线上 Node HTML 引用 `/mindfs-assets/index-DeNebQ9q.js` 和 `/mindfs-assets/index-CnvDg9WU.css`；前者 404，后者 200，`/readyz` 同时返回 200。加查询参数绕过已有缓存后，JS 仍由源站返回可缓存的 404。

## 影响

当前 `v0.4.6` Node 无法加载 Web UI，页面错误提示误导用户重新安装客户端。升级或重装同一个最新版本不会补齐 Cloud 中缺失的文件，因此可能反复失败。

## 修复方向

在 VPS 上从当前 commit 对 `relay` 执行无缓存重建并强制重建容器，先在容器内确认 `/opt/mindfs/web/assets/index-DeNebQ9q.js` 存在，再确认公网 URL 返回 200；若 Cloudflare 仍返回旧 404，再清除此 URL 的边缘缓存。

## 建议动作

`cs-issue`，因为这是已确认的线上部署产物不一致，需要恢复并补部署完整性验证。

## 修复结果

Compose 现以 `asset-sync` 初始化服务把当前镜像 bundle 与官方 release assets 合入持久化 `relay-assets` volume，Relay 只读挂载该 volume。`/readyz` 同时校验 `index.html` 实际引用的入口 JS/CSS，缺失当前入口资源的容器不再被判为 ready。代码修复已完成，生产生效仍需在 VPS 部署当前版本。
