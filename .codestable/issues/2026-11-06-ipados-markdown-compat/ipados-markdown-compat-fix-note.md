---
doc_type: issue-fix
issue: ipados-markdown-compat
path: fast-track
fix_date: 2026-11-06
tags: [ipad, safari, markdown, compatibility]
---

# iPadOS 16.1 Markdown 白屏

- 根因：`mdast-util-gfm-autolink-literal@2.0.1` 的邮箱识别使用正则后向断言，Safari 16.4 之前不支持。截图所示线上 `index-DPXv0Cwl.js` 与官方 v0.5.9 文件 SHA-256 一致。
- 最小修复：`web/package.json` 添加 npm override，固定该间接依赖为 `2.0.0`；`web/package-lock.json` 仅更新对应版本、下载地址和完整性校验。前端业务源码与 Relay 后端无改动。
- 验证：`npm ci`、`npm run build`、`npm ls mdast-util-gfm-autolink-literal`、`git diff --check` 通过。Playwright 在实际 MarkdownViewer 中模拟拒绝后向断言：2.0.1 复现同样异常及空白，2.0.0 正常渲染邮箱链接、网址、表格、任务列表、删除线和行内代码。该验证不等于 iPad 真机验收。
- 构建产物：`web/dist/`，主入口 `index-2t7RFipd.js`；浏览器对照材料在忽略目录 `build/ipad-markdown-audit/`。
- 部署边界：本记录不代表已部署或完成真机验收。节点页面的 HTML 引用其自身前端哈希，仅更新 Relay 镜像不会替换旧节点引用的官方脚本；需部署修复后的节点前端，并将对应新资源同步到云端，保留官方历史哈希文件，再在 iPadOS 16.1 真机复测。
- 后续同步上游：仅保留这一条 override 及对应锁项；上游解决旧 Safari 兼容后移除，不扩展为业务源码分叉。
