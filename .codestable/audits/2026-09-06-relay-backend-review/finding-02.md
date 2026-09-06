---
doc_type: audit-finding
audit: 2026-09-06-relay-backend-review
finding_id: security-02
nature: security
severity: P1
confidence: medium
suggested_action: cs-issue
status: fixed
resolved_in: working-tree
---

# Finding 02：来源 IP 请求头可伪造，跨邮箱限流可绕过

## 2026-09-06 处理进度

已在当前工作区完成源码修复和回归验证，尚未部署到生产。具体行为、测试和部署注意见 [修复记录](../../issues/2026-09-06-relay-backend-hardening/relay-backend-hardening-fix-note.md)。以下保留修复前的审计证据。

## 速答

身份接口无条件信任 `CF-Connecting-IP` 和 `X-Forwarded-For`。如果入口没有可信地覆盖或移除这些头，同一个请求来源可以不断改变头值，绕过每来源每小时 30 次的限制。

## 关键证据

- `cloud/app/identity_handlers.go:182` 在检查 TCP peer 前，直接采用客户端 Header：

  ```go
  if value := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); value != "" {
      return value
  }
  if value := strings.TrimSpace(strings.SplitN(r.Header.Get("X-Forwarded-For"), ",", 2)[0]); value != "" {
      return value
  }
  ```

- `cloud/internal/identity/service.go:332` 把上述 `source` 的 HMAC 作为限流 subject。HMAC 隐藏原值，但不能证明来源可信。
- `cloud/internal/identity/service.go:131` 对不存在的账号仍执行 dummy Argon2 校验；`cloud/internal/identity/password.go:32` 的默认内存成本为 64 MiB，接口中没有并发校验上限。
- `cloud/deploy/Caddyfile.example:3` 仅配置 `reverse_proxy relay:8080`，没有清洗 `CF-Connecting-IP` 或限制为可信 Cloudflare 来源的规则。应用本身也没有 trusted-proxy 配置。

## 定向复现

`TestAuditSpoofedSourceBypassesRateLimit` 使用真实 App、相同 `RemoteAddr` 和固定 `X-Forwarded-For`，依次对 31 个不同合成邮箱发起错误密码登录：

| 请求方式 | 401 | 429 |
|---|---|---|
| 不伪造 CF 头 | 30 | 1 |
| 每次更换 CF-Connecting-IP | 31 | 0 |

测试使用低成本测试 hasher，避免对本机施加高内存压力；没有进行压力测试或真实邮件发送。应用层绕过已确认。由于未核验生产入口清洗规则，整体置信度保留为 medium。

## 影响与边界

- 单邮箱每小时 10 次的限制仍然存在，本项不能表述为“对同一账号无限暴力破解”。
- 攻击者可以跨邮箱喷洒密码、调用验证码接口消耗邮件额度，或触发大量 Argon2 计算。具体容量影响取决于并发数和机器资源，本次未压测。
- 如果生产强制所有请求经过可信边缘，并保证请求头被可信地重写，外部可利用性会降低；应用仍不应把未验证的头当成可信 peer。

## 修复方向与建议动作

`cs-issue`：只从可信代理接受转发来源，入口清洗客户端提供的来源头；应用侧基于明确的代理信任规则选取客户端 IP，并为高成本密码校验提供独立并发预算。
