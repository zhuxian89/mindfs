---
doc_type: audit-finding
audit: 2026-08-04-node-upgrade-recovery
finding_id: "arch-drift-03"
nature: arch-drift
severity: P2
confidence: high
suggested_action: cs-issue
status: resolved
---

# Finding 03：readiness 只检查目录存在，损坏的资源闭环仍显示 ready

## 速答

Cloud 的 `/readyz` 只确认 `index.html` 和 `assets/` 目录存在，不验证 index 或支持版本 manifest 引用的 JS/CSS 是否真的可读取。

## 关键证据

- `cloud/app/operations_handlers.go:19-20` — readiness 只调用 `config.CheckAssetsDir`。
- `cloud/internal/config/config.go:98-113` — `CheckAssetsDir` 只 stat `index.html` 和 `assets` 目录，没有解析或验证任何 asset 引用。
- `.codestable/features/2026-08-03-relay-deployment-baseline/relay-deployment-baseline-design.md:36-38,66` — 设计把 readiness 描述为 Asset Bundle “完整”时才为 200，实现弱于设计。
- 线上 `/readyz` 返回 200 `ready`，同时节点所需的 `index-DeNebQ9q.js` 返回 404。

## 影响

部署系统会把实际无法打开节点页面的 Cloud 判定为健康，无法在切流量或发布后及时阻断不兼容版本。

## 修复方向

为当前 bundle 和受支持历史 bundle 生成 asset manifest，并让 validate/readiness 校验 manifest 中的全部文件存在且为普通文件。

## 建议动作

`cs-issue`，因为 readiness 的可观察语义与已批准部署设计不一致。

## 修复结果

`config.CheckAssetsDir` 现解析 `index.html` 的本地 `src`/`href`，逐一确认引用的 `/assets/` 或 `/mindfs-assets/` 文件存在且为普通文件；单元测试覆盖缺失 CSS 后 readiness 失败、补齐后通过。
