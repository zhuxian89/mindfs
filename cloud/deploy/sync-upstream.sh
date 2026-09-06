#!/bin/bash
# 只同步源码并推送 fork；镜像发布由 Actions 负责，不在这里部署。
set -euo pipefail

fail() { printf 'SYNC FAILED: %s\n' "$*" >&2; exit 1; }

# 先解析完整函数，避免 merge 更新本脚本时改变正在执行的流程。
main() {
  local deploy_dir repo state_dir operation ahead rc
  deploy_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
  cd "$deploy_dir"
  repo=$(git rev-parse --show-toplevel) || fail "不在 Git 仓库中"
  state_dir="$deploy_dir/.relay-upgrade"
  umask 077
  mkdir -p "$state_dir"
  command -v flock >/dev/null || fail "需要 Linux flock"
  exec 9>"$state_dir/operation.lock"
  flock -n -E 75 9 || {
    rc=$?
    if [ "$rc" -eq 75 ]; then
      printf '%s\n' 'SYNC SKIPPED: 另一个同步或部署任务正在运行'
      exit 0
    fi
    fail "无法取得维护锁"
  }

  cd "$repo"
  [ "$(git branch --show-current)" = main ] || fail "只允许在 main 分支同步"
  for operation in MERGE_HEAD CHERRY_PICK_HEAD REVERT_HEAD rebase-merge rebase-apply sequencer; do
    [ ! -e "$(git rev-parse --git-path "$operation")" ] || fail "存在未完成的 Git 操作: $operation"
  done
  [ -z "$(git status --porcelain --untracked-files=no)" ] || fail "工作区不干净，需人工处理"

  git fetch --quiet origin main || fail "git fetch origin 失败"
  git fetch --quiet upstream main || fail "git fetch upstream 失败"
  git merge --ff-only --no-autostash origin/main || fail "无法快进到 origin/main，需人工处理"
  git merge --no-edit --no-autostash upstream/main || {
    if git rev-parse --verify --quiet MERGE_HEAD >/dev/null; then
      git merge --abort || fail "无法中止本次冲突合并，需人工处理"
    fi
    fail "合并 upstream 失败，未推送，需人工处理"
  }

  # 不按 upstream 提交数跳过：上一次 push 失败时仍需重试。
  ahead=$(git rev-list --count origin/main..HEAD)
  if [ "$ahead" -eq 0 ]; then
    printf '%s\n' 'SYNC SKIPPED: 没有待推送的提交'
    return
  fi
  git push --quiet origin HEAD:main || fail "git push origin 失败；下次执行将重试"
  printf 'SYNC OK: 已推送 %s 个提交，镜像发布由 Actions 独立执行\n' "$ahead"
}

main "$@"
