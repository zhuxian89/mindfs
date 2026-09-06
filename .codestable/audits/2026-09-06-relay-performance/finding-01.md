---
doc_type: audit-finding
audit: 2026-09-06-relay-performance
finding_id: performance-01
nature: performance
severity: P1
confidence: high
suggested_action: cs-issue
status: fixed
resolved_in: working-tree
---

# Finding 01：任意 HTTP 方法名使监控统计无界增长

## 2026-09-06 处理结果

非标准方法已归并为 OTHER，无效状态归 0，分类数量有界。相同 App Handler 探针的 20,000 种方法现只产生 1 个分组，指标文本为 446 字节；并发记录/渲染测试通过。见 [修复记录](../../issues/2026-09-06-relay-performance-hardening/relay-performance-hardening-fix-note.md)。以下保留修复前证据。

## 速答

HTTP 方法名由请求方提供，Metrics 将其直接作为永久 map 键的一部分。请求即使返回 404，也能增加一组长期保留的统计数据；方法名种类增加时，进程内存和 `/metrics` 输出持续增长。

## 关键证据

- `cloud/app/request_log.go:85`：所有正常返回的请求都调用 `observe(r.Method, status, duration)`，没有将自定义方法归并为有限分类。
- `cloud/internal/ops/metrics.go:34`：原始方法名直接入键，map 无容量或回收机制：

  ```go
  key := metricKey{method: method, status: status}
  m.mu.Lock()
  value := m.values[key]
  value.count++
  value.duration += duration
  m.values[key] = value
  m.mu.Unlock()
  ```

- `cloud/internal/ops/metrics.go:43`：Render 持锁复制全部分组，再排序和输出三条指标样本。分组增多也会延长锁持有时间和生成响应的耗时。
- `cloud/internal/ops/metrics_test.go:10`：既有测试确认不使用 path/query/token 标签，但只测试标准 GET，没有覆盖自定义方法导致的分组膨胀。

## 本地复现

`TestPerfAuditArbitraryMethodsAccumulateMetrics` 使用合成 App，经完整 Handler 发送 `AUDIT0` 至 `AUDIT19999` 共 20,000 种合法 HTTP 方法，路径均为 `/not-found`。所有请求返回 404，无账号或有效节点要求。

实测保留 20,000 个统计分组；强制 GC 前后对比，堆使用净增加 2,238,392 字节，Render 输出 4,906,876 字节。输出包含每组的请求计数、耗时总和与耗时样本数。测试期间关闭日志输出以避免磁盘日志干扰测量；原始结果见 index 中的 `probes.log`。

这不是正常 GET/POST 重复调用必然增长：相同 method/status 会复用统计项。需要不同方法名持续进入后端；实际公网代理是否额外拦截这些方法未核验。

## 影响

分组在当前 App 生命周期内不会回收，持续异常流量可累积内存。抓取 `/metrics` 时还会临时复制整个 map、排序并构造较大的响应。复现证明了无界增长路径，没有实施线上流量实验或声称线上已经内存不足。

## 修复方向与建议动作

建议 `cs-issue`：监控维度只保留有限的标准方法名，将其他方法归并为统一类别；无需修改请求本身的路由或转发语义。
