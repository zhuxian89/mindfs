#!/bin/bash
# cron wrapper: 仅成功的 SKIPPED 静默；失败保留全部输出和退出码。
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 1
out=$("$script_dir/auto-upgrade.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [[ "$out" == 'UPGRADE SKIPPED:'* ]]; then
  exit 0
fi
printf '%s\n' "$out"
exit "$rc"
