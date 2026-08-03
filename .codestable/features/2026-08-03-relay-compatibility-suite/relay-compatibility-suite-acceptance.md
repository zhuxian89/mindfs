---
doc_type: feature-acceptance
feature: 2026-08-03-relay-compatibility-suite
status: passed
summary: 未修改 MindFS CLI 的真实进程兼容套件已覆盖绑定、HTTP、静态重写、E2EE、WebSocket 和 Cloud 重连
tags: [mindfs, cloud, relay, acceptance, compatibility, black-box, e2ee, websocket]
---

# Relay 黑盒兼容测试套件验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-08-03
> 关联方案 doc：`.codestable/features/2026-08-03-relay-compatibility-suite/relay-compatibility-suite-design.md`

## 1. 接口契约核对

**接口示例逐项核对**：

- [x] 默认 `go test ./...` 不运行重型子进程，入口在 `cloud/compat/suite_test.go:15` 明确 skip。
- [x] `MINDFS_RUN_COMPAT=1 go test ./compat -run TestUnmodifiedNodeRelayCompatibility -v` 从当前 checkout 构建真实 Cloud 和 release-version CLI。代码：`cloud/compat/scenario_test.go:144`。
- [x] `MINDFS_COMPAT_NODE_BINARY` 存在时跳过 Node build；预构建 binary 实测通过同一完整场景。代码：`cloud/compat/scenario_test.go:150`。
- [x] `BindObservation` 对应 `bindObservation`，只在内存保存 code、pairing secret 和 node name。代码：`cloud/compat/scenario_test.go:26`。
- [x] `E2EESession` 对应 `e2eeSession`，保存 client ID、E2EE node ID 和 32 字节 transport key，结束时清零。代码：`cloud/compat/e2ee_test.go:30`。

**名词层“现状 → 变化”与流程图核对**：

- [x] Compatibility Suite、Test Cloud、Test Node、Compatibility Run、E2EE Test Client 和 Process Harness 均有代码落点，没有进入生产二进制。
- [x] Preflight → Build → Cloud → Node → Bind → HTTP → E2EE HTTP → E2EE WS → Restart → Cleanup 的线性流程完整落在 `cloud/compat/suite_test.go:15`。
- [x] 测试不 import 根 module 或 `server/internal/**`；E2EE 算法由测试侧独立实现。证据：import grep 与 `cloud/compat/e2ee_test.go:240`。

## 2. 行为与决策核对

**需求、关键决策与编排约束**：

- [x] Cloud 和 Node 都是 `exec.Cmd` 启动的真实二进制，使用真实 TCP 和临时磁盘状态。代码：`cloud/compat/process_test.go:40`、`cloud/compat/scenario_test.go:166`。
- [x] Test Node 固定使用 `-foreground -e2ee -web-push=false -bind-relay -addr`，未修改 CLI 参数或源码。代码：`cloud/compat/scenario_test.go:204`。
- [x] 当前 checkout Node 构建注入 `v0.1.0`，覆盖 release 静态重写路径；binary override 复用相同测试逻辑。
- [x] E2EE Test Client 独立实现 P-256、HMAC-SHA256、HKDF-SHA256 和 AES-GCM，并验证 server proof。
- [x] Cloud 重启复用地址、DataDir 和 Token Key，Node 不重启、不重新确认绑定。代码：`cloud/compat/scenario_test.go:193`。
- [x] 每次 run 使用独立 HOME、XDG config、Cloud data、root、fixture、binary 和 loopback 端口；HTTP client 禁用代理。
- [x] 子进程环境只继承临时目录/locale 等安全键，不继承宿主 API key；Node PATH 指向空临时目录并配置拒绝外网的代理。
- [x] 所有阶段受总时限和阶段时限控制；成功、失败和 cancel 均由 defer 关闭 Node、Cloud、WS reader 和临时目录。
- [x] 失败信息只包含 stage、有限状态和脱敏日志；bind status 超时不会把 code URL 写入错误。

**明确不做与挂载点反向核对**：

- [x] 未修改 `server/`、`web/`、`cli/`、`android/`、`harmony/`、根构建文件或上游测试。
- [x] 未执行 Agent、会话、Git 或任务工作流；收紧 PATH 后真实 Agent 程序不可见。
- [x] 未访问官方 Relay、Token Station、Hosted Agents 或其他外部服务。
- [x] 未新增浏览器自动化、Docker、TLS 反代、readyz、备份、历史版本矩阵、生产路由、schema 或配置键。
- [x] 挂载点只包含 `cloud/compat/`、`MINDFS_RUN_COMPAT`、`MINDFS_COMPAT_NODE_BINARY` 和 `cloud/compat/README.md`。
- [x] 反向 grep 没有发现清单外引用；删除 `cloud/compat/**` 后测试能力与两个环境变量完全消失。
- [x] 生产 header 缺陷通过独立 issue 修改 `cloud/internal/gateway/**`，没有混入测试实现或修改客户端。

## 3. 验收场景核对

### 正常场景

- [x] **S1-S3**：默认 skip；显式运行构建两个 binary；Cloud/Node health、pairing secret 和 bind URL 在 deadline 内可观察且不输出原值。
- [x] **S4**：管理员登录并确认后，未修改 Node 保存凭据、建立 Connector，Public Route `/health` 返回 `ok`。
- [x] **S5-S6**：fixture index/asset、GET query、POST method/body 经 Public Route 到达 Node；release index 把 `./assets/` 改写为 `/mindfs-assets/`。
- [x] **S7-S8**：E2EE open 验证 server proof并派生 32 字节 key；带 canonical path proof 的 protected POST 返回可解密 JSON。
- [x] **S9**：加密 WebSocket ping 收到相同 request ID 的加密 pong；测试跳过不相关初始事件。
- [x] **S10**：Cloud 复用地址、SQLite 和 Token Key 重启后，Node 不重新绑定自动重连。
- [x] **S11**：`MINDFS_COMPAT_NODE_BINARY=/tmp/mindfs-compat-node` 实测完整场景通过。

### 边界与错误场景

- [x] **S12-S18**：随机端口、流式 stdout parser、重复 poll、重连重试、WS 初始事件过滤、失败清理和 context cancel 均有 deadline/cleanup 代码路径；结束后两个监听端口可重新 bind。
- [x] **S19-S23**：build、Cloud health、Node bind、管理员 API 和 Connector wait 失败均按阶段停止后续步骤并返回脱敏摘要或最后状态。
- [x] **S24-S27**：E2EE proof/envelope、WS handshake/pong、Cloud reconnect 和 binary override 缺失/不兼容都有独立失败阶段，不以重新绑定或原始敏感输出掩盖问题。

### 反向核对

- [x] **S28-S34**：上游路径 git diff 为空；`cloud/compat` 无 root internal import；只使用 loopback；子进程不继承宿主凭证；日志脱敏单测通过；PATH 不含真实 Agent；无生产能力扩张；`X-MindFS-Relayed` 缺陷已有 fast-track fix-note。

## 4. 术语一致性

- `Compatibility Run`：唯一入口为 `TestUnmodifiedNodeRelayCompatibility`，没有第二套场景编排。
- `Test Cloud` / `Test Node`：仅用于 subprocess spec 名称和文档，不与生产类型冲突。
- `Process Harness`：`managedProcess`、`processSpec` 和 `compatibilityRun` 职责分别为进程、启动描述和场景状态。
- `E2EE Test Client`：`e2eeSession` 与协议函数全部位于 `cloud/compat/e2ee_test.go`。
- 防冲突 grep 未出现 `mindfs/server/internal`、`mindfs/server/app` 或根 module import。

## 5. 架构归并

- [x] `.codestable/architecture/cloud-relay-core.md` 新增“黑盒兼容验证”，写入真实进程边界、运行命令、binary override、独立 E2EE、隔离/脱敏和已回归 header 契约。
- [x] 架构 doc 保持运行时 Binding、Connector、Gateway 和 Store 结构不变，没有把 Compatibility Suite 写成生产子系统。
- [x] 已知约束记录 `/mindfs-assets/` 全局资源托管尚未进入当前 Cloud，交由后续部署基线处理。

## 6. requirement 回写

- [x] `mindfs-compatible-cloud-backend` 已是 `current`；本 feature 增加验证证据，没有改变 pitch、用户故事、能力边界或 `implemented_by`，无需更新 requirement。

## 7. roadmap 回写

- [x] `mindfs-cloud-relay-items.yaml` 中 `relay-compatibility-suite` 已从 `in-progress` 更新为 `done`。
- [x] roadmap 主文档 V0 子 feature 清单同步为 `done`，对应 feature 保持 `2026-08-03-relay-compatibility-suite`。
- [x] YAML 校验通过；`relay-deployment-baseline` 的两个依赖现已满足。

## 8. attention.md 候选盘点

- [x] 无候选。兼容测试命令和 binary override 已写入 `cloud/compat/README.md` 与架构文档；它们不是每个 feature 启动都必须知道的仓库陷阱。

## 9. 遗留

- 后续优化点：完整历史版本/操作系统/移动端矩阵留给 `cloud-api-lifecycle`。
- 已知限制：Compatibility Suite 验证 `/mindfs-assets/` 重写契约，但 Cloud 尚未托管该全局资源路径；部署与资源打包由下一项 `relay-deployment-baseline` 处理。
- 实现阶段顺手发现：`X-MindFS-Relayed` 值不兼容已通过 `.codestable/issues/2026-08-03-relayed-header-value/` 修复并验证，无未处理的范围外代码改动。

## 验证命令

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。
- `MINDFS_RUN_COMPAT=1 go test ./compat -run TestUnmodifiedNodeRelayCompatibility -v` 通过。
- `MINDFS_RUN_COMPAT=1 MINDFS_COMPAT_NODE_BINARY=/tmp/mindfs-compat-node go test ./compat -run TestUnmodifiedNodeRelayCompatibility -v` 通过。
