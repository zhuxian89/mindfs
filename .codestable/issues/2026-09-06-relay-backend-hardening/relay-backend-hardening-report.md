---
doc_type: issue-report
issue: 2026-09-06-relay-backend-hardening
status: confirmed
severity: P1
tags: [relay, security, identity, gateway, shutdown, sqlite]
---

# Relay 后端审计问题修复

## 问题与证据

用户已授权“开始解决所有问题”。本次承接 [后端审计](../../audits/2026-09-06-relay-backend-review/index.md) 的六项发现和 [独立复核](../../audits/2026-09-06-relay-backend-verification/index.md)，根因和复现已在前述文档确认，不重新收集同一问题。

1. Cloud Session Cookie 被转发到节点，节点还可回写控制面 Cookie；同源节点脚本对控制 API 的访问需要完整信任边界方案。
2. 无条件采用 CF/XFF 请求头，来源限流可绕过；密码计算无并发预算。
3. 校验旧密码和创建会话分离，密码重置后仍可产生有效会话。
4. body 复制错误被忽略，截断的 chunked 响应被正常结束。
5. 主流程不等待 HTTP Shutdown，活跃请求被提前打断。
6. 匿名绑定记录持续新增，过期只改状态、永久保留。

另维护 Cloud 独立 module 的受影响依赖和构建工具链。上游兼容性审计中的客户端自启动、缓存等不在本次六项后端问题范围内；暂停的本地服务域名功能继续保持暂停。

## 期望行为

在不修改官方客户端源码和既有 HTTP/WS/E2EE 路径的前提下，阻断控制凭据转发和覆盖；限流依赖可信来源；改密后拒绝依赖旧密码的在途签发；传输失败对客户端可见；正常退出有界排空；绑定记录的增长与保留有上限，已绑定设备 Token 保持有效。

## 用户确认的范围调整

2026-09-06 用户了解 Cookie 局部修复与同源权限的区别后，明确要求不继续修复剩余同源问题。已完成的 Cookie 防护保留，独立管理域名不在本次范围内；仍如实记录剩余风险，不把过滤 Cookie 宣称为完整多用户网页隔离。
