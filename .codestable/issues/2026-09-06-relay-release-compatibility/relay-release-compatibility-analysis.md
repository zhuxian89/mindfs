---
doc_type: issue-analysis
issue: 2026-09-06-relay-release-compatibility
status: confirmed
root_cause_type: logic
related: [relay-release-compatibility-report.md]
tags: [cloud-relay, upstream-compatibility]
---

# Relay 发布兼容修复方案

## 1. 根因定位

- `cloud/internal/gateway/http.go` / `websocket.go` 从解码 Path 路由，`cloneRequest` 清空 RawPath，丢失签名路径。
- `cloud/internal/assetsync/service.go` 已支持正式版本增量下载及 hash 校验，但部署没有独立于应用升级的刷新入口，也没有只读核对最新 release 覆盖的命令。
- `cloud/compat` 仅验证 ASCII API 和静态 fixture，未包含月度新接口和编码参数。

## 2. 采用方案

用户已授权上一轮给出的修复建议，本记录固定实现范围，不再重复请求许可。

1. Gateway 以 EscapedPath 切分 `/n/{nodeId}`；节点 ID 解码后校验，转发 URL 同时保存 decoded Path 与对应 RawPath。HTTP/WS 共享逻辑，业务 query 不重编码。
2. 增加 `check-assets <target-dir>`，只读校验当前入口、官方受支持 release 列表及其 marker/hash；缺失或无法查询即返回非零。复用已有列表筛选/marker 校验，避免逐请求联网或全量哈希。
3. 增加 `cloud/deploy/refresh-assets.sh`，按顺序执行现有 sync 和只读 check，失败立即停止；同步期间不停止 Relay，不改路由、不删除卷。部署流程和 Node 独立升级流程复用它；可由既有任务调度器调用，不自动安装任务。
4. 永久测试覆盖特殊字符 HTTP/WS 转发、真实 Node E2EE 编码路径、新增 API（真实额度只测无副作用的错误返回）和资源检查失败分支。

## 3. 影响与约束

- Gateway 改动影响 HTTP/WS 握手，不改消息帧；测试覆盖冒号、编码斜线、百分号、大小写转义、Unicode、query、根路径。
- 资源检查是显式运维命令，可能进行较多磁盘 hash 读取，不放在 `/readyz` 或请求热路径；依赖 GitHub 可用，失败不终止已运行 Relay。
- 旧资源只增不删，已有 immutable 名称冲突策略保留；不声称自动修复内容冲突。
- CLI autostart 与 Web 跨节点缓存是上游客户端问题。本次 Cloud 修复不能消除；保留原 findings 为 open 并明确配置规避/验收边界，不通过修改客户端或注入脚本绕过。

## 4. 文件范围

`cloud/internal/gateway/http.go`、`websocket.go` 及 gateway 测试；`cloud/internal/assetsync/service.go`、新增覆盖检查及测试；`cloud/cmd/mindfs-relay/main.go`、`main_test.go`；`cloud/deploy/refresh-assets.sh`、相关脚本验证及 README；`cloud/compat` 的 suite/E2EE/API/资源验证和 README；相关 issue、audit 状态及 `.codestable/architecture/cloud-relay-core.md`。

## 5. 验证

定向失败复现 → 修复后成功；全 Cloud race + vet；未修改源码 Node 与正式 v0.5.0 二进制兼容验证；公开 Linux release 资源导入和覆盖检查；脚本失败停止行为；文档 YAML/link 与 diff 检查。所有进程/数据使用隔离临时目录。
