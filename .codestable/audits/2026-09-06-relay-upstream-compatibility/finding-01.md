---
doc_type: audit-finding
audit: 2026-09-06-relay-upstream-compatibility
finding_id: bug-01
nature: bug
severity: P1
confidence: high
suggested_action: cs-issue
status: open
regression: introduced-in-2d76ff9-to-3471043
---

# Finding 01：新版自启动会丢弃自定义 Relay 环境变量

## 速答

若用户通过 `MINDFS_RELAY_BASE_URL` 指定自研 Relay，并启用新版 `--autostart`，从没有该变量的服务管理器环境启动时，环境快照及登录 shell 的恢复都会丢弃它。若节点配置文件没有相同地址兜底，节点会改用其他 Relay 地址，现有地址不匹配处理还会清除绑定凭据。

这是本次新增自启动路径的缺陷；继续沿用已有启动方式，不会仅因更新源码而自动触发。

## 关键证据

- `cli/cmd/autostart.go:42-58`：先后对快照与 shell 输出调用同一个过滤恢复函数：

  ```go
  applyEnvironment(snapshot.Environment, true)
  shellEnv, err := readShellEnvironment(snapshot.Shell, autoStartShellTimeout)
  // ...
  applyEnvironment(shellEnv, true)
  ```

- `cli/cmd/autostart.go:156-165,169-172`：过滤器将所有 `MINDFS_` 都当作不可恢复项，并非仅过滤内部控制变量：

  ```go
  if shouldSkipRestoredEnvironment(key) { continue }
  // ...
  if strings.HasPrefix(upper, "MINDFS_") {
      return true
  }
  ```

- `cli/cmd/autostart_unix.go:103-128`：生成的 launchd plist / systemd unit 只有程序参数等内容，没有注入自定义 Relay 环境变量。
- `cli/cmd/autostart.go:313-340`：持久启动参数保留 `--agent-config` 等选项，但没有 Relay URL 参数；只有显式配置文件兜底时才能规避本项。
- `server/internal/relay/manager.go:66-73`：优先使用 `MINDFS_RELAY_BASE_URL`，为空就使用传入配置地址。`server/app/server.go:96-99` 的配置来源为 agent config；仓库 `agents.json:484` 的默认地址是官方 Relay。
- `server/internal/relay/manager.go:94-102`：若解析出来的 Relay base 与已保存 endpoint 不匹配：

  ```go
  if relayBaseMismatch(m.relayBase, creds.Relay.Endpoint) {
      if clearErr := m.service.store.Clear(); clearErr != nil { /* ... */ }
      m.lastError = "relay base changed, rebinding required"
      creds = Credentials{}
  }
  ```

这条地址不匹配保护本身是旧逻辑；新增环境过滤使原本有效的自研 Relay 配置在自启动场景下消失，从而进入该保护路径。

## 定向复现

使用当前两个自启动源码文件的临时副本，未运行安装/删除自启动条目的函数。测试使用临时 HOME、临时配置目录和只打印测试环境的 shell 脚本：

1. 快照包含 `MINDFS_RELAY_BASE_URL=https://relay.audit.invalid`。
2. 模拟登录 shell 同样输出该变量。
3. 自启动进程初始环境不含该变量。
4. 调用真实 `prepareAutoStartEnvironment()`。

实际断言结果：

```text
--- FAIL: TestAuditAutostartRestoresCustomRelay
custom relay lost after snapshot AND login-shell restore:
got "", want "https://relay.audit.invalid"
```

复现文件位于本次会话临时目录 `/tmp/mindfs-relay-audit.L1Pbt6/autostart_relay_audit_test.go`；未连接网络或读写用户真实凭据。此结果只证明配置恢复缺陷，未实际清除任何绑定来演示后果。

## 影响与边界

- 用户当前 shell 确实设置了 `MINDFS_RELAY_BASE_URL`，因此该配置方式与用户有关。
- 尚未确认用户线上节点是否采用新版自启动，或配置文件是否另有正确 Relay 地址；不宣称生产已经受影响。
- 若服务管理器显式注入该变量，或 agent config 持久保存相同地址，本项不会触发。
- 这是节点启动兼容性问题，不是自研 Cloud wire protocol 不兼容。

## 修复方向与建议动作

建议 `cs-issue`：明确区分必须过滤的内部控制变量与应持久保留的 Relay 配置，确保新启动方式保留已选 Relay；在修复前不要无验证地替换既有生产节点启动方式。本审计不修改实现。
