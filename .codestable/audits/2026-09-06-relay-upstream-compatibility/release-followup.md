---
doc_type: audit-release-verification
audit: 2026-09-06-relay-upstream-compatibility
created: 2026-09-06
status: current
scope: 最近一个月上游正式发布、新增接口及当前自研 Cloud Relay 的适配责任
---

# 最近一个月发布与接口适配核验

## 后续修复状态

用户随后授权修复。编码路径现已在工作区修好，并补齐永久 Node API/加密路径回归；资源同步新增独立刷新脚本和只读版本覆盖检查。源码与官方 v0.5.0 Node、官方 Linux 资源归档验证均通过，尚未部署。详情见 [修复记录](../../issues/2026-09-06-relay-release-compatibility/relay-release-compatibility-fix-note.md)。以下保留修复前的发布审核证据与当时结论。

## 结论

**上游确实增加了功能和接口，兼容性需要持续维护。当前新增业务接口由上游 Node 实现，自研 Cloud 不需要重复实现这 11 个接口；但仍需修复转发时的编码路径，并确认新版静态资源已同步。**

仓库包含三部分：`web/` 是前端，`server/` 是运行在用户电脑上的 Node 后端，`cloud/` 是自研 Cloud Relay。本次新增的额度、内存和偏好功能主要发生在前两部分。例如：

```text
Web GET /n/<nodeId>/api/agents/memory
  → Cloud 去掉节点前缀并转发
  → Node GET /api/agents/memory
  → Node 查询本机 AgentPool，返回内存数据
```

新增 API、JSON 字段及会话消息字段不会天然要求 Cloud 新增业务代码；HTTP 路径、响应状态、WebSocket 帧、E2EE 签名和静态资源契约仍然需要逐项验证。

本次只补充审计资料，没有修改 Cloud、Node、Web、CLI 业务代码，没有操作生产服务。保留用户暂停的本地服务域名事项和已拒绝的剩余 Cookie 隔离事项。

## 时间与源码基线

- 月度窗口：2026-08-06 00:00 至本次 2026-09-06 查询时，Asia/Shanghai。
- 窗口前最后一个上游提交：`22022e4229214e67de11f015e43d4852657b2950`。
- 拉取远端引用后的最新上游：`2a604a11392a495301f0cd5967d856c9e38d23eb`，2026-09-04，`fix: git diff render of line start with +/-`。
- 本地 HEAD：`34710433f94ed0301b39aea074411f0b559eb787`。
- `git rev-list --left-right --count HEAD...upstream/main` 返回 `15 0`；`git diff --quiet upstream/main HEAD -- web server cli` 返回 0。因此当前 fork 已包含最新上游，这三个目录内容一致。
- 窗口内上游共 55 个提交。只更新了 `upstream/main` 引用，没有执行 merge、checkout 或替换安装产物。
- 此处的源码对齐不能证明线上镜像、已安装 Node 或浏览器缓存也对齐。此前本机运行态检查仍以 [当次记录](local-runtime.md) 为准，本轮没有重查用户进程。

## 四个正式版本

以 GitHub Releases 的实际 `published_at` 和公开说明为准，不将 8 月 4 日的 v0.4.6 算进本月。

| 发布日期（北京时间） | 版本 | 与本次判断相关的更新 |
|---|---|---|
| 08-13 | [v0.4.7](https://github.com/a9gent/mindfs/releases/tag/v0.4.7) | 新手引导、自定义会话命名模型、Codex 周限/重置时间/可用重置次数 |
| 08-18 | [v0.4.8](https://github.com/a9gent/mindfs/releases/tag/v0.4.8) | 空闲会话资源释放（默认 72 小时）、DeepSeek Harness、新项目元数据位置设置 |
| 08-20 | [v0.4.9](https://github.com/a9gent/mindfs/releases/tag/v0.4.9) | CodeBuddy、开机自启、会话列表缓存和流断点同步、快捷提示词添加/删除 |
| 09-01 | [v0.5.0](https://github.com/a9gent/mindfs/releases/tag/v0.5.0) | 内存占用展示/手动释放、token 用量和缓存命中率、关闭自动命名、toolcall 编码路径 E2EE 修复 |

发布后主分支还有修复；源码比较覆盖到上述最新提交，正式产物验证使用 v0.5.0。

## 新增接口逐项归属

月度 diff 中新增 11 个 HTTP 方法/路径组合，未删除旧的显式 Node API 注册。注册证据：`server/internal/api/http.go:306-315,380-381`。前端使用 `protectedJSON(appPath/appURL(...))`；`web/src/services/base.ts:26-42` 自动附加 `/n/<nodeId>`，`cloud/app/app.go:161` 交给通用 Gateway。

| 功能 | 新增 Node 方法/路径 | Cloud 责任 | 正式 v0.5.0 经 Relay 验证 |
|---|---|---|---|
| 内存查看 | `GET /api/agents/memory` | 转发 | 200；解密并核对 `idle_hours` |
| 手动释放 | `POST /api/agents/release-idle` | 转发 | 200；空 AgentPool 返回 `released_sessions=0` |
| 会话命名偏好 | `GET`、`PUT /api/preferences/session-naming` | 转发 | 200；禁用自动命名后读取到 `disabled=true` |
| 空闲释放阈值 | `GET`、`PUT /api/preferences/idle-session-resource-release` | 转发 | 200；写入 24 小时后读取一致 |
| 新项目元数据位置 | `GET`、`PUT /api/preferences/new-project-meta-location` | 转发 | 200；写入 `home` 后读取一致 |
| 删除提示词 | `DELETE /api/prompts` | 转发 DELETE 与加密 body | 200；先保存临时提示词，再删除并检查列表为空 |
| Codex 额度 | `GET /api/agents/codex/rate-limits` | 转发 | 不存在的测试 agent 返回加密 502，解密得到 Node 的 `agent not configured` |
| Codex 重置额度 | `POST /api/agents/codex/rate-limit-reset` | 转发 | 空 payload 返回加密 400，解密得到 `idempotency_key required` |

共 9 个新增方法/路径验证了成功返回；2 个额度方法/路径验证了预期的业务失败返回和加密传输，**没有验证真实 Codex 额度读取或消耗重置次数**。测试没有安装 Agent、使用真实账户或连接模型供应商。额外的 `POST /api/prompts` 是既有接口，用于构造临时数据。

业务实现证据：`server/internal/api/http_agent_memory.go:23-61` 查询/释放本地 AgentPool；`http.go:1259-1398` 读写本地 preferences；`http_codex_rate_limits.go:15-60` 调用 Node 的 AgentPool/Codex；`http.go:634-651` 调用本地 PromptStore 删除。前端入口在 `agentMemory.ts`、`preferences.ts`、`codexRateLimits.ts`、`prompts.ts`。

其他契约变化：

- `server/internal/api/ws.go:52-55,1011-1018` 增加 `event_cursor` 并用于重放。Cloud 不解析业务 JSON，现有 WS 桥接不需随字段新增改码；本次没有模拟完整会话流断点恢复。
- `server/internal/relay/service.go:346` 将 yamux 流窗口设为 4 MiB。没有替换帧协议，真实 Connector/HTTP/WS 测试通过；本次没有做窗口容量压测。
- `server/internal/api/http.go:1698,1752,1781,1801` 调整缺失目录/文件的状态语义，`managedDirResponse` 增加 `meta_location`；上传增加 `agent_path`、会话增加 token/缓存统计。Cloud 透传状态/响应体，不重写这些业务字段。
- 新手引导状态位于浏览器存储，新增模型接入由 Node 驱动；不是新增 Cloud 存储或模型执行服务。

## 需要处理的适配事项

### 1. Cloud 编码路径缺陷：确实仍需改代码

沿用 [finding-03](finding-03.md)，性质 `bug`、严重度 `P2`、置信度 `high`，不重复编号。

当前 `cloud/internal/gateway/http.go:53,156-157` 使用已解码的 `URL.Path` 剥前缀并清空 `RawPath`，会把 `claude-task-list%3A1` 转为 `claude-task-list:1`。`server/internal/api/http.go:119-130` 则按收到的 `EscapedPath()` 验证签名。

本次用同一个 E2EE session、同一条编码路径访问官方 v0.5.0 Node：

```text
直连 Node → 加密 400，签名验证通过，进入业务处理（测试项目/会话不存在）
经过 Cloud → 401 e2ee_proof_invalid，未进入业务处理
```

因此这不是只有静态推测的缺口。上游 `6dacf9d`（8 月 31 日）修复的是 Node 直达路径，无法补回 Cloud 已丢失的原始编码。该问题在月度窗口前已存在，不能归因于本月新增接口，也不能将其泛化成全部 E2EE 不可用。

### 2. 新版 Web 资源：需要同步并核对覆盖

`server/internal/api/http.go:1628-1665` 在正式 release 经 Relay 访问时，把 `./assets/` 改成 `/mindfs-assets/`；`cloud/app/operations_handlers.go:31-55` 只读本地资源，缺少文件直接 404。

正式 v0.5.0 的入口引用：

```text
/mindfs-assets/index-Cfwl2jiA.js
/mindfs-assets/index-Cdk4SFKm.css
```

隔离测试让 Node 提供真实发布包 Web，而临时 Cloud 只装旧测试资源：入口 HTML 返回 200，但这两个文件返回 404。将实际发布包资源复制进临时 Cloud 后，**147 个 asset 文件全部 HTTP 200，并逐个通过 SHA-256 比较**。这验证了缺失条件和正常提供能力，没有执行生产同步。

现有 `cloud/internal/assetsync/service.go:73-152` 已能导入 GitHub 正式版本并保留资源；`cloud/deploy/docker-compose.yml:8-13` 的 `asset-sync` 是一次性任务（`restart: "no"`），常驻 Relay 不会因此定时查询新 release。应将资源同步和缺失检查纳入发布步骤，兼顾仍在使用的旧 Node 版本。本次没有验证线上卷、CDN 或反向代理中的资源是否齐全，也没有跑完整生产同步命令。

### 3. 新版 CLI/Web 的条件性问题

- [finding-01](finding-01.md)：`cli/cmd/autostart.go:42-58,156-172` 会筛掉 `MINDFS_` 环境变量；自定义 Relay 地址若只靠环境变量且无配置回退，启用新 autostart 会受影响。沿用已有启动方式不自动触发。
- [finding-02](finding-02.md)：`web/src/services/session.ts:1532-1534,1665-1669` 的会话列表缓存没有按 `/n/<nodeId>` 隔离；同域多节点可能先展示另一节点缓存。仍为静态证据，未补做双节点浏览器复现。

这两项需要在升级验收时检查触发条件，单纯在 Cloud 增加业务 handler 不能修复。它们也不是本次要重新启用本地服务域名的理由。

## 官方 Cloud 接口：区分既有差异与本月新增

2026-09-06 19:01（北京时间）只读、无凭据查询官方公开端点；响应摘要和原始 JSON 见 `release-evidence/`。没有调用绑定确认、消费额度或其他写接口。

| 官方端点 | 本次实际响应 | 源码历史与影响 |
|---|---|---|
| [`GET /api/tips`](https://relay.a9gent.com/api/tips) | 200，提示信息列表 | Node 在 4 月已调用；`server/internal/relay/tips.go:126-127` 明确允许 404/204 视为空列表。自研 Cloud 没有此能力时缺少运营提示，不阻断新业务接口。 |
| [`GET /api/token-station/userinfo`](https://relay.a9gent.com/api/token-station/userinfo) | 401，`authorization_invalid` | 7 月 10 日 `a292779` 已引入。Node 需要 Cloud 的独立 Token Station 凭据/服务；当前自研 Cloud 未实现，属于原有功能差异，本次无凭据查询不证明授权后完整行为。 |
| [`GET /api/versions/android`](https://relay.a9gent.com/api/versions/android) | 200，返回 Android v0.3.7 | `web/src/services/appUpdate.ts:4,91-97` 默认直接使用官方地址，可由构建配置覆盖。不是绑定自研 Relay 后必然访问自研 Cloud 的新增接口。 |
| [`GET /api/versions/harmony`](https://relay.a9gent.com/api/versions/harmony) | 404，`version_not_found` | 同上；该前端文件在 5 月已存在，官方当前也未提供 Harmony 版本。不能将官方此刻的 404 误判为自研后端漏适配。 |

`server/internal/relay/` 月度区间唯一源码改动是 yamux 窗口，未新增 Cloud 请求端点。手机更新服务文件在月度区间未修改。Web Push 由 Node 的 `server/internal/webpush/service.go:532` 发往订阅推送服务，本月没有因此要求 Cloud 新增 API。

本地服务发布接口在 6 月已引入，属于用户明确暂停的 `relay-local-service-domains`，本次不展开实施。

## 验证产物与限制

官方归档：[mindfs_v0.5.0_darwin_arm64.tar.gz](https://github.com/a9gent/mindfs/releases/download/v0.5.0/mindfs_v0.5.0_darwin_arm64.tar.gz)，10,385,067 字节。SHA-256 与 GitHub release asset 的 digest 一致：

```text
8dd6d5010eaa8940d4495f088ac2e8868d96e69aea4bb57725351b5f9e049ca2
```

实际发布 JS 包包含上表全部新增接口路径。Node 使用该归档原始二进制，Cloud 从当前工作区构建；HOME、配置、项目、数据库、资源和端口均在测试临时目录/loopback，未操作用户已安装 Node。子进程 PATH 不含 Agent，外部代理指向无服务 loopback 地址，Web Push 关闭。

| 验证 | 结果与口径 |
|---|---|
| 现有 `TestUnmodifiedNodeRelayCompatibility` + 官方二进制 | PASS，场景 5.97 秒；绑定、HTTP、fixture 资源、E2EE、加密 WS、临时 Cloud 重启重连 |
| 临时 overlay `TestAuditReleaseFeatures` | PASS，场景 4.45 秒；上述 11 个新增方法/路径、实际发布资源、编码路径缺陷的预期复现 |
| `TestAuditRelayPreservesEscapedProofPath` | FAIL，预期的缺陷复现；确认当前 Gateway 改变 `%3A`，不是通过的回归测试 |

**补充测试 PASS 表示断言成立，其中包含“现有编码路径会失败”和“缺资源会 404”的断言；不是这两个问题已经修好。** 测试脚本、摘要和日志归档在 `release-evidence/`；临时运行材料在 `/tmp/mindfs-upstream-month-exi1ox7v/`。最初补充测试把空提示词列表假定为 `[]` 导致脚本断言 panic；Node 实际返回 `null`，前端已有归一化处理，已修正临时脚本并重跑，未修改产品行为。

验证使用已安装 Go 1.26.6 的 bin 加入子进程 PATH，`GOTOOLCHAIN=local`、`GOFLAGS=-mod=readonly`、`GOPROXY=off`、`GOSUMDB=off`，并剔除外层 Relay 环境变量。首轮默认 Go 1.26.5 不满足 Cloud go.mod，属于工具链前置失败；最终日志均来自正确工具链。

未验证生产部署、真实模型对话、真实 Codex 账户额度、非空会话释放、所有偏好组合、浏览器 UI/PWA 缓存、Linux 正式二进制和全版本排列组合。没有把 Git 已合并等同于运行态已升级。

建议后续维护顺序：先核对正式版本资源覆盖；修复 Cloud 编码路径并加入永久回归测试；升级验收时检查 autostart 和多节点缓存条件。以后每个上游版本固定核对“release 说明 → Node/Cloud 接口 diff → 官方产物兼容测试”，再决定是否需要改 Cloud。
