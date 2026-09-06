---
doc_type: audit-index
audit: 2026-09-06-relay-upstream-compatibility
scope: 2d76ff9..3471043 上游更新与现有自研 Cloud Relay 的兼容性
created: 2026-09-06
status: active
total_findings: 3
fixed_findings: 1
open_findings: 2
---

# Relay 上游更新兼容性审计

## 2026-09-06 修复进度

finding-03 已在当前工作区修复，HTTP/WS 保留 EscapedPath，源码 Node 与正式 v0.5.0 的加密路径回归均通过。新增 `check-assets` 和 `cloud/deploy/refresh-assets.sh`，部署及 Node 独立升级前同步并验证 release 覆盖；正式 Linux v0.5.0 的 147 个资源已通过隔离导入验证。尚未部署或检查线上资源卷。

finding-01 / finding-02 仍为 open，受“上游客户端源码只读”的既有约束，本轮没有修改 CLI/Web。具体修复、测试、运维步骤及这两项的处理边界见 [修复记录](../../issues/2026-09-06-relay-release-compatibility/relay-release-compatibility-fix-note.md)。以下保留修复前的审计过程和当次运行态证据。

## 最近一个月发布与接口补充核验

已补查 **2026-08-06 至 2026-09-06** 的上游发布、最新源码、官方公开 HTTP 响应和 v0.5.0 真实发布包：4 个正式版本、55 个提交、11 个新增 Node API 方法/路径组合。当前 fork 已包含最新上游，`web/`、`server/`、`cli/` 与 `upstream/main` 内容一致。

新增业务 API 的实现位于 Node；Cloud 能转发。正式发布包验证了 9 个新增接口的成功路径、2 个额度接口的预期业务错误返回，以及 147 个静态资源的传输完整性。编码路径缺陷在补充审核时复现，后续已按上节修复；新版本静态资源仍需在部署环境同步。详情、版本清单与验证边界见 [发布与接口适配补充](release-followup.md)。原 3 项发现保留编号。

## 本机运行态补充（后续已核验）

用户指出本机正在正常使用后，进一步核验了实际安装产物与配置：运行的是 `~/.mindfs/mindfs` **v0.5.0**，自 2026-09-05 起以 `-foreground -addr 127.0.0.1:7331` 持续运行；自研 Relay 已绑定，**E2EE 未开启，内建 autostart 未启用**。Chrome 存在本机直连，本地实际 JS/CSS 均返回 200。

**因此 finding-01 与 finding-03 的关键触发条件在当前本机不成立；finding-02 的双节点切换场景未验证，远端资源缺失也没有证据。下文是源码层面的条件性发现，不应视为用户当前服务故障或必须停机升级的理由。** 今天 Git pull 没有替换该安装产物或重启现有进程。完整安全证据与限制见 [本机运行态核验](local-runtime.md)。

## 范围与基线

- 用户自研 Relay 已在线稳定运行约一个月；本次评估是否可以继续服务更新后的上游节点与前端。
- 更新区间：`2d76ff9..3471043`。审计时 `HEAD=3471043`，本地 `main` 与 `origin/main` 一致。
- 范围：`cloud/` 协议、相关测试与部署资源契约；`server/internal/relay/`、节点 HTTP/WS/E2EE；`web/` 中影响 Relay 的请求、资源与缓存变更；新增 CLI 自启动的 Relay 配置传递。
- 只检查、不修复。业务源码、现有运行服务、生产数据库及配置未修改；不执行部署或现有服务重启。定向复现写在 `/tmp/mindfs-relay-audit.L1Pbt6/`，其中 gateway 测试通过 Go overlay 注入，没有在业务目录落盘。
- 已读 `.codestable/attention.md`；暂停的 `relay-local-service-domains` 不纳入整改，也不要求 wildcard DNS/TLS 或 OpenResty。
- 这是限定范围的兼容性审计，不是整个应用的安全、性能或架构全面审计。

## 总评

**核心 Relay 协议兼容，有真实进程测试支持；但新版存在两项条件性周边回归，另确认一项原有 E2EE 编码路径缺陷，不能将核心测试 PASS 解读为整个新版可无条件上线。**

`git rev-parse 2d76ff9:cloud 3471043:cloud` 两次均为：

```text
2e2ed9ca457065ee41154d06cd701ae27fa63e91
```

因此 Cloud 源码、独立依赖和 schema 在此次更新中完全未变。已执行的重型测试构建当前 Cloud 与当前节点；由于 Cloud tree 相同，它验证了 **更新前 Cloud 源码等价实现 + 更新后节点** 的组合，不代表已经比对线上镜像、配置、数据库或静态资产目录。

## 已验证兼容的部分

| 项目 | 证据与结论 |
|---|---|
| 绑定、凭据与节点连接 | `server/internal/relay/service.go:184` 保留绑定轮询契约，`:331-350` 仍为 Bearer Device Token + WebSocket + yamux.Client；`cloud/internal/connector/handler.go:41-82` 对应鉴权与 yamux.Server。凭据存储、Manager、WS adapter 无区间改动。 |
| 唯一 Connector 改动 | `server/internal/relay/service.go:346` 新增 `MaxStreamWindowSize = 4 << 20`，不是更换帧协议。双方仍用 yamux v0.1.2，其 WindowUpdate 动态通知接收窗口，不要求两端配置值相同。真实兼容测试通过；未做大流量性能压测。 |
| HTTP 与应用 WebSocket | Cloud 仍按 `/n/{nodeId}/` 转发，保持 method/query/body 和业务 header；WS data/close 帧格式不变。新增 `event_cursor` 属于节点与前端的应用消息字段，Cloud 不解析它。 |
| 新增业务 API | 内存、额度、偏好、提示词删除等 API 均是节点 API，经原有 `protectedJSON(appPath/appURL(...))` 封装转发，不要求 Cloud 新增业务端点。 |
| E2EE 核心 | 浏览器 E2EE 实现和节点加密包无区间改动；真实 P-256/HMAC/HKDF/AES-GCM 握手、加密 HTTP 和加密 WS 已通过。编码路径另见 finding-03。 |
| Cloud 重启恢复 | 测试重启的是隔离的临时 Cloud 子进程；节点不重启、不重新绑定，健康路由恢复。没有操作线上 Cloud。 |

## 发现清单

| # | 性质 | 严重度 | 置信度 | 新旧属性 | 标题 |
|---|---|---|---|---|---|
| 1 | bug | P1 | high | 此次新增 | [新版自启动丢弃自定义 Relay 环境变量](finding-01.md) |
| 2 | bug | P1 | medium | 此次新增 | [同域名路径式多节点串用会话列表缓存](finding-02.md) |
| 3 | bug | P2 | high | 原有缺陷，本次仍存在 | [Gateway 丢失部分转义路径，E2EE 请求签名失配](finding-03.md) |

`high` 的两项已进行隔离定向复现；缓存问题有完整静态调用链证据，但未实际运行双节点浏览器场景，因此定为 `medium`。没有将前端依赖缺失或原有文案测试失败计为 Relay 协议缺陷。

### 严重度 × 性质

| 性质 | P0 | P1 | P2 | 合计 |
|---|---|---|---|---|
| bug | 0 | 2 | 1 | 3 |
| security | 0 | 0 | 0 | 0 |
| performance | 0 | 0 | 0 | 0 |
| maintainability | 0 | 0 | 0 | 0 |
| arch-drift | 0 | 0 | 0 | 0 |
| **合计** | **0** | **2** | **1** | **3** |

以上零项表示本次未报告该类兼容性发现，不代表做过全量安全/性能审计。

## 单独的部署兼容条件：前端静态资源

**新增 Node 业务 API 不要求 Cloud 逐条增加处理器；编码路径仍需按 finding-03 修复，上线新节点/前端前还需确认 Cloud 能提供该构建对应的静态资源。**

- `server/internal/api/http.go:1628-1665`：标准 release 版本（例如 `v1.2.3`）经 Relay 访问时，把 `./assets/` 改成 `/mindfs-assets/`。
- `cloud/app/operations_handlers.go:31-55`：该入口只从 Cloud 本地资产根读取；缺少文件直接 404，不会自动回源节点。
- `cloud/deploy/docker-compose.yml:8-20`：`asset-sync` 是 `restart: "no"` 的一次性步骤，不是持续同步任务；`:34-36` Relay 只读挂载资产卷。
- 所以如果新节点引用的 JS/CSS hash 不在现有资产卷，页面会加载失败，即使 Connector 正常在线。
- 本次没有读取线上 asset volume 或其 hash 清单，不能声称已经触发该问题，也不能声称无须同步资源。
- 重型 compat 使用临时 fixture，而不是实际新 Web bundle，不能证明线上资产齐全。

## 本地验证结果

### 1. 节点 Relay / E2EE / API：通过

```sh
env -u MINDFS_RELAY_BASE_URL -u MINDFS_INTERNAL_RESTART \
  go test -mod=readonly ./server/internal/relay ./server/internal/e2ee ./server/internal/api \
  -count=1 -timeout=180s
```

三个包均 PASS。首轮未剔除 shell 的 `MINDFS_RELAY_BASE_URL`，导致四个 Relay 测试使用的预期地址被覆盖；隔离后全部通过。这是测试环境污染，非协议回归。没有修改用户环境或真实凭据来使测试通过。

### 2. Cloud 普通测试：12 个包通过

在 `cloud/` 目录执行：

```sh
env -u MINDFS_RELAY_BASE_URL -u MINDFS_INTERNAL_RESTART \
  MINDFS_RUN_COMPAT=0 GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go test ./... -count=1 -timeout=180s
```

注意此命令跳过需要显式开启的重型进程场景；该场景已另行运行，不以 skip 当通过。

### 3. 真实 Cloud + 真实最新节点：通过

在 `cloud/` 目录执行：

```sh
env -u MINDFS_RELAY_BASE_URL -u MINDFS_INTERNAL_RESTART -u MINDFS_COMPAT_NODE_BINARY \
  MINDFS_RUN_COMPAT=1 GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly \
  go test ./compat -run '^TestUnmodifiedNodeRelayCompatibility$' -count=1 -v -timeout=180s
```

`TestUnmodifiedNodeRelayCompatibility` PASS，场景耗时 10.22 秒。覆盖：临时账户密码登录、绑定确认、Connector 在线、HTTP/fixture 静态资源、E2EE 握手、加密 POST、加密 WS、Cloud 重启后节点重连、测试进程清理及端口释放。入口：`cloud/compat/suite_test.go:15-71`。

### 4. 前端现有测试：10 通过，1 原有失败

在 `web/` 目录执行 `node --test tests/*.test.mjs`。

唯一失败：`web/tests/agent-lifecycle-restart.test.mjs:33-36` 要求英文文案精确为 `Switch and restart Agent config`，实际为 `Agent config switch & restart`。`2d76ff9` 中实际文案已如此，非本次新增，更不是 Relay 传输失败。

最初从仓库根运行导致三个使用 cwd 的测试找不到 `src/services/*`；改为正确工作目录后这三项通过。没有修改测试来消除失败。

### 5. 前端类型检查：未通过，缺少已声明依赖

`npm --prefix web run typecheck` 返回 exit 2：

```text
Cannot find module 'pdfjs-dist'
Cannot find module 'docx-preview'
Cannot find module 'read-excel-file/browser'
Cannot find module 'pptx-preview'
```

同时出现由缺失类型引起的隐式 any 报错。对应 `DocumentViewer.tsx`、`web/package.json` 和 `web/yarn.lock` 在区间内未改动。本次未安装依赖、未修改锁文件，也未宣称前端全量构建成功。

### 6. 定向缺陷复现：两个断言按预期失败

- CLI 自启动：在临时目录复制当前 `autostart.go` / `autostart_unix.go`，临时 HOME 和模拟 shell 都提供 `MINDFS_RELAY_BASE_URL=https://relay.audit.invalid`，调用真实 `prepareAutoStartEnvironment()` 后值为空，确认 finding-01。
- Gateway：仅通过 `go test -overlay=...` 注入内存 HTTP 序列化测试，调用真实 `parseNodeRoute` / `cloneRequest`；`%3A` 在到达节点前变为 `:`，确认 finding-03。
- 这两个失败用于复现缺陷，不是通过的回归测试，不计入上述 PASS。

## 验证边界

未验证：线上实际部署版本与 Git baseline 的对应关系、生产 TLS/WSS/反向代理/SMTP、旧云资产卷、真实浏览器/PWA 缓存隔离、实际文件上传下载、大流量与长时间稳定性、完整业务 agent 对话。测试中的 Cloud 重启恢复只重测健康路由，不证明原先 E2EE session/WS 已恢复。没有验证“只换前端、仍用旧本地节点”的混合版本组合。

## 下一步建议

1. **上线前先确认资产兼容条件**：新 bundle 的 hash 文件是否已经存在 Cloud `/mindfs-assets/` 的资源目录中；本次未执行同步或部署。
2. **P1 / 新版自启动**：若 Relay 地址依赖 `MINDFS_RELAY_BASE_URL`，先处理 finding-01，再采用新 `--autostart`。用户当前 shell 确实设置该变量，但未核实线上启动方式，不能声称已经受影响。沿用现有启动配置不自动触发此问题。
3. **P1 / 多节点缓存**：如使用同域名 `/n/A/` 与 `/n/B/`，建议通过 `cs-issue` 处理 finding-02；单节点场景不触发跨节点串用。
4. **P2 / 原有编码路径**：若遇到展开工具调用详情时重复要求密钥或 `e2ee_proof_invalid`，按 finding-03 开 `cs-issue`。不要将其误报为本次更新破坏全部 E2EE。
5. 前端完整验证仍需补齐依赖并另行处理原有文案断言；这不要求重写 Relay 后端。

本审计未自动修复、提交、部署或重启任何现有服务。
