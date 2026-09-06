#!/bin/bash
# 独立部署 GHCR 已发布镜像；不合并、推送源码或在服务器构建。
set -euo pipefail

fail() { printf 'UPGRADE FAILED: %s\n' "$*" >&2; exit 1; }

# 只输出部署所需的非敏感字段；完整 Compose JSON 留在私有临时目录。
read_config() {
  docker compose config --format json >"$work_dir/config.json" || fail "Compose 配置无效"
  python3 - "$work_dir/config.json" >"$work_dir/settings" <<'PY' || fail "需要使用双服务共用镜像的 1Panel 配置"
import json, sys
from urllib.parse import urlsplit
c = json.load(open(sys.argv[1]))
r, a = c['services']['relay'], c['services']['asset-sync']
if 'build' in r or 'build' in a or not r.get('image') or r['image'] != a.get('image'):
    sys.exit(1)
u = r['environment']['MINDFS_CLOUD_PUBLIC_URL']
p = urlsplit(u)
if p.scheme not in ('http', 'https') or not p.hostname or p.username or p.password or p.query or p.fragment:
    sys.exit(1)
for value in (c['name'], r['container_name'], r['image'], u.rstrip('/')):
    if not value or any(ch.isspace() for ch in value):
        sys.exit(1)
    print(value)
PY
  { read -r project; read -r container; read -r configured_image; read -r public_url; } <"$work_dir/settings"
}

verify_identity() {
  docker inspect "$container" >"$work_dir/container.json" || fail "找不到现有 Relay；本脚本不用于初装"
  python3 - "$work_dir/config.json" "$work_dir/container.json" <<'PY' || fail "现网 Compose 项目、service 或数据卷与配置不符，已停止"
import json, sys
c = json.load(open(sys.argv[1]))
items = json.load(open(sys.argv[2]))
if len(items) != 1:
    sys.exit(1)
actual = items[0]
labels = actual['Config'].get('Labels') or {}
if labels.get('com.docker.compose.project') != c['name'] or labels.get('com.docker.compose.service') != 'relay':
    sys.exit(1)
if actual['State']['Status'] != 'running':
    sys.exit(1)
mounts = {m['Destination']: m for m in actual['Mounts']}
relay = {m['target']: m for m in c['services']['relay']['volumes']}
assets = {m['target']: m for m in c['services']['asset-sync']['volumes']}
for target, writable in (('/var/lib/mindfs-cloud', True), ('/var/lib/mindfs-assets', False), ('/backups', True)):
    expected, mounted = relay.get(target, {}), mounts.get(target, {})
    name = c['volumes'].get(expected.get('source'), {}).get('name')
    if not name or expected.get('type') != 'volume' or bool(expected.get('read_only', False)) == writable:
        sys.exit(1)
    if mounted.get('Type') != 'volume' or mounted.get('Name') != name or mounted.get('RW') != writable:
        sys.exit(1)
sync = assets.get('/var/lib/mindfs-assets', {})
if sync.get('type') != 'volume' or sync.get('source') != relay['/var/lib/mindfs-assets']['source'] or sync.get('read_only', False):
    sys.exit(1)
PY
}

running_target() {
  local actual
  actual=$(docker inspect --format '{{.Image}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' "$container") || return 1
  [ "$actual" = "$target_id running healthy" ]
}

verify_http() {
  local endpoint expected code asset
  for endpoint in healthz readyz; do
    expected=ok
    [ "$endpoint" != readyz ] || expected=ready
    curl --fail --silent --show-error --connect-timeout 10 --max-time 30 \
      "$public_url/$endpoint" >"$work_dir/response.json" || fail "$endpoint 请求失败"
    python3 - "$work_dir/response.json" "$expected" <<'PY' || fail "$endpoint 响应异常"
import json, sys
if json.load(open(sys.argv[1])).get('status') != sys.argv[2]:
    sys.exit(1)
PY
  done
  docker compose cp relay:/var/lib/mindfs-assets/index.html "$work_dir/index.html" || fail "无法读取新 index.html"
  python3 - "$work_dir/index.html" >"$work_dir/assets" <<'PY' || fail "index.html 中未找到有效资产引用"
import re, sys
from html.parser import HTMLParser
class Assets(HTMLParser):
    files = set()
    def handle_starttag(self, tag, attrs):
        for key, value in attrs:
            if key in ('src', 'href') and value and re.fullmatch(r'(?:\./|/)?assets/[A-Za-z0-9_./-]+\.(?:js|css)', value):
                path = value.split('assets/', 1)[1]
                if '..' not in path.split('/'):
                    self.files.add(path)
p = Assets()
p.feed(open(sys.argv[1]).read())
if not p.files:
    sys.exit(1)
print('\n'.join(sorted(p.files)))
PY
  while IFS= read -r asset; do
    code=$(curl --silent --show-error --connect-timeout 10 --max-time 30 --output /dev/null \
      --write-out '%{http_code}' "$public_url/mindfs-assets/$asset") || fail "资产 $asset 请求失败"
    [ "$code" = 200 ] || fail "资产 $asset 返回 $code"
  done <"$work_dir/assets"
}

main() {
  local deploy_dir state_dir work_dir rc project container configured_image public_url
  local requested_image target_id pinned_id backup previous
  deploy_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
  cd "$deploy_dir"
  umask 077
  state_dir="$deploy_dir/.relay-upgrade"
  mkdir -p "$state_dir"
  command -v flock >/dev/null || fail "需要 Linux flock"
  exec 9>"$state_dir/operation.lock"
  flock -n -E 75 9 || {
    rc=$?
    if [ "$rc" -eq 75 ]; then
      printf '%s\n' 'UPGRADE SKIPPED: 另一个同步或部署任务正在运行'
      exit 0
    fi
    fail "无法取得维护锁"
  }
  command -v python3 >/dev/null || fail "需要 Python 3 解析 Compose 和 Docker JSON"
  work_dir=$(mktemp -d "$state_dir/check.XXXXXX")
  # 将路径固定在 trap 中，函数返回后仍可清理；路径来自 mktemp。
  trap "rm -rf -- $(printf '%q' "$work_dir")" EXIT
  export COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.1panel.yml}"
  read_config
  export COMPOSE_PROJECT_NAME="$project"
  verify_identity

  requested_image="$configured_image"
  docker pull --quiet "$requested_image" || fail "镜像拉取失败；请检查镜像是否已发布及 GHCR 拉取权限"
  docker image inspect "$requested_image" >"$work_dir/image.json" || fail "无法检查目标镜像"
  python3 - "$work_dir/image.json" "$requested_image" >"$work_dir/target" <<'PY' || fail "无法确定本次镜像的不可变 digest"
import json, re, sys
image = json.load(open(sys.argv[1]))[0]
ref = sys.argv[2]
repo = ref.split('@', 1)[0]
if ':' in repo.rsplit('/', 1)[-1]:
    repo = repo.rsplit(':', 1)[0]
candidates = [d for d in image.get('RepoDigests', []) if re.fullmatch(re.escape(repo) + r'@sha256:[a-f0-9]{64}', d)]
if '@' in ref:
    candidates = [d for d in candidates if d == ref]
if len(candidates) != 1:
    sys.exit(1)
print(candidates[0])
print(image['Id'])
PY
  { read -r RELAY_IMAGE; read -r target_id; } <"$work_dir/target"
  export RELAY_IMAGE
  pinned_id=$(docker image inspect --format '{{.Id}}' "$RELAY_IMAGE") || fail "本地缺少固定 digest 镜像"
  [ "$pinned_id" = "$target_id" ] || fail "固定 digest 与已拉取镜像不一致"
  read_config
  [ "$configured_image" = "$RELAY_IMAGE" ] || fail "Compose 未采用 RELAY_IMAGE，无法锁定版本"
  verify_identity

  previous=''
  if [ -f "$state_dir/successful-image" ]; then
    previous=$(cat "$state_dir/successful-image")
  fi
  if [ "$previous" = "$RELAY_IMAGE" ] && running_target; then
    printf 'UPGRADE SKIPPED: 已成功部署且运行健康 %s\n' "$RELAY_IMAGE"
    return
  fi

  backup="/backups/mindfs-cloud-pre-upgrade-$(date +%Y%m%d-%H%M%S)-$$.db"
  docker compose exec -T relay mindfs-relay backup "$backup" || fail "数据库备份失败"
  # refresh-assets 同时执行 sync 与覆盖检查，并继承相同的项目和 digest。
  "$deploy_dir/refresh-assets.sh" || fail "资源同步或覆盖检查失败"
  docker compose run --rm --no-deps relay validate || fail "validate 失败"
  docker compose run --rm --no-deps relay migrate || fail "migrate 失败"
  docker compose up -d --force-recreate --no-deps --no-build --pull never --wait --wait-timeout 120 relay || fail "Relay 重建或健康检查失败"
  verify_identity
  running_target || fail "Relay 未以目标镜像健康运行"
  verify_http

  # 所有验证完成后才原子记录成功；失败时保留旧记录供下次重试。
  printf '%s\n' "$RELAY_IMAGE" >"$work_dir/successful-image"
  mv -f -- "$work_dir/successful-image" "$state_dir/successful-image"
  printf 'UPGRADE OK: 镜像 %s，备份 %s\n' "$RELAY_IMAGE" "$backup"
}

main "$@"
