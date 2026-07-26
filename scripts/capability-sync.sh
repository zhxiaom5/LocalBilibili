#!/bin/sh
set -eu

project=LocalBilibili
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
hub_dir="${LOCAL_CAPABILITY_HUB:-$repo_dir/../LocalContentCapabilityHub}"
if [ "${1:-}" = "--hub" ]; then hub_dir=$2; shift 2; fi
[ "$#" -eq 0 ] || { echo 'usage: capability-sync.sh [--hub HUB]' >&2; exit 2; }
decisions="$hub_dir/decisions.tsv"; catalog="$hub_dir/capabilities.tsv"; queue="$repo_dir/docs/功能任务队列.tsv"
progress="$repo_dir/docs/开发进度.md"
[ -f "$decisions" ] && [ -f "$catalog" ] || { echo 'capability hub data is missing' >&2; exit 1; }
tmp=$(mktemp "$repo_dir/docs/.capability-queue.XXXXXX"); trap 'rm -f "$tmp"' EXIT
awk -F '\t' -v OFS='\t' -v project="$project" -v now="$(date '+%Y-%m-%d %H:%M:%S %z')" '
  FILENAME==ARGV[1] { if (FNR>1) { local[$1]=$6; goal[$1]=$7; updated[$1]=$8; evidence[$1]=$9 } next }
  FILENAME==ARGV[2] { if ($0 !~ /^#/ && NF>=8) title[$1]=$2; next }
  $0 !~ /^#/ && $2==project {
    state=local[$1]; if (state=="") { state=($3=="approved" ? "ready" : ($3=="in_progress" ? "in_progress" : ($3=="done" ? "done" : "pending_approval"))) }
    if ($3=="done") state="done"
    stamp=(updated[$1]=="" ? now : updated[$1]); print $1,title[$1],$3,$4,$5,state,goal[$1],stamp,evidence[$1]
  }
' "$queue" "$catalog" "$decisions" 2>/dev/null > "$tmp" || true
{ printf '%s\n' 'capability_id	capability	central_status	priority	central_note	local_status	goal_ref	updated_at	evidence'; cat "$tmp"; } > "$queue"
trap - EXIT; rm -f "$tmp"
[ -f "$progress" ] || printf '# LocalBilibili 开发进度\n\n本文件由本项目维护；中央 Hub 只派发与汇总，不在此替代开发。\n\n## 进度记录\n' > "$progress"
echo "$project queue synchronized: $queue"
