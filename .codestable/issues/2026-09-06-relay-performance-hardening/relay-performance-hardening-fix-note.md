---
doc_type: issue-fix
issue: 2026-09-06-relay-performance-hardening
status: verified
path: standard
fix_date: 2026-09-06
related: [relay-performance-hardening-report.md, relay-performance-hardening-analysis.md]
tags: [relay, performance, memory, sqlite, websocket]
---

# Relay 性能改进与验证

## 采用方案

用户生产规格为 4 核、6 GB 内存、带宽充足。本轮完成监控增长修复、数据库读写隔离和 WS 下行按消息大小优化。结果对应未提交工作区，尚未部署生产，不代表已测得生产容量。

| 项目 | 当前行为 |
|---|---|
| 监控 | 9 种标准方法保留，其他方法聚合为 OTHER；状态限制为 100–999，无效值归 0。方法/状态组合数量有界，不改原请求方法 |
| 数据库 | WAL + FULL 同步；写事务仍串行，GetNode/ListNodesByOwner 使用最多 4 条只读连接；连接设置 5 秒 busy timeout；新增 session expires_at 索引 |
| WS 节点→浏览器 | ≤1 MiB 消息复用 4/32/256/1024 KiB 分档池并一次写出；较大消息用复用的 32 KiB 缓冲流式发送，完整读完声明长度才发送 FIN |
| WS 错误与慢接收 | 小消息读取失败不发出消息；大消息中途失败不发送完成帧，由 Handler 关闭连接。流式写入每次有 15 秒超时，保持等待节点数据与等待客户端写入的边界 |

分档方案由小/大消息对比基准验证。单纯全部使用小块写入会增加小消息分片和系统调用；最终方案保留小消息的一次写出路径。没有降低 Argon2 强度或消息上限，也没有使用节点缓存、合并写事务或降低 session last_seen 精度。

## 量化结果

环境为 Apple M2 Pro / darwin arm64 / Go 1.26.6；基准 GOMAXPROCS=4。仅模拟 4 个执行线程，没有将本机总内存限制为 6 GB。数据库使用临时文件，WS 使用回环 TCP + 合成节点 pipe；不能将其 MB/s 当成公网吞吐，也不能据此承诺同时在线人数。

| 指标 | 优化前 | 最终结果 |
|---|---|---|
| 20,000 种自定义方法的统计分组 | 20,000 | 1 |
| 同一探针的指标输出 | 4,906,876 字节 | 446 字节 |
| 单独节点查询，三轮中位数 | 17.6 µs/op | 15.4 µs/op |
| 有持续鉴权写入时的节点查询，三轮中位数 | 187.9 µs/op | 13.5 µs/op |
| 有效会话鉴权，三轮中位数 | 344.7 µs/op | 92.2 µs/op |
| 1 MiB WS 消息耗时，200 次 × 3 轮中位数 | 0.565 ms/op | 0.512 ms/op |
| 1 MiB WS 消息分配量 | 约 1.05 MB/op | 约 5.8–11.1 KB/op |
| 32 MiB WS 消息分配量，10 次 × 2 轮 | 约 33.56 MB/op | 约 82.7–88.8 KB/op |
| 32 MiB WS 消息耗时 | 23.5–39.1 ms/op | 22.8–23.2 ms/op |

WS 对比使用相同的 `BenchmarkNodeWebSocketStreaming`，旧路径通过临时 overlay 重建本轮优化前的 Gateway（仍保留上一轮 Cookie/截断修复）。节点开始发送被同步到计时之后，防止漏算第一条消息分配。B/op 为整个基准运行的 Go 分配量，包含模拟发送/接收开销，不是进程 RSS 或瞬时峰值；sync.Pool 也会受 GC 影响。大消息分片使小对象分配次数增加，但总分配字节大幅减少；这些样本不是统计意义上的生产吞吐保证。

## 验证覆盖

- 监控：大量自定义方法与非法状态、标准计数/耗时保真、并发记录和渲染；同一真实 App Handler 的 20,000 请求复现已收敛为一个分组。
- 数据库：未提交写期间的节点读取、提交后立即可见、禁用/删除可见、4 个并发读者与写者、只读保护、连接耗尽时取消/恢复、带特殊字符路径、关闭后重开、过期索引查询计划。
- 既有数据库/身份回归：老 schema 迁移、备份快照一致性和禁止覆盖、密码重置与在途登录、旧密码快照改密、绑定限额和清理不撤销设备。
- WS：空/单字节、4/32/256/1024 KiB 分档边界、32 MiB 上限、text/binary、连续消息和关闭码；超大长度/非法 opcode；小消息与大消息截断不能完成；大消息未收齐前能转发前段；客户端断连和停读释放；8 条连接并发 32 MiB，逐字节核验不串数据。
- Cloud 全包 race 与 vet；真实未修改 Node 的绑定、HTTP、加密 WS/E2EE、Cloud 重启与重连。

验证日志目录：`/tmp/mindfs-perf-fix-y0ovsxqw/`。最终结果以 `race-final.log`、`compat-final.log` 为准；其他文件包括 `metrics-after.log`、`store-after.log`、`ws-small-before.log`、`ws-small-final.log`、`ws-before.log` 和 `ws-large-final.log`。数据库优化前日志仍保留在原性能审计的临时目录。

最终 `go test -race ./...` 的 12 个包全部通过，`go vet ./...` 通过；真实未修改 Node 兼容场景再次通过，场景耗时 7.12 秒。文档 YAML 与 `git diff --check` 通过。

## 改动文件

- `cloud/internal/ops/metrics.go`、`metrics_test.go`。
- `cloud/internal/store/sqlite.go`、`sqlite_connections.go`、`sqlite_nodes.go`、`schema.sql`、`sqlite_concurrency_test.go`。
- `cloud/internal/gateway/websocket.go`、`ws_frames.go`、`ws_message.go`、`websocket_test.go`、`ws_streaming_test.go`（含基准）。
- `cloud/deploy/README.md`；本 issue 的三份记录、性能审计状态和现状架构。

## 保留边界

1. 浏览器→节点协议要求先发送整条消息的长度，这一方向继续缓冲完整消息并受原 32 MiB 上限约束。未增加全局连接配额或总消息字节预算；不能称为已解决所有并发内存问题。
2. SQLite 写入仍串行，last_seen 仍逐次落盘；优化主要隔离节点读取并降低 WAL 写入成本。验证码清理的 OR 条件仍可能扫描表，未引入改变清理时序的额外方案。
3. WAL/SHM 需要现有本地可写数据卷及 SQLite 文件锁。部署说明已补充；仍使用现有 `backup` 一致性快照流程，不复制运行中的主库文件作备份。
4. 本轮未新增整站限流、改变 DNS/TLS、部署网络、生产资源限制或多实例支持。用户决定不继续处理的同源隔离和暂停的本地服务域名功能保持原决定。
