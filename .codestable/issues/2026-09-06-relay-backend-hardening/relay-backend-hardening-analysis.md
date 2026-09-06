---
doc_type: issue-analysis
issue: 2026-09-06-relay-backend-hardening
status: confirmed
tags: [relay, security, protocol, compatibility]
---

# 根因与实施范围

## 已确认方案

用户已授权处理审核确认的问题，无需逐文件重复放行。实施范围为 `cloud/**` 下与六项问题相关的入口、服务、存储、测试和部署说明，以及本次问题和审计状态文档。

| 问题 | 实施方案 | 主要文件 |
|---|---|---|
| 控制 Cookie | HTTP/WS 请求移除 Cloud 专用 Cookie；拒绝节点 Set-Cookie 覆盖；节点 Cookie 约束到对应 /n/{id}/ 路径；移除节点的全站清理和扩大 SW scope 的响应头 | `internal/gateway/{http,websocket,cookies}.go`、`internal/identity/service.go`、`app/binding_handlers.go` |
| 可信来源 | 默认 TCP peer；配置 CIDR 后从右向左解析 XFF；忽略 CF 头；示例代理移除 CF 头；来源限流先于邮箱计数；所有密码计算共用两槽并发预算 | `app/{identity_handlers,request_source}.go`、`internal/config/config.go`、`internal/identity/{service,password_budget}.go`、`deploy/` |
| 改密时序 | 会话创建事务比较被验证过的 password hash；密码变更用同一旧 hash 做条件更新，防止旧请求覆盖已重置的密码 | `internal/store/{contracts,sqlite_identity}.go`、`internal/identity/service.go` |
| 截断响应 | HTTP 和 WS 非 101 响应检查复制错误；头已提交后通过 ErrAbortHandler 中止下游；日志中间件记录中断指标并继续传播该信号 | `internal/gateway/{http,websocket}.go`、`app/request_log.go` |
| 退出 | 主流程等待 Shutdown 完成；超时关闭普通 HTTP 连接；随后释放 App 和 Connector | `cmd/mindfs-relay/main.go`、`deploy/docker-compose.yml` |
| 绑定保留 | 新增 challenge 全局滚动速率 120/min、总量 10,000；poll 来源限流 600/min；字段长度上限；过期 24h 后每轮删除最多 1,000 条；按清理/计数条件建索引 | `app/binding_handlers.go`、`internal/binding/service.go`、`internal/store/{sqlite_binding,sqlite_sessions}.go`、`internal/store/schema.sql` |
| 依赖 | Cloud module 更新 Gorilla、x/net、x/crypto 及所需 x/sys；Go 最低版本和 Docker 构建器更新到 1.26.6 | `go.mod`、`go.sum`、`Dockerfile`、`deploy/deploy_test.go` |

新增测试仅覆盖实际风险：跨 owner Cookie 转发、请求头伪造、密码重置与签发交错、过期快照改密、密码并发预算、截断响应、活跃请求排空及超时、容量上限和清理不撤销设备。

## Cookie 隔离的范围决定

2026-09-06 用户了解剩余同源风险后明确要求不继续修复。保留 HTTP/WS Cookie 过滤和节点响应防护；独立管理 origin 不再纳入本次实施范围。过滤 Cookie 不代表完整的多用户网页隔离，单源节点脚本访问 Cloud 控制 API 的风险仍存在。无需继续等待部署域名选择。

## 验证范围

- Cloud 全套单元/HTTP 集成测试与 race、vet。
- 开启真实未修改 Node 的兼容测试，覆盖绑定、HTTP、WS/E2EE 和重连。
- 使用当前 Cloud 工具链重新运行 govulncheck，区分符号命中与实际可利用性。
- 不对线上服务实施洪泛、重启或数据库迁移；源码修复完成与部署上线分开记录。
