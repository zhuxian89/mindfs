---
doc_type: audit-finding
audit: 2026-09-06-relay-backend-review
finding_id: bug-04
nature: bug
severity: P1
confidence: high
suggested_action: cs-issue
status: fixed
resolved_in: working-tree
---

# Finding 04：上游响应截断被包装成成功下载

## 2026-09-06 处理进度

已在当前工作区完成源码修复和回归验证，尚未部署到生产。具体行为、测试和部署注意见 [修复记录](../../issues/2026-09-06-relay-backend-hardening/relay-backend-hardening-fix-note.md)。以下保留修复前的审计证据。

## 速答

节点已开始响应后，Relay 忽略复制 body 的错误并正常结束 HTTP handler。对于没有原始 Content-Length 的 chunked 响应，节点意外断连会被转换为一个协议完整的 200 响应，客户端可能把不完整文件当成下载成功。

## 关键证据

- `cloud/internal/gateway/http.go:81` 去除 hop-by-hop 头后提交响应，再丢弃复制错误：

  ```go
  removeHopHeaders(response.Header)
  copyHeaders(w.Header(), response.Header)
  w.WriteHeader(response.StatusCode)
  _, _ = io.Copy(w, response.Body)
  return nil
  ```

- `cloud/internal/gateway/http.go:170` 去除 `Transfer-Encoding`，由公网 HTTP Server 重新编码 body。上游不完整的 chunk 边界本来会通过 body 读取错误暴露，但该错误被丢弃。
- `cloud/app/gateway_handler.go:19` 只有 gateway 返回错误时才进入错误处理；目前此路径始终返回 nil，日志和请求指标也不会体现传输失败。

## 定向复现

`TestAuditTruncatedChunkedResponseLooksSuccessful` 使用 `net.Pipe` 模拟节点，真实回环 HTTP Server 作为公网侧：

```text
HTTP/1.1 200 OK
Content-Type: application/octet-stream
Transfer-Encoding: chunked

3
abc
[连接关闭，缺少最后的 0 长度 chunk]
```

真实 HTTP 客户端收到 `200`，body 为 `abc`，`Content-Length: 3`，`io.ReadAll` 返回 nil error；gateway 同样返回 nil。公网侧自动补出了一个完整的小响应，抹去了上游截断的证据。

## 影响与边界

触发条件是响应已经开始、上游随后异常结束，并且接收方没有其他完整性机制。若原始 Content-Length 仍被保留，客户端通常可以识别长度不符；此复现明确针对 chunked/未知长度路径。带独立校验和或完整性校验的上层格式可能仍能发现缺失，本次不宣称所有下载或 E2EE 都会静默损坏。

## 修复方向与建议动作

`cs-issue`：保留复制错误，并在响应头已提交后中止下游响应（例如按 Go 反向代理语义使用 `http.ErrAbortHandler`），让客户端观察到传输失败；同时记录低敏错误指标。此时追加一段 JSON 错误会污染原 body，不能作为修复。
