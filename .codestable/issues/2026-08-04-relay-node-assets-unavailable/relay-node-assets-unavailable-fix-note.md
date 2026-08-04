---
doc_type: issue-fix
issue: 2026-08-04-relay-node-assets-unavailable
status: completed
path: standard
fix_date: 2026-08-04
related: [relay-node-assets-unavailable-analysis.md]
tags: [cloud-relay, asset-compatibility, deployment, multi-release]
---

# Relay Node Assets Unavailable 修复记录

## 1. 实际采用方案

采用 analysis 方案 A：保留未修改客户端既有的全局 `/mindfs-assets/` 契约，在 Cloud 后端实现官方式持久化多 release asset repository。

新增 `mindfs-relay sync-assets <source-dir> <target-dir>`：先合并当前镜像的 Web bundle，再通过 GitHub Releases API 回填 `v0.1.8` 起所有正式 release 的 `linux_amd64.tar.gz`。下载严格校验官方 size 与 SHA-256，只安全提取 `web/assets/`；content-hashed 文件只增不删，同名不同内容立即失败。每个 release 保存带文件 SHA-256 的完整性 marker，缺失 hashed 文件时会重新导入修复。

Compose 新增一次性 `asset-sync` 初始化服务和持久化 `relay-assets` volume；Relay 等同步成功后启动，并只读挂载合并后的资源集合。同时增强 readiness 的入口资源校验，并让所有 asset 404 返回 `Cache-Control: no-store`。

## 2. 改动文件清单

- `cloud/internal/assetsync/service.go`、`service_test.go`：release 发现、下载校验、安全提取、幂等合并、完整性 marker 与测试。
- `cloud/cmd/mindfs-relay/main.go`、`main_test.go`：新增 `sync-assets` 运维命令。
- `cloud/deploy/docker-compose.yml`、`deploy_test.go`：新增 init service、持久 volume、只读 Relay 挂载和启动依赖。
- `cloud/Dockerfile`：为 non-root 同步进程准备可写 asset volume 目录。
- `cloud/internal/config/config.go`、`config_test.go`：readiness 校验 `index.html` 实际引用的本地 JS/CSS。
- `cloud/app/operations_handlers.go`、`operations_handlers_test.go`：asset 404 `no-store`，并回归多 release hash 同时可读。
- `cloud/deploy/README.md`：记录首次部署、升级、持久 volume 和 HTTP 验证步骤。
- `cloud/go.mod`：将 readiness HTML parser 使用的 `golang.org/x/net` 标为直接依赖。
- `.codestable/attention.md`、architecture、audit 与本 issue 文档：固化客户端零修改和官方证据优先的兼容原则，回写当前实现。

## 3. 验证结果

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `docker compose config --quiet` 通过。
- 最终 `cloud/Dockerfile` 镜像构建通过。
- 在临时 Docker volume 中使用最终镜像真实同步官方 releases，未启动 Cloud 服务：首次 `current_files=157`、`releases_imported=25`、`assets_added=2184`、`assets_reused=1472`；第二次同步 `releases_imported=0`、`releases_present=25`、`assets_added=0`。
- 合并结果约 110.8 MiB，包含 25 个 release marker 和 2166 个 asset 文件；`index-C0gNCfj8.js`、`index-B4USfphH.js`、`index-DeNebQ9q.js` 同时存在，对应官方 `v0.4.4`、`v0.4.5`、`v0.4.6`。
- 官方 `relay.a9gent.com` 的上述三个版本主 JS 均返回 `200` 与 `Cache-Control: public, max-age=31536000, immutable`，实现语义与黑盒证据一致。
- Git 差异只涉及 `cloud/**` 与 `.codestable/**`；`web/`、`server/`、`cli/`、Android、Harmony 和其他客户端代码均未修改。

## 4. 遗留事项

- 按用户要求未在本机启动或部署 Cloud，也未运行需要启动真实 Cloud/Node 进程的 opt-in `MINDFS_RUN_COMPAT=1` 场景；默认 compatibility package 测试已通过或按设计跳过重型场景。
- 修复需要在 VPS 部署当前镜像后才会作用于 `relay.20260310.best`。部署时必须保留 `relay-assets` volume，等待 `asset-sync` 成功，再验证当前和历史 hash 的公网响应。
- 客户端 `index.html` 的强制 alert 保持不变；Cloud 通过多 release 资源完整性保证规避其触发条件，符合客户端零修改铁律。
