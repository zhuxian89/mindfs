---
doc_type: audit-finding
audit: 2026-09-06-relay-performance
finding_id: performance-03
nature: performance
severity: P2
confidence: medium
suggested_action: cs-refactor
status: improved
resolved_in: working-tree
---

# Finding 03：WebSocket 整消息缓冲，缺少跨连接内存预算

## 2026-09-06 处理结果

节点→浏览器方向已改为 ≤1 MiB 分档池复用、较大消息以 32 KiB 缓冲流式发送。相同桥接基准的 32 MiB 消息分配量从约 33.56 MB/op 降至 82.7–88.8 KB/op；小消息分档避免额外分片成本。并发大消息、分档/消息边界、截断、停读、断连和真实 E2EE 兼容通过。

浏览器→节点方向仍因帧头必须先给长度而整消息缓冲，原 32 MiB 上限不变，也未新增跨连接总预算，因此仅标为 improved，不宣称全部内存风险消失。见 [修复记录](../../issues/2026-09-06-relay-performance-hardening/relay-performance-hardening-fix-note.md)。以下为优化前证据。

## 速答

Cloud 在两个转发方向都先持有完整 WS 消息，再向下游写入。单条消息有 32 MiB 限制，但不同连接可以同时分配；并发大消息会推高内存占用和 GC 压力。

## 关键证据

- `cloud/internal/config/config.go:28`：`defaultMaxWSMessageBytes = int64(32 << 20)`。
- `cloud/internal/gateway/websocket.go:109`：浏览器方向使用 `publicWS.ReadMessage()`，返回整条消息的 `[]byte` 后才写入节点流。
- `cloud/internal/gateway/websocket.go:230`：节点方向按帧内声明长度一次性分配，并读满后才返回：

  ```go
  size := int64(binary.BigEndian.Uint32(header[1:]))
  if size > maxMessageBytes {
      return 0, 0, nil, 0, "", errWSTooLarge
  }
  payload := make([]byte, size)
  if _, err := io.ReadFull(r, payload); err != nil {
      return 0, 0, nil, 0, "", err
  }
  ```

- `cloud/internal/gateway/websocket.go:89` 设置单连接 ReadLimit，`:92` 后启动两个桥接 goroutine；Gateway 当前没有共享的消息字节预算或活跃连接配额。密码计算的两槽预算只覆盖身份服务，不覆盖转发。

## 本地测量

`BenchmarkPerfAuditReadWSFrame` 对现有解帧函数进行 20 次 × 2 轮微基准：

- 1 MiB 消息：每次约 1,048,632–1,048,636 字节分配。
- 32 MiB 消息：每次约 33,554,502–33,554,508 字节分配。

输入缓冲构建排除在计时和分配统计之外；上述值包含解帧辅助对象，主要成本来自 payload。没有真实网络、加密或慢速客户端，因此不能将日志中的内存处理 MB/s 当作 Relay 吞吐。

若 10 条不同连接同时处于持有 32 MiB 节点消息的阶段，仅这一方向的 payload 就约 320 MiB；这是一项条件成立时的容量估算，未执行 10 连接峰值实测，也不表示 10 个普通在线节点就占用这些内存。读取失败后的临时缓冲可回收，当前证据不是永久泄漏。

## 影响与边界

主要触发条件是并发大消息、慢速收发或低内存机器。小文本消息、低并发场景未证明存在明显性能问题，所以定 P2、medium。普通 HTTP body 使用 `io.Copy` 流式传输，不适用“整个文件读入内存”的描述。

## 修复方向与建议动作

建议 `cs-refactor`：根据真实消息分布评估缓冲复用或流式转发；跨连接预算属于会改变过载行为的方案，需要另行明确。实现必须保留帧边界、长度校验、错误关闭和未修改客户端的 E2EE 语义，不能只调小消息限制来代替兼容优化。
