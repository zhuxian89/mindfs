---
doc_type: audit-index
audit: 2026-09-06-relay-backend-review
scope: Cloud Relay 后端身份、绑定、转发、持久化和退出流程
created: 2026-09-06
status: partially-fixed
fixed_findings: 5
partially_fixed_findings: 1
remaining_action: declined-by-user
total_findings: 6
---

# Relay 后端代码审查

## 2026-09-06 修复进度

**02—06 已完成源码修复和回归验证；01 已封堵 Cookie 转发/覆盖，用户于 2026-09-06 明确决定不继续修复剩余同源隔离问题。** 01 保持部分修复的技术状态，不再等待管理域名选择。依赖升级后的 govulncheck 符号命中为 0，真实未修改 Node 的 HTTP/WS/E2EE 与重连兼容测试通过。尚未部署生产。

详见 [修复记录](../../issues/2026-09-06-relay-backend-hardening/relay-backend-hardening-fix-note.md)。下文总评、代码行号与复现结果保留为修复前基线，不表示这些路径在当前工作区仍全部存在。

## 总评

发现 6 项值得修复的问题，其中 5 项通过隔离 Go 测试复现，1 项通过真实临时 Relay 子进程复现。最严重的是 Cloud 登录 Cookie 被转发给节点：当已登录用户访问另一个用户控制的节点时，节点可以取得并重放访问者的 Cloud 会话。

稳定运行一个月与这些发现并不矛盾：问题分别需要不可信节点、特定请求头、持续创建绑定、并发改密、传输中断或重启等条件。此次没有核验线上用户构成、入口代理规则、数据库大小或镜像版本，因此不能认定生产已经受到攻击或出现数据损坏。

## 范围与基线

- 基线：`34710433f94ed0301b39aea074411f0b559eb787`，本地工具链 `go1.26.5 darwin/arm64`。
- 用户授权审查自研后端，范围据此收敛到 `cloud/`，重点读取 `app/`、`internal/identity/`、`internal/binding/`、`internal/store/`、`internal/connector/`、`internal/gateway/` 和 `cmd/mindfs-relay/main.go`；补查配置、运维、资源同步和部署示例。
- 已读 `.codestable/attention.md` 和 `.codestable/architecture/cloud-relay-core.md`。按 bug、安全、性能、可维护性、架构对照检查；未单独报告纯长度/风格问题，也不重复把已有安全、可靠性发现计作架构偏离。
- `relay-local-service-domains` 继续保持暂停，不纳入整改。本次没有修改 Node、Web 或 Cloud 业务源码，也没有访问生产数据库、发送邮件、部署或重启现有服务。
- 本次仅新增审计文档。复现代码、合成数据库和临时二进制位于 `/tmp/mindfs-backend-review.epfP86/`。测试进程仅监听回环地址，退出实验只向自行启动的临时子进程发送信号。
- 既有上游兼容性报告讨论不同问题，继续有效，本次不覆盖其结论：[上游兼容性审计](../2026-09-06-relay-upstream-compatibility/index.md)。

## 发现清单

| # | 性质 | 严重度 | 置信度 | 问题 | 证据位置 |
|---|---|---|---|---|---|
| 1 | security | P0 | high | [Cloud 会话转发给不可信节点](finding-01.md) | `cloud/internal/gateway/http.go:148` |
| 2 | security | P1 | medium | [可伪造来源 IP 绕过跨邮箱限流](finding-02.md) | `cloud/app/identity_handlers.go:182` |
| 3 | security | P1 | high | [密码重置后，并发旧密码登录仍可签发会话](finding-03.md) | `cloud/internal/store/sqlite_identity.go:242` |
| 4 | bug | P1 | high | [上游传输截断被包装成成功响应](finding-04.md) | `cloud/internal/gateway/http.go:84` |
| 5 | bug | P1 | high | [进程退出不等待优雅关闭](finding-05.md) | `cloud/cmd/mindfs-relay/main.go:129` |
| 6 | performance | P1 | high | [匿名绑定记录永久累积](finding-06.md) | `cloud/internal/store/sqlite_sessions.go:27` |

P0 的紧急程度取决于是否允许不可信用户/节点共用 Relay：代码中的会话泄露已复现，但未确认线上存在该场景。第 2 项在应用层已复现，生产是否可由外部触发取决于入口是否可信地覆盖/移除这些头，因此置信度保留为 medium。其余 high 表示触发路径与结果得到验证，不表示已经在线上发生。

## 按维度分布

| 性质 | P0 | P1 | P2 | 合计 |
|---|---|---|---|---|
| security | 1 | 2 | 0 | 3 |
| bug | 0 | 2 | 0 | 2 |
| performance | 0 | 1 | 0 | 1 |
| maintainability | 0 | 0 | 0 | 0 |
| arch-drift | 0 | 0 | 0 | 0 |
| **合计** | **1** | **5** | **0** | **6** |

## 验证结果

在 `cloud/` 运行：

| 验证 | 结果与边界 |
|---|---|
| `go test ./...` | 12 个包全部通过；重型兼容测试默认跳过，不代表本次重新验证了真实上游 release |
| `go test -race ./...` | 全部通过；业务时序缺陷不一定表现为 Go 内存数据竞争 |
| `go vet ./...` | 通过 |
| `go test -overlay /tmp/mindfs-backend-review.epfP86/overlay.json ./app ./internal/identity ./internal/gateway -run TestAudit -v -count=1` | 5 个定向测试通过；这些测试断言的是缺陷现象，因此 PASS 表示复现成功，并非问题已修复 |
| 临时二进制退出实验 | 收到 `100 Continue` 确认请求已进入 handler 后发送 SIGTERM，请求体仍未完成，进程约 0.009 秒退出，未等待设置的 10 秒窗口 |
| `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | 实际运行 v1.7.0，报告 8 项符号层命中；适用性详见下文 |

复现入口为 `/tmp/mindfs-backend-review.epfP86/build_probes.py` 和 `shutdown_probe.py`；结果文件为 `probes.log`、`shutdown-result.json`。全部使用合成账号、验证码和会话，不记录真实用户凭据，不发送真实邮件。

## 依赖扫描补充

下面属于扫描器命中与适用性复核，不计入上述 6 个已复现问题，也不能按数量推断公网可利用漏洞数量。没有核验线上二进制的实际 Go 版本；`cloud/Dockerfile:10` 使用的是 `golang:1.25-alpine`，并非本机扫描用的 Go 1.26.5。

| 告警 | 扫描发现 / 修复版本 | 本项目适用条件 |
|---|---|---|
| [GO-2026-6278](https://pkg.go.dev/vuln/GO-2026-6278) | gorilla/websocket 1.5.1 → 1.5.3 | 问题涉及客户端掩码随机数。Cloud 业务路径使用服务端 Upgrader，不能直接据此认定 Cloud 的入站 WS 可被该问题攻击 |
| [GO-2026-6218](https://pkg.go.dev/vuln/GO-2026-6218) | Go 1.26.5 → 1.26.6 | URL 路径解析复杂度；报告示例入口是 healthcheck 的 HTTP 客户端，需进一步追踪不可信 URL 的可控性 |
| [GO-2026-6091](https://pkg.go.dev/vuln/GO-2026-6091) | Go 1.26.5 → 1.26.6 | html/template 正则上下文；当前模板固定，绑定页 Execute 参数为 nil，未复现动态输入注入 |
| [GO-2026-6090](https://pkg.go.dev/vuln/GO-2026-6090) | Go 1.26.5 → 1.26.6 | TLS post-handshake 消息消耗；示例部署在外部终止公网 TLS，Cloud 的相关 TLS 调用包括 SMTP 等客户端连接，暴露面需按实际部署核验 |
| [GO-2026-6089](https://pkg.go.dev/vuln/GO-2026-6089) | Go 1.26.5 → 1.26.6 | 未加密 HTTP/2 探测时未应用 ReadHeaderTimeout；当前入口未显式启用该协议模式 |
| [GO-2026-5972](https://pkg.go.dev/vuln/GO-2026-5972) | Go 1.26.5 → 1.26.6 | ASN.1 递归；扫描路径经过 SMTP TLS 证书解析，实际攻击输入需要进一步验证 |
| [GO-2026-5026](https://pkg.go.dev/vuln/GO-2026-5026) | x/net 0.21.0 → 0.55.0；Go 1.26.5 → 1.26.6 | 域名标签处理；需结合具体 HTTP 客户端目标核验 |
| [GO-2025-3595](https://pkg.go.dev/vuln/GO-2025-3595) | x/net 0.21.0 → 0.38.0 | HTML tokenizer 解析差异；本项目用于读取本地 index 的资源引用，未证实公网 XSS |

建议在独立维护变更中更新依赖、核验部署工具链，并跑已有协议兼容测试；不要把根目录 `go.mod` 的更新误认为已更新 `cloud/go.mod`。

## 下一步建议

1. 有多用户或不可信节点时，优先通过 `cs-issue` 处理 finding-01 的控制面凭据隔离，并核验同源节点内容的权限边界；仅去掉一个 Cookie 不足以证明整个浏览器信任边界安全。
2. 公网服务优先修 finding-02、finding-06，避免请求来源伪造和无上限持久记录累积。
3. 本迭代处理 finding-03、finding-04、finding-05：保护改密后的会话失效语义、传输失败可见性和重启期间的请求完成。
4. 暂不因这些条件性发现断言当前服务不可用。修复后按具体风险加入回归测试，并在既有官方协议路径约束内实施。
