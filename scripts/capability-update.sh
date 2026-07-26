#!/bin/sh
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd); queue="$repo_dir/docs/功能任务队列.tsv"; progress="$repo_dir/docs/开发进度.md"
[ "$#" -eq 4 ] || { echo 'usage: capability-update.sh CAP-XXX local_status goal_ref evidence' >&2; exit 2; }
id=$1; status=$2; goal=$3; evidence=$4
case "$status" in pending_approval|ready|in_progress|blocked|done) ;; *) echo "invalid local status: $status" >&2; exit 2;; esac
[ -f "$queue" ] || { echo 'run capability-sync.sh first' >&2; exit 1; }
awk -F '\t' -v id="$id" 'NR>1 && $1==id { found=1 } END { exit found?0:1 }' "$queue" || { echo "unknown capability: $id" >&2; exit 1; }
tmp=$(mktemp "$repo_dir/docs/.capability-update.XXXXXX"); trap 'rm -f "$tmp"' EXIT
awk -F '\t' -v OFS='\t' -v id="$id" -v status="$status" -v goal="$goal" -v evidence="$evidence" -v now="$(date '+%Y-%m-%d %H:%M:%S %z')" 'NR==1 {print; next} $1==id {$6=status;$7=goal;$8=now;$9=evidence} {print}' "$queue" > "$tmp"; mv "$tmp" "$queue"; trap - EXIT
if [ ! -f "$progress" ]; then printf '# LocalBilibili 开发进度\n\n本文件由本项目维护；中央 Hub 仅汇总，不在此替代开发。\n\n## 进度记录\n' > "$progress"; fi
printf '\n- %s｜%s｜项目状态：%s｜Goal：%s｜证据：%s\n' "$(date '+%Y-%m-%d %H:%M:%S %z')" "$id" "$status" "$goal" "$evidence" >> "$progress"
