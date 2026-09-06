---
doc_type: issue-report
issue: 2026-09-06-relay-performance-hardening
status: confirmed
tags: [relay, performance, memory, sqlite, websocket]
---

# Relay 性能与资源使用改进

承接 [性能审计](../../audits/2026-09-06-relay-performance/index.md)。用户提供生产规格 4 核、6 GB 内存、带宽充足，要求优化考虑各种情况。现有证据：任意 HTTP 方法使指标分组永久增长；会话写入与节点读争用唯一连接；WS 节点消息按全长分配内存。

期望：限制监控分类数量，降低写入对转发启动的影响，降低 WS 大消息分配；保留未修改客户端的 HTTP/WS/E2EE 契约、密码强度、32 MiB 消息上限和事务语义。覆盖正常读写、并发、慢收发、截断、断连、备份与重开。线上吞吐/人数需要生产负载数据，不能从机器规格直接保证。
