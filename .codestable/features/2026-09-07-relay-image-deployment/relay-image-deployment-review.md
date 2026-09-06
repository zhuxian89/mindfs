---
doc_type: feature-review
feature: 2026-09-07-relay-image-deployment
status: passed
reviewer: subagent
reviewed: 2026-09-07
round: 1
---

# Relay 镜像部署提交前审查

## 1. Scope And Inputs

- 来源：用户要求 push 的 ad-hoc/pre-merge 审查，关联本 feature 已批准设计。
- Design / Checklist：同目录 relay-image-deployment-design.md、relay-image-deployment-checklist.yaml；steps 全 done。
- Implementation evidence：relay-image-deployment-implementation.md 与本轮实际命令结果。
- Evidence pack / DoD results：none（非 goal/gate 编排）。
- Gate results：旧项目缺少工作区门禁工具，沿用用户批准的 worktree-override.md 人工范围核对，不声称自动门禁通过。
- Diff basis：以 60c34f0 为基线，审查部署相关 tracked diff 和新增文件。
- Baseline dirty files：cloud/app/ 三个 tracked 修改及 pages/、relay_pages.go；暂停 feature 2026-08-05-relay-local-service-domains。均不属于本次提交。IDE 选中的 design/relay-ui-concepts/README.md 也不纳入。

### Independent Review

- Detection：有原生 Agent，无 Paseo；ocr CLI 未找到。
- 环节 A：native-agent，completed；独立上下文审查覆盖范围内代码、测试、文档及必要健康接口契约。
- 环节 B：not-available，没有启动 OCR，也未安装新工具。
- OCR severity mapping：不适用。
- Merge policy：主对话按源码、测试与真实端点定义逐条核验；没有采纳缺乏证据的 blocking。未发布 SHA 的限制归入已发布版本选择边界，而不是宣称 digest 可代替不存在的镜像。
- Gate effect：所有已启动审查均已返回；无 blocking/important。

## 2. Diff Summary

- 新增：build-relay.yml、sync-upstream.sh、同步与部署的 Go 离线测试，以及本 feature 文档。
- 修改：1Panel Compose、auto-upgrade.sh、cron wrapper、deploy_test.go、部署 README、状态 ignore 与架构部署段落。
- 删除：none。
- Staged：审查时尚未暂存；后续仅按本 scope 逐文件暂存。
- 风险热点：镜像发布时序、持久卷身份、失败重试、私有配置与维护锁；不修改应用协议或数据库 schema。

## 3. Adversarial Pass

- 假设生产故障：标签移动导致维护步骤使用不同镜像，或迁移/探活失败后仍记录成功并永久跳过。
- 检查反例：同 marker 但容器镜像漂移/不健康；多 RepoDigests；拉取、同步、校验、迁移、重建和公网检查失败；项目/service/卷不匹配；push 拒绝后重试；锁竞争与 cron 输出含 SKIPPED 的失败。
- 结果：代码保持 digest、项目和卷约束，成功记录最后同文件系统原子替换，故障测试覆盖相关停止/重试分支。已有 healthz/readyz 实际返回 status=ok/ready，与新解析逻辑一致。

## 4. Findings

### blocking

none。

### important

none。

### nit

- REV-001 `cloud/deploy/auto-upgrade.sh:31-42`：容器完全停止和项目/卷不匹配共用错误文案，定位方向不够清楚；README 已明确停止容器须先诊断恢复。非本次推送阻塞。

### suggestion

- 连续 push 的中间 pending run 可能被 GitHub concurrency 替换。只能选择已成功发布的 SHA 或 digest；不能假设每个提交都有可部署镜像。

### learning

- manifest digest 与容器 image ID 需先解析再比较；无法唯一解析时失败关闭。
- SIGKILL 不会运行 EXIT trap；私有 check.* 目录可能残留。权限仍受 0700/0600 保护，不能承诺所有终止方式都清理临时文件。

### praise

- 发布、同步和部署职责明确；不新增 SSH 自动部署。
- 项目和三个持久卷先核对，备份/资源检查保留，失败不覆盖成功记录。
- 使用临时本地 Git 仓库与受限假命令测试，不以真实生产操作充当测试。

## 5. Test And QA Focus

本轮已执行：

- `go -C cloud test ./...`：全部通过，deploy 86.459s。
- 将 HEAD 导出到临时目录，仅覆盖本轮部署文件，不包含用户未提交 app 工作；该副本的 Cloud 全量测试全部通过，deploy 87.544s。
- 三个维护脚本 `bash -n`、refresh-assets.sh `sh -n`：通过。
- `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/build-relay.yml`：通过。
- `git diff --check`：通过。
- 原 App tracked diff 及五个 untracked 页面/Go 文件指纹：与整改前一致。
- 本 scope 的 GitHub Token/私钥格式扫描：未命中；不等同于完整秘密审计。

后续实际环境重点：首次 Actions 的 Linux flock/Git 测试、多架构镜像发布、服务器私有拉取授权、原项目/卷身份，以及一次经授权的真实升级。当前审查不要求执行服务器部署。

## 6. Residual Risk

- macOS 跳过真实 util-linux flock 竞争，Linux Actions 在发布前执行该测试。
- 独立 refresh-assets.sh 不参与维护锁，运维须避免与部署重叠；已在 README/架构说明。
- 迁移或资源同步失败可能已经改变持久状态；容器完全停止时不能在线备份。保留人工恢复边界，不自动回退数据库。
- SIGKILL 后可能留下受限权限的临时配置目录，需运维在确认没有运行任务后处理。
- 此报告不等于 GHCR 发布或生产验收；以实际 Actions/服务器结果为准。

## 7. Verdict

Status: passed。无 blocking/important；允许按用户明确授权 scoped commit，并以非 force 方式推送至 origin/main。只触发镜像发布，不执行服务器部署；后续公开运行状态需另行核验。
