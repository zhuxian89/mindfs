---
doc_type: issue-fix
issue: 2026-09-06-relay-release-compatibility
path: standard
status: verified
fix_date: 2026-09-06
related: [relay-release-compatibility-report.md, relay-release-compatibility-analysis.md]
tags: [cloud-relay, e2ee, upstream-compatibility, assets]
---

# Relay 发布兼容修复记录

## 1. 实际采用方案

用户在发布兼容审核后授权“开始修复吧”，本次完成此前建议中的 Cloud 编码路径、资源同步检查和永久兼容回归。现有其他工作区改动全部保留，没有 commit、push、部署、线上同步或重启，也没有修改上游 Web/Node/CLI。

### 编码路径

HTTP 和 WebSocket Gateway 改为从 `URL.EscapedPath()` 切分节点路由，节点 ID 解码后校验，业务路径保留原始转义。转发同时设置 decoded `URL.Path` 和相匹配的 `RawPath`，原始 query/ForceQuery、HTTP method/body 和 E2EE headers 不变。拒绝节点 ID 中的编码斜线和非法转义，防止节点路由边界含糊。

修复前 `%3A` 被改为 `:`，官方 v0.5.0 Node 经 Relay 返回 401 `e2ee_proof_invalid`；修复后 `%3A`、`%3a`、`%2F`、`%252F` 均和直连 Node 得到相同的加密业务响应。不存在的测试会话返回加密 400，证明已通过验签并进入业务处理；没有用不存在的数据声称业务查询成功。

### 资源刷新与覆盖检查

- 新增 `mindfs-relay check-assets <target-dir>`：只读验证当前 index 入口、官方受支持正式 release 列表及每个 `.complete` marker 的 immutable 文件 SHA-256。缺版本、缺文件、内容改变、无匹配 archive、空发布列表或 GitHub 请求失败均返回非零。
- 同步和检查复用相同的发布过滤逻辑（正式版本、自 v0.1.8 起、分页）；空受支持列表不再被当成成功。沿用只增不删的历史 hashed 资源和同名不同内容失败策略。
- 新增 `cloud/deploy/refresh-assets.sh`：先校验 Compose 配置，再执行一次性 `sync-assets`，最后用只读挂载执行 `check-assets`。任一步失败立即停止，不启动依赖、不重建/停止 Relay，不迁移数据库。
- 部署文档改为 Cloud 发布及 Node 独立升级前运行同一个脚本。既有调度器可定期调用；本次未创建调度任务。定时刷新可能滞后于刚发布的版本，升级前仍需显式运行。
- 检查和 hash 读取只在显式运维命令中进行，不放入转发热路径或 `/readyz`，不增加常驻轮询 goroutine。

覆盖检查以现有 marker 记录的 immutable 资源为准；它不证明任意开发构建或同名非 hash 资源的所有历史内容均可同时提供。只读检查不会修复文件；同步能恢复缺失文件，遇到已有 immutable 文件内容冲突会失败，保留现场供运维处理。

## 2. 本轮文件清单

| 文件 | 改动 |
|---|---|
| `cloud/internal/gateway/http.go`、`websocket.go` | 编码路径保真与节点 ID 校验 |
| `cloud/internal/gateway/escaped_path_test.go` | HTTP/WS 各 10 个 request target 场景及非法输入 |
| `cloud/internal/assetsync/service.go`、`check.go` | 共享版本选择、只读覆盖检查 |
| `cloud/internal/assetsync/check_test.go` | 新发布缺失、增量补齐、历史文件缺失/损坏、查询错误与只读验证 |
| `cloud/internal/assetsync/release_compat_test.go` | 显式选择的正式 Linux archive 导入、全部资源内容比对、重复同步 |
| `cloud/cmd/mindfs-relay/main.go`、`main_test.go` | 运维命令接入与参数校验 |
| `cloud/deploy/refresh-assets.sh`、`refresh_assets_test.go`、`README.md` | 刷新脚本、失败即停止验证、部署/Node 升级步骤 |
| `cloud/compat/node_api_test.go`、`README.md` | 11 个新增 API、编码路径真实 Node 验证及运行方法 |
| `.codestable/architecture/cloud-relay-core.md`、本 issue 与关联 audit | 当前行为、修复状态、保留限制 |

`git status` 中其他修改来自此前安全/性能修复，本轮没有回退或重新归属这些改动。

## 3. 验证结果

| 检查 | 结果 |
|---|---|
| Gateway/资源/命令/刷新脚本定向测试 | 4 个 package 通过 |
| Cloud 全量 `go test -race ./... -count=1` | 12 个 package 通过；最慢 Gateway 17.98 秒；重型兼容测试另行显式执行 |
| Cloud `go vet ./...` | 通过；新增正式资源测试后补查对应 package |
| 未修改源码 Node：`TestCurrentNodeAPICompatibility` | PASS，6.62 秒 |
| 未修改源码 Node：`TestUnmodifiedNodeRelayCompatibility` | PASS，6.23 秒 |
| 官方 v0.5.0 Darwin ARM64 Node：新增 API/编码路径 | PASS，4.17 秒 |
| 官方 v0.5.0 Darwin ARM64 Node：核心 HTTP/WS/E2EE/重连 | PASS，5.20 秒 |
| 官方 v0.5.0 Linux AMD64 资源：带 race 的导入验证 | PASS，4.59 秒；147 个文件与归档内容一致，覆盖检查和重复增量同步通过 |
| `/bin/sh -n refresh-assets.sh`、文档 YAML/链接、`git diff --check` | 通过 |

新增 API 覆盖：9 个新增方法/路径成功返回（内存、空会话释放、三组偏好读写、提示词删除），2 个额度接口返回预期的加密业务错误（不存在的 agent / 缺少 idempotency key）。不使用真实 Codex 账户或重置额度，不运行 Agent。

路径场景还包括根路径、Unicode、空格、百分号、重复 query、query 编码及空 query；HTTP/WS 序列化验证 request target 完全一致。现有大消息、慢客户端、截断和 Cookie 过滤回归仍通过。

正式 Linux archive 的 GitHub SHA-256：`2ac0f269a574b77219454c21a93c97b9e9d881810ef6b96fcd62bb4150f7f14d`，大小 10,725,167 字节。导入测试在 loopback 提供下载好的 archive，保留官方 metadata 的原始 size/digest；不是合成发布包。测试只验证此实际 release，不将单版本覆盖冒充线上全版本覆盖。

验证环境为已安装 Go 1.26.6，`GOTOOLCHAIN=local`、`GOFLAGS=-mod=readonly`、`GOPROXY=off`、`GOSUMDB=off`、`GOMAXPROCS=4`。Node 子进程使用临时 HOME、项目、配置、数据库、端口及无效外网代理；未改用户运行配置。正式 Node 下载/校验使用公开 release，不访问用户云端数据。

测试日志归档于本目录 `evidence/`；运行材料目录为 `/var/folders/gc/tm9k8ng57hg_r71x5dq1c1j00000gn/T/mindfs-release-fix-dx_v07kl/`。

## 4. 部署与遗留事项

代码验证已完成，线上尚未生效。按 [部署文档](../../../cloud/deploy/README.md) 的完整升级顺序构建新版镜像、运行刷新脚本、validate/migrate，再更新 Relay；保留全部 volumes 和当前 Cloudflare/1Panel/OpenResty/Docker 网络安排。后续只更新 Node 时，先执行 `cloud/deploy/refresh-assets.sh`，无需为资源同步重启 Relay。

原 [finding-01](../../audits/2026-09-06-relay-upstream-compatibility/finding-01.md) 与 [finding-02](../../audits/2026-09-06-relay-upstream-compatibility/finding-02.md) 继续 open，不能标成 Cloud 修好了客户端：

- **CLI autostart**：依然过滤 `MINDFS_`。可沿用既有启动方式；若后续采用新 autostart，需在现有完整 agent 配置中持久设置根字段 `relayBaseURL`，并以绝对路径 `--agent-config` 启动（该参数由 autostart 保留），或由服务管理器显式注入 Relay 环境变量。本次仅记录规避方法，没有修改真实配置或验证完整开机启动。
- **Web 同域多节点缓存**：仍需上游按节点隔离 IndexedDB。单节点不触发；上游修好前，同域多节点可分别使用独立浏览器配置文件隔离本地缓存。这是临时使用边界，未代替前端修复，也未宣称双节点 UI 已通过验证。

以上保留来自项目“未修改客户端源码”的既有约束（`.codestable/attention.md` 及 architecture 第 4 节），不是等待额外许可才停下 Cloud 修复。暂停的本地服务域名和用户拒绝的剩余 Cookie 隔离未重新启用。
