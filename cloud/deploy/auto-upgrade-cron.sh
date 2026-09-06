#!/bin/bash
# cron wrapper: SKIPPED 时静默，其余(OK/FAILED)输出并投递
out=$(/root/ai-projects/mindfs/cloud/deploy/auto-upgrade.sh 2>&1)
rc=$?
case "$out" in *"UPGRADE SKIPPED"*) exit 0;; esac
echo "$out"
exit $rc
