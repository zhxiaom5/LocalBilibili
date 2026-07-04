#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NODE_SCRIPT="$ROOT_DIR/scripts/fnos-browser-install.mjs"
FPK_PATH=""
BUILD=0
ARGS=()

usage() {
  cat <<'EOF'
Usage:
  scripts/deploy-fnos-playwright.sh --login [--headed]
  scripts/deploy-fnos-playwright.sh --install [--build] [--dry-run]

Options:
  --login             Open fnOS in a visible browser and save Playwright storageState.
  --install           Upload and install/upgrade the FPK. Headless by default.
  --build             Run scripts/build-fnos-fpk.sh before installing.
  --dry-run           Check FPK, login state, and fnOS page reachability without installing.
  --self-test-selectors
                      Run a local mock fnOS UI flow to verify selector behavior.
  --headed            Show the browser while installing/debugging.
  --fpk PATH          FPK path. Defaults to the highest-versioned LocalBilibili FPK under dist/.
  --fnos-url URL      fnOS app center URL. Defaults to FNOS_WEB_URL or http://192.168.50.247:5666.
  --verify-url URL    LocalBilibili URL to check after install.
  --state PATH        storageState path. Defaults to FNOS_STORAGE_STATE or .local/fnos/storage-state.json.
  -h, --help          Show this help.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --build)
      BUILD=1
      shift
      ;;
    --fpk)
      FPK_PATH="$2"
      ARGS+=("--fpk" "$2")
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    --login|--install|--dry-run|--self-test-selectors|--headed|--fnos-url|--verify-url|--state)
      if [[ "$1" == "--fnos-url" || "$1" == "--verify-url" || "$1" == "--state" ]]; then
        ARGS+=("$1" "$2")
        shift 2
      else
        ARGS+=("$1")
        shift
      fi
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

select_latest_fpk() {
  python3 - "$ROOT_DIR/dist" <<'PY'
import re
import sys
from pathlib import Path

dist = Path(sys.argv[1])
pattern = re.compile(r"^localbilibili-v(\d+(?:\.\d+)*)(-root)?-x86_64\.fpk$")
candidates = []
if dist.exists():
    for path in dist.iterdir():
        match = pattern.match(path.name)
        if not match or not path.is_file():
            continue
        version = tuple(int(part) for part in match.group(1).split("."))
        # Prefer the normal package over a root-suffixed package if versions tie.
        root_rank = 0 if match.group(2) is None else -1
        candidates.append((version, root_rank, str(path)))
if not candidates:
    raise SystemExit(1)
print(max(candidates)[2])
PY
}

if ! command -v node >/dev/null 2>&1; then
  echo "node is required for Playwright deployment" >&2
  exit 1
fi

if [ ! -d "$ROOT_DIR/web/node_modules/@playwright/test" ]; then
  echo "Playwright dependencies are missing. Run: npm install --prefix web" >&2
  exit 1
fi

if [ "$BUILD" -eq 1 ]; then
  "$ROOT_DIR/scripts/build-fnos-fpk.sh"
fi

if [ -z "$FPK_PATH" ] && printf '%s\n' "${ARGS[@]}" | grep -Eqx -- '--install|--self-test-selectors'; then
  if ! FPK_PATH="$(select_latest_fpk)"; then
    echo "No versioned LocalBilibili FPK found under $ROOT_DIR/dist." >&2
    echo "Run with --build, run scripts/build-fnos-fpk.sh first, or pass --fpk PATH." >&2
    exit 1
  fi
  ARGS+=("--fpk" "$FPK_PATH")
fi

if printf '%s\n' "${ARGS[@]}" | grep -qx -- '--install'; then
  if [ ! -f "$FPK_PATH" ]; then
    echo "FPK not found: $FPK_PATH" >&2
    echo "Run with --build or run scripts/build-fnos-fpk.sh first." >&2
    exit 1
  fi
fi

exec node "$NODE_SCRIPT" "${ARGS[@]}"
