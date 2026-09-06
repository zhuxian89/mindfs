---
doc_type: audit-index
audit: 2026-09-06-relay-performance
scope: 当前 Cloud Relay 工作区的转发内存、数据库竞争、监控统计及后台清理
created: 2026-09-06
status: partially-fixed
fixed_findings: 1
improved_findings: 2
total_findings: 3
---

# Relay 后端性能审计

## 2026-09-06 优化进度

用户补充生产为 4 核、6 GB 内存、带宽充足，并要求覆盖各种情况。当前工作区已修复 01 的无界指标分类；02 使用 WAL + FULL 和独立节点只读池；03 对下行小消息使用分档缓冲池、大消息使用流式转发。02 的串行写入与 03 的上行整消息缓冲/缺少全局预算仍如实保留为边界。

最终全包 race/vet 和真实未修改 Node 兼容测试通过，尚未部署。优化后结果与场景覆盖见 [性能改进记录](../../issues/2026-09-06-relay-performance-hardening/relay-performance-hardening-fix-note.md)。以下审计结论及数据保留为优化前证据。

## 总评

发现 1 项应修的无界内存增长问题和 2 项随负载增长的性能优化点。监控统计允许任意 HTTP 方法名永久增加统计分组，已用真实 App Handler 复现。数据库的单连接竞争和 WebSocket 整消息分配也得到本地测量，但没有生产机器配置、实际并发、消息大小或监控数据，不能认定线上已经出现性能不足，也不能从本地微基准推导可承载人数或公网吞吐。

普通 HTTP body 使用流式转发，不会把整个下载文件读入 Cloud 内存。密码计算已限制为两项并发，绑定 challenge 也已有创建、容量和保留限制；这些不重复计作未修复问题。

## 范围与基线

- 用户询问当前后端性能，审计范围沿用 `cloud/`：Gateway、Connector/Registry、SQLite 身份/绑定/节点查询、Metrics、清理任务和部署入口。
- 基线为包含本轮 hardening 与 code-simplifier 改动的未提交工作区，HEAD 为 `34710433f94ed0301b39aea074411f0b559eb787`。结果描述当前源码，不代表生产已部署这些改动。
- 本次新增性能审计文档，未修改业务代码、配置、客户端或现有测试。临时探针通过 Go overlay 注入；数据库均为临时合成数据，无生产压测。
- 本审计补充原后端审计，未替代安全/兼容性结论。用户决定不继续修复的同源隔离问题与暂停的本地服务域名功能不在本次范围内。

## 发现清单

| # | 性质 | 严重度 | 置信度 | 问题 | 证据 |
|---|---|---|---|---|---|
| 1 | performance | P1 | high | 任意 HTTP 方法名使监控统计无界增长 | [finding-01.md](finding-01.md)，`cloud/internal/ops/metrics.go:34` |
| 2 | performance | P2 | medium | 会话鉴权逐次写盘，与转发节点查询争用唯一数据库连接 | [finding-02.md](finding-02.md)，`cloud/internal/store/sqlite_identity.go:381` |
| 3 | performance | P2 | medium | WebSocket 整消息缓冲，缺少跨连接的内存预算 | [finding-03.md](finding-03.md)，`cloud/internal/gateway/websocket.go:230` |

## 按维度分布

| 性质 | P0 | P1 | P2 | 合计 |
|---|---|---|---|---|
| performance | 0 | 1 | 2 | 3 |
| **合计** | **0** | **1** | **2** | **3** |

本轮只按性能维度定级，不重复增加 security 标签计数。

## 验证结果与限制

环境：Apple M2 Pro，darwin/arm64，Cloud Go 1.26.6。微基准固定 `-cpu=4`，与功能探针顺序执行。

| 本地验证 | 结果 | 限制 |
|---|---|---|
| 20,000 种合法自定义方法访问真实 App Handler 的不存在路由 | 全部 404；保留 20,000 个统计分组；GC 后堆约增加 2.1 MiB；指标文本约 4.7 MiB | 合成异常流量，未经过公网反向代理；没有证明线上已被这样访问 |
| GetNode，500 次 × 3 轮 | 平均每次 14.1–22.8 µs，中位数 17.6 µs | 仅数据库调用，无公网、Node 和 yamux 开销 |
| GetUserBySession，500 次 × 3 轮 | 平均每次 338–376 µs | 合成有效会话，每次推进 last_seen 时间，包含真实临时文件写入 |
| GetNode + 一个持续鉴权写入 goroutine | 平均每次 187.5–196.2 µs，中位数 187.9 µs，约为独立查询的 10.7 倍 | 持续写入负载，仍为亚毫秒级；不等于真实业务整体慢 10.7 倍 |
| readWSFrame，20 次 × 2 轮 | 1 MiB 消息每次分配约 1 MiB；32 MiB 消息每次分配约 32 MiB | 仅解帧内存微基准；日志中 MB/s 是内存处理速度，不能当作网络吞吐 |
| 清理语句 EXPLAIN QUERY PLAN | `SCAN user_sessions`、`SCAN email_verification_codes` | 确认扫描计划，未测生产表规模或长尾延迟 |

探针、overlay 和原始日志位于临时目录：

`/var/folders/gc/tm9k8ng57hg_r71x5dq1c1j00000gn/T/mindfs-perf-audit-w7jwbk8v/`

- `probes.log`：App 指标增长和 SQLite 查询计划。
- `store-bench.log`：数据库微基准、PRAGMA 和连接等待计数。
- `ws-bench.log`：消息分配微基准。
- `overlay.json` 与对应 `*_perf_test.go`：可重复执行的探针源码，未加入 Cloud module。

执行命令在 `cloud/` 下为：

```sh
go test -overlay <临时目录>/overlay.json ./app ./internal/store -run '^TestPerfAudit' -count=1 -v
go test -overlay <临时目录>/overlay.json ./internal/store -run '^$' -bench '^BenchmarkPerfAuditStore$' -benchtime=500x -count=3 -cpu=4
go test -overlay <临时目录>/overlay.json ./internal/gateway -run '^$' -bench '^BenchmarkPerfAuditReadWSFrame$' -benchmem -benchtime=20x -count=2 -cpu=4
```

## 下一步建议

审计时建议优先处理 P1 的监控统计无界增长，该项现已修复。P2 已完成上文所述源码优化；后续应结合实际活跃用户数、上行 WS 消息大小和数据库等待时间决定是否增加资源预算等机制。当前证据不支持仅因使用 SQLite 就换数据库，也不支持降低密码哈希强度。后续优化必须保留未修改 Node/Web 的 HTTP/WS/E2EE 契约。
