---
doc_type: issue-fix
issue: 2026-09-06-relay-backend-hardening
status: partially-fixed
remaining_action: declined-by-user
path: standard
fix_date: 2026-09-06
related: [relay-backend-hardening-analysis.md, relay-backend-hardening-report.md]
tags: [relay, security, identity, gateway, shutdown, sqlite, dependencies]
---

# Relay 后端审计问题修复记录

## 当前结果

六项审计中的 **02—06 已完成源码修复和回归验证**。01 已封堵 HTTP/WS 的 Cloud Cookie 转发、节点响应覆盖 Cloud Cookie 及全站清理响应头。2026-09-06 用户了解剩余同源风险后明确决定不继续修复；保留已完成的 Cookie 防护，不实施独立管理域名。当前授权范围内的修复已完成，01 仍按技术现状标为部分修复，不再等待域名选择。

本记录描述工作区源码，不代表线上已部署。没有提交、推送、重启现有服务、迁移生产数据库或调用真实邮件接口。

## 采用方案

| 项目 | 修复后行为 | 验证 |
|---|---|---|
| 01 Cookie 局部修复 | 删除所有 Cloud 会话 Cookie 的转发，保留节点自己的 Cookie；过滤 Cloud Set-Cookie，约束节点 Cookie 路径，删除 Clear-Site-Data/Service-Worker-Allowed | 跨账号真实 App 请求 + HTTP/WS 公共克隆路径与响应头测试；同源网页权限尚未验收 |
| 02 来源限流 | 默认 TCP peer；可信 CIDR 配置 + XFF 从右向左解析；不信任 CF 头；来源计数先于邮箱，避免被限流来源仍创建邮箱计数；密码操作共享两槽预算 | 同源 IP 31 次不同邮箱登录，即使伪造 CF 头仍出现 429；代理链、IPv6、畸形输入与密码预算回归 |
| 03 改密竞态 | 会话事务比较验证时的旧 hash；主动改密采用旧 hash 条件 UPDATE | 在旧密码校验与会话创建间完成 reset，在途登录被拒绝；旧 User 快照不能覆盖 reset 后密码 |
| 04 响应截断 | 复制失败后中止 HTTP 流，并记录中断指标，不再正常完成缺失 body | 3 字节和 64 KiB 截断响应、普通 HTTP 与 WS 非 101 分支均使客户端看到错误 |
| 05 退出等待 | 主流程等待 Shutdown；超时关闭普通 HTTP，随后释放 App；Compose 停止宽限 15 秒 | 活跃请求完成后才返回；超时请求被关闭；真实兼容测试中的 Cloud 重启和重连通过 |
| 06 绑定生命周期 | 新记录 120/min、总量 10,000、poll 来源 600/min、字段长度限制；过期 24h 后分批回收并增加索引 | 创建/容量限制、窗口恢复、已有 code 可轮询、清理不删除新记录且不撤销设备 Token |

## 依赖与构建

Cloud 独立 module 更新为：Go 最低版本 1.26.6，gorilla/websocket 1.5.3，x/crypto 0.56.0，x/net 0.57.0，x/sys 0.47.0。x/crypto 0.56.0 要求 x/net 0.57.0，故统一使用兼容依赖组合，未强行固定互相冲突的版本。

Docker 构建器使用 `golang:1.26.6-alpine`。本机执行验证时报告 `go version go1.26.6 darwin/arm64`，未修改全局 Go 安装或根 module 的依赖。

`govulncheck v1.7.0` 的最终结果：**0 个符号可达漏洞、0 个导入包漏洞**；另有 1 个仅 module 层命中，扫描器确认当前代码未调用对应路径。完整输出在 `/tmp/mindfs-relay-hardening-validation/govulncheck.log`。不能将源码扫描结果视为已验证线上镜像。

## 验证命令与结果

在 `cloud/` 执行：

```sh
go test ./...
go test -race ./...
go vet ./...
MINDFS_RUN_COMPAT=1 go test ./compat -run '^TestUnmodifiedNodeRelayCompatibility$' -count=1 -v -timeout=180s
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
```

- 常规测试与新增回归通过；更新工具链后的全包 race 测试通过，vet 通过。
- 真实未修改 Node 兼容测试通过，场景耗时 16.66 秒，覆盖绑定、公开 HTTP、独立 E2EE 验证、加密 WS、Cloud 重启和节点重连、端口释放。
- 没有前端源码改动。完整浏览器多源隔离尚未实现，因此没有把上述兼容测试说成多用户浏览器安全验收。

## 2026-09-06 代码简化

按用户 `/code-simplier` 请求，使用本机 `code-simplifier` 指令中的规则整理本轮修改，保持行为不变：

- HTTP 与 WebSocket 非 101 响应共用 `writeHTTPResponse`，统一响应头、状态码、body 写回和截断中止；各入口原有的 Cookie/响应头过滤顺序及资源关闭位置保持不变。
- 提取 `checkNewBindChallengeLimits`，将总容量和每分钟创建数检查集中到一个函数；继续使用创建 challenge 的同一事务，已有 challenge 轮询仍跳过新增限额。
- 展开改密错误分支与测试中的紧凑 goroutine，提高可读性；保留原有错误映射和测试断言。

简化后重新执行 `go test -race ./...`、`go vet ./...` 均通过；真实未修改 Node 兼容测试再次通过，场景耗时 6.83 秒。`git diff --check` 和六份更新文档的 YAML frontmatter 校验通过。本轮未改变依赖，不重复运行前述漏洞扫描。

## 改动文件清单

- `cloud/app/`：`binding_handlers.go`、`identity_handlers.go`、`request_source.go`、`request_log.go`；测试 `request_source_test.go`、`security_regression_test.go`。
- `cloud/internal/identity/`：`service.go`、`password_budget.go`、`password_race_test.go`。
- `cloud/internal/store/`：`contracts.go`、`sqlite_identity.go`、`sqlite_binding.go`、`sqlite_sessions.go`、`schema.sql`、`binding_limits_test.go`。
- `cloud/internal/gateway/`：`http.go`、`websocket.go`、`cookies.go`、`response_failure_test.go`。
- `cloud/internal/binding/service.go`；`cloud/internal/config/config.go`、`config_test.go`。
- `cloud/cmd/mindfs-relay/`：`main.go`、`shutdown_test.go`。
- `cloud/go.mod`、`go.sum`、`Dockerfile`；`cloud/deploy/Caddyfile.example`、`docker-compose.yml`、`deploy_test.go`、`README.md`。
- CodeStable：本 issue 的 report/analysis/fix-note、原六项 finding 的当前处理状态、审计 index 的处理进度及现状架构补充。独立复核报告和上游兼容审计正文保持原样。

## 部署注意与保留限制

1. 必须先按实际代理链设置 `MINDFS_CLOUD_TRUSTED_PROXIES`；不设置时安全地忽略来源头，但同一代理后的用户会共用配额。示例 Caddy 去除 CF 头；其他既有代理配置需要同等的可信来源处理。配置说明已写入部署 README。
2. 旧数据库若已有超过 10,000 条记录，新绑定可能需等待数轮清理；已绑定设备和已存在 challenge 不受新增容量限额影响。删除记录释放 SQLite 可复用页，不保证文件大小立即减小。
3. **01 剩余风险由用户决定保留**：单域名下，不可信节点的 JavaScript 仍能同源请求 Cloud 控制接口。2026-09-06 用户明确要求不继续修复这一部分；不将其记为已解决，也不继续推进独立管理域名。
4. 所有源码修复需要按既有备份、资源同步、迁移、健康检查流程发布；本轮尚未执行生产部署。
