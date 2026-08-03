---
doc_type: feature-acceptance
feature: 2026-08-03-relay-deployment-baseline
status: passed
summary: Cloud Relay 已具备 assets、readiness、metrics、迁移、在线 SQLite 备份和 Docker/Caddy 部署基线
tags: [mindfs, cloud, relay, acceptance, deployment, docker, sqlite, backup, readiness, assets]
---

# Relay 部署基线验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-08-03
> 关联方案 doc：`.codestable/features/2026-08-03-relay-deployment-baseline/relay-deployment-baseline-design.md`

## 1. 接口契约核对

- [x] `Config.AssetsDir` 对应 required `MINDFS_CLOUD_ASSETS_DIR`，必须含 `index.html` 和 `assets/`。
- [x] `BackupResult` 含 source、destination、size；命令输出只含 destination 与 size。
- [x] Metrics 提供 method/status 维度的 count 与 duration，不接受 path/query/body 标签。
- [x] Ops Command 完整包含默认/显式 serve、validate、migrate、backup、healthcheck；未知参数在启动前失败。
- [x] `/readyz`、`/metrics`、`/mindfs-assets/{path}` 与 roadmap 4.13 契约一致。
- [x] 主流程 command dispatch → config → operation/serve 与 design Mermaid 节点均有代码落点。

## 2. 行为与决策核对

- [x] 默认无参数仍进入 serve，不破坏现有启动方式；所有其他 command 不启动 HTTP listener。
- [x] serve 保留自动迁移，migrate 可重复执行且不清空已有数据。
- [x] backup 使用 SQLite `VACUUM INTO`，禁止覆盖、禁止源目标相同、成功后 chmod 0600 并 integrity check。
- [x] healthz 不依赖 DB/assets；readyz 每次执行 DB ping 与 bundle 检查，失败返回 503。
- [x] assets 使用 `os.Root` 约束到 bundle `assets/`，拒绝空路径、目录、遍历、越界 symlink 和不存在文件。
- [x] metrics 复用已有 request log 的 method/status/duration 观察点，没有增加破坏 WebSocket Hijack 的 ResponseWriter 层。
- [x] Docker 多阶段构建现有 Web + Cloud，最终 distroless non-root，只允许 data/backup volume 写入。
- [x] Caddy 外部终止 TLS/WSS；Cloud 仍根据 `MINDFS_CLOUD_PUBLIC_URL=https://...` 生成 wss Connector endpoint。

**范围守护与挂载点**：

- [x] 五个挂载点均已落地：assets config、三个 HTTP endpoints、五个 Ops Command、Dockerfile、deploy 示例目录。
- [x] 未新增 Kubernetes/Helm/systemd/Terraform、PostgreSQL、Redis、应用内 TLS、自动备份调度或远端存储。
- [x] Relay Binding/Connector/Gateway/E2EE 协议与 SQLite schema 未改变。
- [x] 上游只读路径 git diff 为空；Web build 只在 Docker stage 设计中执行，不向 workspace 写 `web/dist`。

## 3. 验收场景核对

### 正常场景

- [x] **S1-S3**：完整配置 serve/validate/migrate 通过；新库和已有库重复 migrate 均成功。
- [x] **S4**：在线 backup 生成可查询 admin_sessions、integrity check 通过、权限 0600 的独立文件。
- [x] **S5**：healthcheck 对 ready 返回成功，对 503/不可达返回错误且不打印 body。
- [x] **S6**：metrics 包含 up、ready、request count/duration，自动测试确认不含 path/query/secret。
- [x] **S7**：existing asset 返回正确内容和 immutable cache；真实 compatibility suite 已验证 index 重写后的 `/mindfs-assets/app.js` 可加载。
- [x] **S8-S9**：Dockerfile/Compose/Caddy 静态契约测试通过，`docker compose --env-file .env.example config` 通过。

### 边界与错误场景

- [x] **S10-S12**：缺 index/assets 配置失败；移除 index 后 healthz 仍 200、readyz 503；unsafe asset 全部 404 且不泄露绝对路径。
- [x] **S13-S14**：源库保持打开时 backup 成功；已有 destination 拒绝且字节不变，父目录按 0700 创建。
- [x] **S15-S16**：原 10 秒 SIGTERM shutdown 保留；Caddy reverse_proxy 与 https public URL 的 wss 生成已有测试证据。
- [x] **S17-S22**：配置、SQLite、backup、healthcheck、command usage 和 Docker build 输入错误均有非 0/构建失败路径，不降级运行不完整服务。

### 反向核对

- [x] **S23-S28**：只读路径无 diff；cloud/go.mod 无 PostgreSQL/Redis/metrics server/TLS/调度依赖；日志/metrics/命令无 secret/query/body；无应用内 TLS/远端上传；Relay 协议/schema/client 均未修改。

## 4. 术语一致性

- Deployment Baseline 只指 V0 container/ops 能力；未与后续 observability/disaster recovery 混用。
- Ops Command 全部由同一 `mindfs-relay` binary 提供。
- Readiness Probe 与 healthz 语义在代码、部署 healthcheck 和文档中一致。
- SQLite Backup 只指 verified online snapshot；文档明确禁止复制 live db/wal/shm。
- Asset Bundle 只读且只暴露 `assets/`，不托管 Node index/PWA 根资源。

## 5. 架构归并

- [x] `architecture/cloud-relay-core.md` 已加入 Ops Command、探活、metrics、assets、backup 和 container stack 的当前结构与命令。
- [x] 删除“Cloud 尚未托管 /mindfs-assets”旧限制，改为当前已实现与剩余部署边界。
- [x] TLS 外部终止、non-root、可写 volume、backup 0600 和 readiness 语义已记录为稳定约束。

## 6. requirement 回写

- [x] Requirement 保持 `current` 和原始 pitch/用户故事不变；变更日志追加 V0 可部署能力实际落地。

## 7. roadmap 回写

- [x] `relay-deployment-baseline` 已从 `in-progress` 更新为 `done`，主文档同步 feature ID。
- [x] roadmap 标记 V0 三项全部完成；roadmap 仍为 active，因为 V1-V3 继续 planned。
- [x] roadmap 部署契约已补入 `MINDFS_CLOUD_ASSETS_DIR` 和 `/mindfs-assets/{path}`，YAML 校验通过。

## 8. attention.md 候选盘点

- [x] 无候选。容器必须从仓库根构建、Docker daemon 需求和 backup 命令均已在 `cloud/deploy/README.md` 与 architecture 中明确，不属于每个 feature 的通用启动陷阱。

## 9. 遗留

- Docker daemon 当前不可连接，因此本机未实际执行 image build/start smoke test；Dockerfile、Compose、Caddy 已通过自动静态测试和 Compose 解析，发布环境仍需补一次真实镜像冒烟。
- 自动备份调度、保留策略、远端存储与恢复演练属于 `cloud-backup-recovery`，不在 V0。
- Kubernetes、systemd 和多实例部署不在本 feature。

## 验证命令

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。
- `go build -o /tmp/mindfs-relay-v0 ./cmd/mindfs-relay` 通过。
- 当前 checkout 与 `MINDFS_COMPAT_NODE_BINARY` 两种 Compatibility Run 均通过。
- `docker compose --env-file .env.example config` 通过。
- Docker image build/start 未执行：本机 Docker daemon 不可连接。
