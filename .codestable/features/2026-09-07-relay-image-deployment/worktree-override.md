# 工作区门禁人工豁免

- reason：当前项目未安装 `.codestable/tools/codestable-worktree-gate.py`；2026-09-07 start 命令以退出码 2 报文件不存在，不能声称自动门禁通过。
- scope：仅本次已批准的 Relay 镜像发布与独立部署功能；在当前工作区人工核对范围，不安装或修改 CodeStable 框架，不提交、不 push、不执行生产脚本。保护既有 `cloud/app/` 与暂停 feature 的所有未提交改动。
- approval：用户在工具询问“是否记录一次人工门禁豁免，按已批准的方案继续？”后选择“人工核对后继续（推荐）”。
- manual-check：变更前后核对 git status、功能 diff 与 app 改动指纹；只编辑本功能文件。收尾门禁同样无法自动执行时明确记录，不伪称通过。
- 2026-09-07 收尾授权更新：用户随后明确要求“push把”，授权提交并推送本次部署整改；此前“不提交、不 push”仅对应实现阶段。本轮先在 deploy/relay-image-pipeline 分支形成独立提交，再以非 force 方式推送 origin/main。不纳入 App、暂停 feature 或选中的 UI 设计文件，不执行服务器部署。
