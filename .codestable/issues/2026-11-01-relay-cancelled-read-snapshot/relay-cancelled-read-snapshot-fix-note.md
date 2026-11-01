---
doc_type: issue-fix
issue: relay-cancelled-read-snapshot
path: fast-track
status: verified-locally
fix_date: 2026-11-01
tags: [cloud-relay, sqlite, wal, cancellation, node-not-found]
---

# Relay 新节点间歇性 node_not_found 修复记录

## 1. 问题描述

同一节点地址间歇性返回 `node_not_found`，旧节点正常，新节点已成功写入数据库并可建立 Connector。用户提供的线上并发检查为 32 次中 19 次成功、13 次失败；新节点只存在于 WAL 合并视图。该线上现象来自用户提供的诊断，本次没有连接生产进程。

## 2. 根因与证据

`cloud/go.mod` 原固定 `modernc.org/sqlite v1.34.5`（SQLite 3.46.0）。旧驱动的 `stmt.query()` 在结果已经生成、返回前收到 context 取消时，会将结果清为 nil，却未关闭它，留下未完成语句和隐式读事务。该连接继续读取旧 WAL 快照，无法看到之后提交的新节点。

`GetNode` 和 `ListNodesByOwner` 使用独立只读池（最多四个连接）；`AuthenticateDeviceToken` 使用写池。健康和泄漏连接混用，解释了相同地址时好时坏，以及 Connector 认证正常但网页查询失败。

临时库诊断直接观察到问题连接残留 `busy_statements=1`、`transaction_state=1`，同时 `autocommit=1`；因此 autocommit 不能证明没有隐式读事务。只在临时复制的旧驱动取消分支增加 `r.Close()` 后，原诊断的 64,000 次新节点读取全部通过。

上游已确认并修复相同缺陷：

- [MR 81：取消查询时关闭已生成的 Rows](https://gitlab.com/cznic/sqlite/-/merge_requests/81)，首次发布于 v1.40.1。
- [MR 86：取消查询不应中断连接上的后续查询](https://gitlab.com/cznic/sqlite/-/merge_requests/86)，发布于 v1.42.2。
- [驱动 changelog](https://gitlab.com/cznic/sqlite/-/blob/v1.61.1/CHANGELOG.md)。

这是已复现并由对照实验证实的依赖缺陷。尚未从线上原进程直接提取残留语句；升级后的线上 A/B 验证仍是部署验收的一部分。

## 3. 修复方案与文件

| 文件 | 改动 |
|---|---|
| `cloud/go.mod`、`cloud/go.sum` | 升级到 sqlite v1.61.1 / SQLite 3.54.1，使用其要求的 libc v1.79.0，同步所需间接依赖 |
| `cloud/internal/store/sqlite_cancellation_test.go` | 四个 worker 并发取消 12,000 次真实节点查询，然后在 WAL 中提交新节点，逐一检查全部四个物理读连接及实际 GetNode/ListNodesByOwner 路径 |
| `cloud/compat/suite_test.go` | 无效配对测试与随后有效配对间隔三秒，符合当前未修改 Node 的配对限流，保证核心兼容场景可执行 |
| 本文件 | 根因、修复、验证与部署边界 |

回归测试仅使用临时数据库，通过公开驱动错误类型识别已取消 context 的 `SQLITE_INTERRUPT`；未取消请求的中断、提交后任何读取错误都会失败。测试不重建读池，不对新提交做 checkpoint，也不重试 `ErrNotFound`。没有向生产查询添加缓存、重试或连接寿命配置。

本次修改 Cloud 独立模块。根目录 Node 模块的依赖、IDEA 插件和现有协议保持原状。

## 4. 验证结果

| 检查 | 结果 |
|---|---|
| 相同新增回归测试 + 原 v1.34.5 依赖 | 20 轮失败；前 16 轮均抓到新节点不可见，后 4 轮因旧驱动泄漏耗尽测试进程文件句柄，不能将后四轮算作节点缺失样本 |
| 新驱动取消回归 `-count=50` | 50 轮通过，合计 600,000 次取消时序查询；所有后续物理连接读取和业务查询通过 |
| `go test -race ./internal/store -count=1` | 通过，包含旧库迁移、并发读写、新增取消回归 |
| 取消回归 `-race -count=10` | 10 轮通过 |
| `go test ./... -count=1` | Cloud 全量通过；含旧库幂等迁移、备份、节点重命名/禁用/删除、HTTP/WS 和部署脚本回归 |
| `go vet ./...`、`go mod verify`、`git diff --check` | 通过 |
| `MINDFS_RUN_COMPAT=1` 两个真实进程兼容测试 | 当前 Node API/编码路径通过；核心 HTTP/WS/E2EE/Relay 重启重连通过 |
| 旧驱动创建的真实文件 + 尚未合并 WAL | 主文件单独读取看不到新节点；新版成功读取完整节点、设备 Token、owner、登录 Session；在线备份成功，重复打开三次均保留数据，完整性检查 ok |
| 静态构建 | Linux AMD64、macOS ARM64 成功；二进制元数据确认 Go 1.26.6、sqlite v1.61.1、libc v1.79.0 |

证据和构建产物位于仓库忽略目录 `build/relay-sqlite-cancellation-fix-20261101/`。原始诊断与旧驱动对照日志位于相邻 IDEA 项目的 `build/reports/relay-node-read-diagnosis-20261101/`。所有数据写入和故障复现均在临时或合成测试库完成。

## 5. 线上部署与验收

截至本记录，代码修复和本地验证完成，**尚未部署到 relay.20260310.best**。用户明确负责部署；本次交付源码修复及验证记录。此次本地验证未构建或发布镜像，也未重启线上服务。

上线沿用 [部署文档](../../../cloud/deploy/README.md) 中现有部署类型对应的完整升级步骤：先在线备份，刷新并检查资源，再执行 validate/migrate，重建 Relay 容器。保留原数据卷、资产卷、备份卷、环境配置及反代网络。升级会关闭旧进程中的泄漏连接；新驱动消除再次触发的缺陷。

部署后应确认运行二进制/镜像包含新版 sqlite，再确认 healthz/readyz、资产检查及 Node 自动重连；A/B 均在线时，对两者现有节点 URL 做并发重复访问，并新建一次节点验证新提交可见。无需删除节点、数据库或重新绑定来完成本修复。
