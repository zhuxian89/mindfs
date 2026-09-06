---
doc_type: feature-implementation
feature: 2026-09-07-relay-image-deployment
status: implemented
summary: 发布、上游同步与独立镜像部署已实现，离线回归通过
---

# 实现完成汇报

## 动了哪些文件

以下是收尾时的完整 git status。`cloud/app/` 与 `2026-08-05-relay-local-service-domains/` 是接续前已有工作，本轮未修改。`build-relay.yml`、生产 Compose、部分部署测试、同步脚本初稿、状态忽略规则和方案文档也继承自此前会话；本轮补齐同步测试、部署流程、故障测试及说明。

```text
 M .codestable/architecture/cloud-relay-core.md
 M .gitignore
 M cloud/app/app_test.go
 M cloud/app/binding_handlers.go
 M cloud/app/relay_browser_handlers.go
 M cloud/deploy/README.md
 M cloud/deploy/auto-upgrade-cron.sh
 M cloud/deploy/auto-upgrade.sh
 M cloud/deploy/deploy_test.go
 M cloud/deploy/docker-compose.1panel.yml
?? .codestable/features/2026-08-05-relay-local-service-domains/
?? .codestable/features/2026-09-07-relay-image-deployment/
?? .github/
?? cloud/app/pages/
?? cloud/app/relay_pages.go
?? cloud/deploy/auto_upgrade_test.go
?? cloud/deploy/sync-upstream.sh
?? cloud/deploy/sync_upstream_test.go
```

## 改了哪些函数 / 类型

| 步骤 | 文件与入口 | 最终行为 |
| --- | --- | --- |
| S1 发布配置 | `.github/workflows/build-relay.yml:1`、`cloud/deploy/docker-compose.1panel.yml:1`、`cloud/deploy/deploy_test.go:44` | main 测试后发布 SHA 镜像；核对 main 再推广 latest；生产双服务共享镜像，默认 Compose 保留 build |
| S2 同步入口 | `cloud/deploy/sync-upstream.sh:8` main、`cloud/deploy/sync_upstream_test.go:98` 等测试 | 保护已有 Git 操作和本地工作，快进 origin、合并 upstream、推送未发布提交，push 失败可重试；不调用 Docker |
| S3 部署入口 | `cloud/deploy/auto-upgrade.sh:8` read_config、`:29` verify_identity、`:59` running_target、`:65` verify_http、`:103` main | 私有 JSON 解析、项目与卷守护、固定 digest、完整升级验证与原子成功标记 |
| S3 调度输出 | `cloud/deploy/auto-upgrade-cron.sh:1`、`cloud/deploy/auto_upgrade_test.go:209` 等测试 | 相对脚本路径、只静默成功跳过，失败保留退出码和输出；覆盖升级阶段失败、错卷、失败重试及锁分支 |
| S4 运维交付 | `cloud/deploy/README.md:120`、`.codestable/architecture/cloud-relay-core.md` 部署段落 | 说明首次切换、权限、原项目/卷保留、独立执行及故障处理 |

`.gitignore` 忽略部署状态目录。临时完整配置为私有文件，正常成功/失败均清理；没有打印配置凭据。同步/部署的测试分别归属两个测试文件，没有扩展应用协议或引入部署框架。

## 是否触碰方案外文件或引入新概念

没有修改 app、客户端或暂停 feature。27 个既有保护文件的 SHA256 与接续时一致。

没有新增产品概念。实现使用 Python 3 标准库解析结构化配置与容器挂载，依赖已回填 design/README；没有新增 Python 第三方包。服务器不再需要 Go/Node 构建环境和原来的构建内存检查。

## 代码质量反射检查

部署入口按配置读取、身份检查、实际运行状态、HTTP 验证和主流程划分函数；同步逻辑独立保留在自身脚本。没有万能 helpers 或方案外重构。故障分支均对应已批准的升级保护与重试契约。

## 推进顺序退出信号

| 步骤 | 结果 | 证据 |
| --- | --- | --- |
| S1 | done | 生产/本地 Compose 契约测试与合成配置通过；actionlint v1.7.7 通过；六个固定 action commit 经 GitHub API 确认存在 |
| S2 | done | 临时本地 Git 仓库用例通过，包括 push 拒绝后重试、冲突、脏工作区、已有 merge/rebase、分支、origin 分叉和 untracked 文件冲突 |
| S3 | done | 19 个阶段故障注入后保留成功记录并可重试；14 类身份/镜像/资源验证不匹配阻止成功；显式 digest、Compose 文件继承、cron 与锁分支通过 |
| S4 | done | Cloud 全量测试、Shell 语法、合成 Compose、diff 检查与主代理单独复核通过，说明与架构已更新 |

S4 审查方式明确调整为本轮主代理单独 diff 复核，未启动独立 reviewer。独立审查与用户 review 留待验收；本记录不是 acceptance 报告，也不宣称独立代理审查通过。

## 验收场景自检

| 契约 | 证据与实际边界 |
| --- | --- |
| main 发布与旧 SHA 重跑 | workflow main 条件、串行组、发布前 fetch/HEAD 比较的静态测试和 actionlint 通过；未执行真实 Actions 发布 |
| 默认源码构建 / 生产共用镜像 | 实际 Compose 2.29.7 config 使用纯合成环境；双服务同 digest、原卷名、默认 build 均已验证 |
| 上游同步保护与 push 重试 | 本地 bare origin/upstream 和临时 checkout 测试，无真实远端写入 |
| 无新镜像 / CI 尚未完成 | 部署测试不允许 Git/build；只拉取已发布引用；成功记录加实际 image ID/健康状态控制跳过 |
| 升级完整顺序 | 明确断言 backup → refresh config/sync/check → validate → migrate → up → 健康与公网资源验证；run/up 均验证固定 digest 与项目环境 |
| 阶段失败与下次重试 | 每个故障点都要求非零退出、调用停止、旧成功标记不变；清除故障后下一次完成升级 |
| 项目、service、卷与互斥 | 错项目/service、错卷、bind mount、读写属性或 asset-sync 卷不一致均在备份前停止；锁冲突和系统错误分支分别测试 |
| cron 保留错误 | 非零输出即使含 SKIPPED 仍保留状态与全文；正常跳过才静默 |
| 无越界修改/真实部署 | 保护文件指纹一致；未提交、push、SSH、安装 cron 或运行生产升级脚本；测试 Docker/curl 均为临时命令桩 |

## 验证记录

- `go -C cloud test ./...`：通过，deploy 包 146.176s，其余包通过（部分缓存）。
- 同步专项：`go -C cloud test ./deploy -run TestSync -count=1`：通过。
- 升级、cron、维护锁专项：通过。最后简化测试桩的命令匹配后，升级成功/跳过/运行漂移场景重新通过（10.159s）。
- `bash -n` 三个维护脚本、`sh -n refresh-assets.sh`：通过。
- `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/build-relay.yml`：通过。
- 两份 Compose 使用 `--env-file /dev/null` 与合成变量进行 config JSON 校验：通过。没有读取或输出生产 `.env`。
- `git diff --check`：通过；27 个保护文件指纹一致。

## 尚未执行的外部验证

真实多架构镜像构建、GHCR 发布/服务器拉取及生产升级未执行。Linux 真实 flock 竞争测试已写入，在本机 macOS 跳过，后续 Linux Actions 会执行；本机覆盖了注入锁返回码的控制流。生产凭据、现网项目名/卷及首次切换仍须在实际启用时按 README 核验。

工作区门禁工具仍缺失，沿用已授权的 `worktree-override.md` 人工范围核对，没有伪称自动门禁通过。部署脚本失败不自动回滚迁移；若容器已经停止，需要先诊断并恢复运行，才能再次在线备份。

## 后续提交前复核

用户随后要求 push。2026-09-07 已完成原生独立上下文代码审查，无 blocking/important，见 `relay-image-deployment-review.md`。工作区 Cloud 全量测试（deploy 86.459s）与不包含 App 未提交改动的隔离副本全量测试（deploy 87.544s）均通过；Shell、actionlint、差异检查和原 App 指纹复核通过。前述“独立审查未执行”为此前实现阶段的历史状态；本轮只授权提交及镜像发布，不授权服务器部署。
