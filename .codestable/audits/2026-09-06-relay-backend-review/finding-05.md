---
doc_type: audit-finding
audit: 2026-09-06-relay-backend-review
finding_id: bug-05
nature: bug
severity: P1
confidence: high
suggested_action: cs-issue
status: fixed
resolved_in: working-tree
---

# Finding 05：主流程没有等待 Shutdown，优雅关闭失效

## 2026-09-06 处理进度

已在当前工作区完成源码修复和回归验证，尚未部署到生产。具体行为、测试和部署注意见 [修复记录](../../issues/2026-09-06-relay-backend-hardening/relay-backend-hardening-fix-note.md)。以下保留修复前的审计证据。

## 速答

收到 SIGTERM 后，Shutdown 运行在另一个 goroutine。它关闭 listener 时 ListenAndServe 立即返回，主流程随即关闭 App 并退出，没有等待 Shutdown 完成，正在处理的请求会被提前打断。

## 关键证据

- `cloud/cmd/mindfs-relay/main.go:120` 异步执行 Shutdown：

  ```go
  go func() {
      <-runCtx.Done()
      shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
      defer cancel()
      if err := server.Shutdown(shutdownCtx); err != nil { /* log */ }
  }()
  ```

- `cloud/cmd/mindfs-relay/main.go:129` 对 ErrServerClosed 直接返回，没有完成信号或 join：

  ```go
  if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
      return err
  }
  return nil
  ```

- `cloud/cmd/mindfs-relay/main.go:112` 的 defer 调用 App.Close；`cloud/app/app.go:113` 起关闭 Registry、资源根和 Store，甚至早于其他 HTTP handler 排空。

## 真实进程复现

1. 从当前 Cloud 源码构建 `/tmp/mindfs-backend-review.epfP86/mindfs-relay`。
2. 使用临时数据目录、合成配置和回环端口启动独立子进程。
3. 发送带 `Expect: 100-continue` 的登录请求，声明 body 长度但暂不发送 body。
4. 收到 `HTTP/1.1 100 Continue`，确认 handler 已尝试读取请求体；随后仅向此测试进程发送 SIGTERM。
5. 请求仍未完成时，进程约 **0.009 秒**后以 0 退出，未等待代码设置的 10 秒窗口。

复现脚本为 `shutdown_probe.py`，结果保存在 `shutdown-result.json`。没有向现有服务发送信号，也没有发送 SMTP 邮件。

## 影响与边界

正常长期运行不会触发；部署重启、容器停止或人工终止时，长 HTTP 请求、上传下载和控制面请求可能中断。WebSocket 是 hijacked 连接，本来就需要独立的关闭策略，不能单靠 net/http Shutdown 排空；本项首先证明普通 HTTP 的等待流程也没有生效。

## 修复方向与建议动作

`cs-issue`：主流程等待 Shutdown 完成或超时，然后按顺序释放 Registry 和 Store；为 Connector/WS 明确关闭策略，保证普通 HTTP 的排空期间依赖仍可用。
