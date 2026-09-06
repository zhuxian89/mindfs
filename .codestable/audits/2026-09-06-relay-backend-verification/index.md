---
doc_type: audit-index
audit: 2026-09-06-relay-backend-verification
scope: 对已有 Relay 后端审查六项发现及依赖扫描证据的独立复核
created: 2026-09-06
status: active
total_findings: 6
new_findings: 0
verification_of: 2026-09-06-relay-backend-review
---

# Relay 后端审查可信度复核

## 结论

**原报告六项核心缺陷均有真实代码依据，本轮重新执行后均复现相应现象，未发现凭空编造的缺陷。报告可作为修复依据，但不能直接等同于线上故障或已被利用的证明；严重度需要结合部署与信任边界重新解释。**

其中 5 项重跑原 Go overlay 测试，优雅退出项用本轮重新构建的二进制和新编写的临时脚本独立复现。原报告及六份 finding 文件均保持未修改；本记录是补充核验，不替代或暗中重写原报告。

基线：`34710433f94ed0301b39aea074411f0b559eb787`，与原报告一致。原复现目录 `/tmp/mindfs-backend-review.epfP86/` 仍存在，脚本、overlay、测试与日志可读取；并非只有无法追溯的文字摘要。

## 逐项判定

| 原发现 | 本轮结果 | 证据强度与必要限定 |
|---|---|---|
| [01 Cloud Cookie 转发给节点](../2026-09-06-relay-backend-review/finding-01.md) | 成立；定级需条件化 | 测试从另一个用户节点实际收到的 HTTP 请求中提取 Cookie，再调用真实 `/api/auth/me` 验证身份重放。它手动加入 Cookie、使用模拟节点，不是浏览器全链路测试。真实浏览器须已登录、向 Cookie 所属主机发送请求且符合 Secure/SameSite 规则。节点须不可信或已被攻陷。不能把 P0 无条件套用到全为可信节点的个人环境；可按条件性 P1 排期，存在现实不可信节点时再提升紧急性。 |
| [02 来源 IP 限流绕过](../2026-09-06-relay-backend-review/finding-02.md) | 应用层成立；公网可达性未知 | 固定 TCP peer/XFF，31 个不同合成邮箱；不伪造时 1 次 429，变化 CF-Connecting-IP 时 0 次 429。实际调用登录 handler，并非只检查日志。Caddy 默认保护 XFF，但普通 CF-Connecting-IP 不自动清洗；可信边缘若可靠覆盖该头可改变结论。保留 medium/P1，未证明生产容量攻击或无限猜某一个账号密码。 |
| [03 重置密码与旧密码登录竞态](../2026-09-06-relay-backend-review/finding-03.md) | 成立 | 包装器先调用真实密码验证，只暂停返回时机，不伪造成功。重置确实删除旧会话，随后恢复在途 Login，仍产生可认证的新会话。属于业务 TOCTOU，race detector 通过不能排除。只证明已经在途且使用旧密码的请求窗口，不代表重置后新发起的旧密码登录也成功。 |
| [04 截断响应被视为成功](../2026-09-06-relay-backend-review/finding-04.md) | 成立 | 真实 HTTP server/client 验证：上游 chunked 响应缺少终止块，下游却收到 200、3 字节完整响应，io.ReadAll 无错误。Go 上游解码器确实产生 UnexpectedEOF，但 Gateway 丢弃 io.Copy 错误。测试覆盖小 HTTP/1.1 chunked 响应，不等于所有下载或官方客户端必然静默保存损坏文件。 |
| [05 没有等待优雅关闭](../2026-09-06-relay-backend-review/finding-05.md) | 成立；独立新二进制复现 | 活跃请求已收到 100 Continue，128 字节请求体尚未发送；向自行启动的临时子进程发 SIGTERM 后约 0.0197 秒退出、exit=0、客户端 EOF。主流程确实没等待 Shutdown goroutine。10 秒是最大等待窗口，不是每次应强制等待满 10 秒；本例成立在于仍有活跃请求。 |
| [06 绑定记录无自动删除](../2026-09-06-relay-backend-review/finding-06.md) | 成立；性能影响规模未测量 | 同一合成 device 的 32 个不同合法匿名 code 创建不同 SQLite 行；清理时间推进一年后仍保留 expired 行。TTL 使状态失效，但不删除；相同 code 的幂等不能约束不同 code。未做洪泛测试、容量压测或生产 DB 检查，不能宣称当前已变慢或磁盘耗尽。 |

原报告矩阵为 security 3 项（P0 1、P1 2）、bug 2 项（P1）、performance 1 项（P1）。本轮未新建六个重复问题；上表强调原发现的适用条件，其中 Cookie 项不应无条件继承生产 P0。

## 关键源码证据

- Cookie：`cloud/app/identity_handlers.go:140-157` 设置/读取 host-only、Path=/、HttpOnly、SameSite=Lax 的会话；`cloud/internal/gateway/http.go:143-159` 未剔除控制面 Cookie；`cloud/app/app.go:151-162` 同主机挂载控制面与 `/n/`。
- 来源：`cloud/app/identity_handlers.go:182-193` 优先信任 CF Header；`cloud/internal/identity/service.go:325-345` 真正使用该来源限流。
- 改密：`cloud/internal/identity/service.go:131-151` 验证和创建会话分离；`cloud/internal/store/sqlite_identity.go:236-292` 新建会话不比较已验证密码版本，重置只删除提交当时的会话。
- 截断：`cloud/internal/gateway/http.go:72-85` 忽略 body 复制错误并正常返回。原报告对 Transfer-Encoding 被移除的机制略不精确：Go 的 HTTP 解析器已处理该头，根因是吞掉复制错误，而非单独某个删头动作。
- 退出：`cloud/cmd/mindfs-relay/main.go:112-132` 在 goroutine 中调用 Shutdown，却在 ListenAndServe 返回后直接结束主流程；`application.Close()` 不负责 HTTP handler 排空。
- 增长：`cloud/internal/store/sqlite_binding.go:24-75` 为新 code 插入；`cloud/internal/store/sqlite_sessions.go:8-30` 对绑定记录只有状态 UPDATE，没有 DELETE。

## 本轮实际执行

### 五项 Go 复现

先完整读取 overlay 与三个注入测试文件，并核对辅助函数使用临时 SQLite、假邮件发送器、回环 HTTP/net.Pipe。剔除继承的所有 `MINDFS_*` 环境后，在 cloud module 执行：

```sh
go test -overlay=/tmp/mindfs-backend-review.epfP86/overlay.json \
  ./app ./internal/identity ./internal/gateway \
  -run '^TestAudit' -count=1 -v -timeout=60s
```

五项全部 PASS，含义是**缺陷现象成功复现**，不是缺陷修复后的回归通过：

- `TestAuditRelaySessionCrossesNodeBoundary`
- `TestAuditSpoofedSourceBypassesRateLimit`
- `TestAuditAnonymousBindChallengesSurviveExpiry`
- `TestAuditPasswordResetAllowsInflightOldPasswordLogin`
- `TestAuditTruncatedChunkedResponseLooksSuccessful`

新执行日志：`/private/tmp/claude-501/-Users-hongweizhang-java-project-mindfs/8b30ef5a-922c-40ad-a2d2-4c855063eb66/tasks/bn6529l95.output`。

### 退出实验

材料：`/tmp/mindfs-backend-shutdown-verify.H6embl/`。从当前 cloud 源码重新 `go build` 到该目录，不复用原报告中来源未重新确认的二进制。测试进程使用临时 HOME、数据库、资产和合成 SMTP 配置，只监听随机回环端口，只发送未完成的登录请求，不调用发码/邮件接口。

```json
{
  "reproduced": true,
  "pid": 43410,
  "active_request_confirmed": "100 Continue",
  "request_body_sent_bytes": 0,
  "declared_request_body_bytes": 128,
  "exit_code": 0,
  "exit_after_sigterm_seconds": 0.0197,
  "client_received_eof": true,
  "configured_shutdown_timeout_seconds": 10
}
```

首轮将模拟 SMTP host 改为 loopback 时被项目的固定 QQ SMTP 校验拒绝，进程未开始监听；这不计入缺陷复现。恢复符合校验的合成配置后才得到上面的结果。原失败日志保留为 `server.log`，成功实验日志为 `server-valid-config.log`，没有用后一次结果覆盖前一次日志。

### 依赖扫描核验

报告中的 8 个 Go advisory ID 已逐一通过 `vuln.go.dev` 官方 JSON 核对，条目真实、主要修复版本吻合。另从已缓存 `golang.org/x/vuln@v1.7.0` 源码构建扫描器，对当前 cloud module 重扫，再次得到相同 **8 个符号层 advisory 命中**：

```text
GO-2025-3595
GO-2026-5026
GO-2026-5972
GO-2026-6089
GO-2026-6090
GO-2026-6091
GO-2026-6218
GO-2026-6278
```

证据目录：`/tmp/mindfs-backend-vuln-verify-ijf_ofw1/`，原始 JSON 为 `govulncheck-retry.json`。

诚实记录两个工具细节：

- 首次 `go run ...@v1.7.0` 因 `GOPROXY=off` 下的 deprecation 查询失败，不能将其零结果当无漏洞；随后直接从缓存源码构建工具才成功扫描。
- 本地源码构建的扫描器自报版本是 `v0.0.0`，源码目录版本为 v1.7.0；不冒称其版本字符串是发布包 v1.7.0。JSON 模式退出码 0 也不意味着零告警。

**符号可达不等于可被公网利用。**例如 Gorilla 项涉及客户端 mask，Cloud 业务入站 WS 使用服务端 Upgrader；Go stdlib 命中基于本机 go1.26.5，不证明线上镜像工具链相同。GO-2026-6278 在官方库中还标为 UNREVIEWED，更新依赖可作为维护动作，不能单凭数量宣布 8 个公网漏洞。

## 与用户当前正常使用的关系

这些是后端身份隔离、限流、时序、传输故障和清理问题，不能用之前“本机未启用内建 autostart/E2EE”来否定。Cloud 登录 Cookie 与 E2EE 配对密钥是不同层；反过来，源码缺陷可复现也不证明当前用户已经受影响。

本轮没有核实真实用户/节点信任关系、完整入口代理链、线上 DB 大小、线上 Cloud 二进制或浏览器 Cookie 发送情况。此前本机正常使用仍成立。

## 建议

- 将报告当作**有证据的待修复清单**，而非已发生的生产故障清单。
- 如 Relay 对不可信用户开放，优先核验并处理 Cookie 控制面隔离与真实入口 Header 信任链；个人全可信环境则不要仅因表格里的 P0 就停机。
- 改密竞态、响应截断、优雅退出、绑定记录清理均值得按使用场景加入回归并排期，不需要声称已出现攻击或数据损坏才能修复。
- 修复仍需另行授权；本轮不改原报告和业务源码，不触碰生产数据库、不发邮件、不重启现有服务。原本机 MindFS PID 82054 与启动时间保持不变。

## 外部一手参考

- [Caddy reverse_proxy 请求头默认行为](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy#headers)
- [GO-2026-6278 官方数据](https://vuln.go.dev/ID/GO-2026-6278.json)
- [GO-2026-6218 官方数据](https://vuln.go.dev/ID/GO-2026-6218.json)
- [GO-2026-6091 官方数据](https://vuln.go.dev/ID/GO-2026-6091.json)
- [GO-2026-6090 官方数据](https://vuln.go.dev/ID/GO-2026-6090.json)
- [GO-2026-6089 官方数据](https://vuln.go.dev/ID/GO-2026-6089.json)
- [GO-2026-5972 官方数据](https://vuln.go.dev/ID/GO-2026-5972.json)
- [GO-2026-5026 官方数据](https://vuln.go.dev/ID/GO-2026-5026.json)
- [GO-2025-3595 官方数据](https://vuln.go.dev/ID/GO-2025-3595.json)
