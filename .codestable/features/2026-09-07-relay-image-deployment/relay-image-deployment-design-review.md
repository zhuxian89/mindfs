---
doc_type: feature-design-review
feature: 2026-09-07-relay-image-deployment
status: passed
reviewed: 2026-09-07
round: 1
---

# Relay 镜像部署方案审查

## 1. Scope And Inputs

审查输入是会话内完整计划及对应代码事实；design/checklist 为用户批准后同一契约的项目内归档。相关文档：mindfs-compatible-cloud-backend requirement、cloud-relay-core architecture。核验 Dockerfile、两个 Compose、auto-upgrade/cron/refresh-assets 脚本与既有 Go 测试。

### Independent Review

- Status: completed
- Detection: native-agent
- Provider / agent: Plan agent ad142b2461d795f1c
- Raw output: 会话内独立方案审查结果；提出私有包凭据、卷身份、Compose 参数、锁测试和 latest 发布顺序问题。
- Merge policy: 主对话按代码及本机 Compose 帮助逐条核验。私有/公开选择属于真实部署前提而非代码阻塞；补充卷身份检查、真实参数及离线测试后交由用户批准。
- Gate effect: 用户已通过 ExitPlanMode 批准修订后的计划；没有待返回 reviewer。

## 2. Design Summary

三个职责分离：源码同步、Actions 发布、独立镜像部署。四个实现步骤；每个核心场景追溯到测试或明确的外部验收边界。基线部署测试通过，保留已存在的 app 改动。

## 3. Findings

### blocking

none（项目/卷身份检查已写入计划与验收契约）。

### important

none（已明确私有包授权前提；Compose 2.29.7 run 无 --pull，已调整为缓存 digest；上游同步需支持 push 失败重试）。

### nit

none。

### suggestion

none。

### learning

manifest digest 与容器 image ID 不同，必须先解析再比较；缓存的 digest 不应退回移动标签。

### praise

沿用单实例 Compose 与既有资源覆盖检查，不引入 SSH 自动部署或调度平台。

## 4. User Review Focus

已确认独立执行部署、不发布本地未提交工作、不执行生产。本次先交付脚本和离线测试；服务器凭据、版本及初次升级另行验证。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | design 第3节/checklist S1-S4 | 实现故障注入测试 |
| DoD Contract | pass | E | design 第3-4节 | 如实记录外部未运行项 |
| Steps and checks traceability | pass | E | checklist source | 每步即时记录证据 |
| Roadmap compliance | n/a | E | 非 roadmap 条目 | none |
| Module interface | pass | C | 现有 Ops 与 Compose | 固定 digest/项目环境 |
| Validation and artifacts | pass | C | 既有脚本桩模式和本地基线 | 回归与独立 diff review |

Summary: E=4, C=2, H=0, H-only core checks=none。

## 6. Residual Risk

同类宿主独立上下文审查并非异构 provider；真实 GHCR 多架构构建、镜像拉取及生产升级尚未执行，不能以离线测试替代。Linux flock 真并发由 CI 验证，macOS 用替身覆盖分支。

## 7. Verdict

Status: passed。用户已批准整体计划，按已批准契约实施；后续仍需独立代码审查和 QA，不将本报告当成生产验收。
