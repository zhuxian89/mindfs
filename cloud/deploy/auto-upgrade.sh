#!/bin/bash
# MindFS Relay auto-upgrade: merge upstream -> build -> deploy -> verify
# 任何一步失败立即退出(exit 1)，绝不重试删 volume。成功输出 UPGRADE OK 摘要。
set -euo pipefail

REPO=/root/ai-projects/mindfs/cloud
COMPOSE="docker compose -f docker-compose.1panel.yml"
cd "$REPO/deploy"

fail() { echo "UPGRADE FAILED: $*" >&2; exit 1; }

# 0. 检查上游
git fetch upstream --quiet || fail "git fetch upstream 失败"
COUNT=$(git rev-list --count HEAD..upstream/main)
if [ "$COUNT" -eq 0 ]; then
  echo "UPGRADE SKIPPED: upstream 无新提交"
  exit 0
fi
echo "发现上游 $COUNT 个新提交，开始升级"

# 1. 合并 upstream（fork 有自维护提交，非 ff）
git config user.name "zhuxian89"
git config user.email "421690794@qq.com"
git merge upstream/main --no-edit --quiet || {
  N=$(git diff --name-only --diff-filter=U | wc -l)
  git merge --abort 2>/dev/null || true
  fail "merge upstream 有 $N 个冲突文件，已 abort，需人工处理"
}
N=$(git diff --name-only --diff-filter=U | wc -l)
[ "$N" -eq 0 ] || { git merge --abort 2>/dev/null || true; fail "merge 后仍有 $N 个冲突"; }

# 2. 推送 fork
git push origin main --quiet || fail "git push origin 失败"

# 3. 备份数据库
BK="/backups/mindfs-cloud-pre-upgrade-$(date +%Y%m%d-%H%M%S).db"
$COMPOSE exec -T relay mindfs-relay backup "$BK" || fail "数据库备份失败: $BK"

# 4. 工作区检查 + compose 配置
test -z "$(git status --porcelain --untracked-files=no)" || fail "工作区不干净"
$COMPOSE config --quiet || fail "compose 配置无效"

# 5. 内存检查（本机 OOM 铁律）
AVAIL=$(free -g | awk '/^Mem:/{print $7}')
[ "$AVAIL" -ge 3 ] || fail "可用内存 ${AVAIL}G < 3G，取消构建"

# 6. 构建（前台，脚本整体由调用方后台执行）
$COMPOSE build --pull --no-cache relay >/dev/null 2>&1 || fail "镜像构建失败"

# 7. asset-sync（绝对不能跳过）
$COMPOSE run --rm asset-sync >/dev/null 2>&1 || fail "asset-sync 失败"

# 8. validate
$COMPOSE run --rm relay validate >/dev/null 2>&1 || fail "validate 失败"

# 9. migrate
$COMPOSE run --rm relay migrate >/dev/null 2>&1 || fail "migrate 失败"

# 10. 重启
$COMPOSE up -d --force-recreate relay >/dev/null 2>&1 || fail "relay 重启失败"

# 11. 健康检查
sleep 8
curl -sf http://127.0.0.1:13005/healthz | grep -q '"ok"' || fail "healthz 异常"
curl -sf http://127.0.0.1:13005/readyz | grep -q '"ready"' || fail "readyz 异常"

# 12. 新资产验证
$COMPOSE cp relay:/var/lib/mindfs-assets/index.html /tmp/mindfs-auto-upgrade-index.html >/dev/null 2>&1 || fail "无法读取新 index.html"
ASSETS=$(grep -oE 'assets/index-[^"]+\.(js|css)' /tmp/mindfs-auto-upgrade-index.html | sort -u)
[ -n "$ASSETS" ] || fail "index.html 中未找到资产引用"
while read -r f; do
  CODE=$(curl -s -o /dev/null -w '%{http_code}' "https://relay.20260310.best/mindfs-assets/${f#assets/}")
  [ "$CODE" = "200" ] || fail "新资产 $f 返回 $CODE"
done <<< "$ASSETS"

echo "UPGRADE OK: 合并上游 $COUNT 提交, 备份 $BK, 新资产: $(echo $ASSETS | tr '\n' ' ')"
