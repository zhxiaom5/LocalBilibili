#!/usr/bin/env bash
set -euo pipefail

fail=0

report() {
  printf 'open-source audit: %s\n' "$*" >&2
}

reject_tracked_path() {
  local pattern="$1"
  local matches
  matches="$(while IFS= read -r path; do
    [ "$path" = ".env.example" ] && continue
    [ -e "$path" ] && printf '%s\n' "$path"
  done < <(git ls-files --cached --others --exclude-standard) | grep -E "$pattern" || true)"
  if [ -n "$matches" ]; then
    report "tracked private/generated path detected:"
    printf '%s\n' "$matches" >&2
    fail=1
  fi
}

reject_content() {
  local pattern="$1"
  if git grep -nEI "$pattern" -- . ':!go.sum' ':!web/package-lock.json' ':!scripts/verify-open-source-release.sh' >/tmp/localbilibili-open-source-content.$$; then
    report "sensitive content pattern detected:"
    cat /tmp/localbilibili-open-source-content.$$ >&2
    fail=1
  fi
  rm -f /tmp/localbilibili-open-source-content.$$
}

reject_tracked_path '(^|/)(\.env($|\.)|.*\.(db|sqlite|sqlite3|pem|key|p12|pfx)|cookies\.txt|.*\.cookies\.json|logs/|output/|dist/|samples/|media/|downloads/|docs/threads/)'
reject_content '(/Users/|/opt/homebrew/|/private/|gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_|AIza[0-9A-Za-z_-]{20,}|xox[baprs]-|AKIA[0-9A-Z]{16}|BEGIN [A-Z ]*PRIVATE KEY|@(gmail|outlook|icloud|qq)\.(com|cn))'

if [ ! -f LICENSE ]; then
  report 'LICENSE is missing: public publication is blocked until the project owner chooses one.'
else
  report 'LICENSE is present.'
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi

report 'tracked-file and literal-secret checks passed.'
