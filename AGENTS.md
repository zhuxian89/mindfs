# 项目修改范围

本仓库是 MindFS 的 fork。项目自行开发和维护的是 Cloud Relay 后端。

- 代码修改仅限 `cloud/` 下的 Relay 后端源码及其对应测试。
- 上游 MindFS 代码一律只读，包括 `server/`、`web/` 和各平台客户端。`server/internal/relay/` 属于上游节点端，同样只读。
- 排查问题时可以阅读上游代码。若根因在上游，指出具体位置和依据，只在 Relay 后端范围内实施修复。
