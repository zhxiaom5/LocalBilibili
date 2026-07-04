#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PKG_DIR="$ROOT_DIR/packaging/fnos/localbilibili"
APP_BIN="$PKG_DIR/app/localbilibili"
SUBTITLE_CLI_BIN="$PKG_DIR/app/subtitle-cli"
SUBTITLE_RUNTIME_DIR="$PKG_DIR/app/subtitle-runtime"
YTDLP_BIN="$PKG_DIR/app/yt-dlp"
DIST_DIR="$ROOT_DIR/dist"
TOOLS_DIR="$ROOT_DIR/.tools/fnpack"
YTDLP_TOOLS_DIR="$ROOT_DIR/.tools/yt-dlp"
OUT_FPK="$DIST_DIR/localbilibili-x86_64.fpk"
VERSION="$(awk -F= '$1 == "version" { gsub(/[[:space:]]/, "", $2); print $2 }' "$PKG_DIR/manifest")"
VERSIONED_FPK="$DIST_DIR/localbilibili-v${VERSION}-x86_64.fpk"

log() {
  printf '[fnos-fpk] %s\n' "$*" >&2
}

fnpack_url() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$os:$arch" in
    Darwin:arm64) echo "https://static2.fnnas.com/fnpack/fnpack-1.2.1-darwin-arm64" ;;
    Darwin:x86_64) echo "https://static2.fnnas.com/fnpack/fnpack-1.2.1-darwin-amd64" ;;
    Linux:x86_64) echo "https://static2.fnnas.com/fnpack/fnpack-1.2.1-linux-amd64" ;;
    Linux:aarch64|Linux:arm64) echo "https://static2.fnnas.com/fnpack/fnpack-1.2.1-linux-arm64" ;;
    *) echo "" ;;
  esac
}

resolve_fnpack() {
  if command -v fnpack >/dev/null 2>&1; then
    command -v fnpack
    return
  fi

  local url tool
  url="$(fnpack_url)"
  if [ -z "$url" ]; then
    echo "fnpack is not installed and no download URL is known for $(uname -s)/$(uname -m)" >&2
    exit 1
  fi

  mkdir -p "$TOOLS_DIR"
  tool="$TOOLS_DIR/$(basename "$url")"
  if [ ! -x "$tool" ]; then
    log "downloading fnpack: $url"
    curl -fL "$url" -o "$tool"
    chmod +x "$tool"
  fi
  echo "$tool"
}

require_file() {
  if [ ! -e "$1" ]; then
    echo "required packaging file missing: $1" >&2
    exit 1
  fi
}

prepare_subtitle_runtime() {
  local source_dir="${LOCALBILIBILI_SUBTITLE_RUNTIME_DIR:-$ROOT_DIR/third_party/subtitle-runtime/linux-amd64}"
  rm -rf "$SUBTITLE_RUNTIME_DIR"
  mkdir -p "$SUBTITLE_RUNTIME_DIR/models"

  if [ ! -d "$source_dir" ]; then
    echo "subtitle runtime missing: $source_dir" >&2
    echo "Provide LOCALBILIBILI_SUBTITLE_RUNTIME_DIR with Linux x86_64 whisper-cli, ffmpeg, and models/ggml-small.bin." >&2
    exit 1
  fi

  for file in whisper-cli ffmpeg; do
    if [ ! -x "$source_dir/$file" ]; then
      echo "subtitle runtime executable missing or not executable: $source_dir/$file" >&2
      exit 1
    fi
    cp "$source_dir/$file" "$SUBTITLE_RUNTIME_DIR/$file"
    chmod +x "$SUBTITLE_RUNTIME_DIR/$file"
  done

  find "$source_dir" -maxdepth 1 \( -type f -o -type l \) \( -name 'lib*.so' -o -name 'lib*.so.*' \) -print0 |
    while IFS= read -r -d '' lib; do
      cp -L "$lib" "$SUBTITLE_RUNTIME_DIR/$(basename "$lib")"
      chmod +x "$SUBTITLE_RUNTIME_DIR/$(basename "$lib")"
    done

  if [ -f "$source_dir/LICENSE" ]; then
    cp "$source_dir/LICENSE" "$SUBTITLE_RUNTIME_DIR/WHISPER_LICENSE"
  fi

  if [ ! -s "$source_dir/models/ggml-small.bin" ]; then
    echo "subtitle runtime model missing: $source_dir/models/ggml-small.bin" >&2
    exit 1
  fi
  cp "$source_dir/models/ggml-small.bin" "$SUBTITLE_RUNTIME_DIR/models/ggml-small.bin"
}

prepare_ytdlp_runtime() {
  local source_bin="${LOCALBILIBILI_YTDLP_LINUX_BIN:-}"
  local cached_bin="$YTDLP_TOOLS_DIR/yt-dlp_linux"
  rm -f "$YTDLP_BIN"

  if [ -z "$source_bin" ]; then
    mkdir -p "$YTDLP_TOOLS_DIR"
    source_bin="$cached_bin"
    if [ ! -x "$source_bin" ]; then
      log "downloading yt-dlp linux x86_64 runtime"
      curl -fL "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux" -o "$source_bin"
      chmod +x "$source_bin"
    fi
  fi

  if [ ! -x "$source_bin" ]; then
    echo "yt-dlp linux runtime missing or not executable: $source_bin" >&2
    echo "Provide LOCALBILIBILI_YTDLP_LINUX_BIN with a Linux x86_64 yt-dlp executable." >&2
    exit 1
  fi
  cp "$source_bin" "$YTDLP_BIN"
  chmod +x "$YTDLP_BIN"
}

log "building embedded web assets"
npm run build --prefix "$ROOT_DIR/web"

log "cross-compiling linux/amd64 binary"
rm -f "$APP_BIN"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$APP_BIN" "$ROOT_DIR/cmd/server"
chmod +x "$APP_BIN"

log "cross-compiling linux/amd64 subtitle-cli"
rm -f "$SUBTITLE_CLI_BIN"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$SUBTITLE_CLI_BIN" "$ROOT_DIR/tools/subtitle-cli"
chmod +x "$SUBTITLE_CLI_BIN"

log "preparing subtitle runtime"
prepare_subtitle_runtime

log "preparing yt-dlp runtime"
prepare_ytdlp_runtime

if command -v file >/dev/null 2>&1; then
  file "$APP_BIN"
  file "$APP_BIN" | grep -Eq 'ELF 64-bit.*x86-64|ELF 64-bit.*x86_64' || {
    echo "compiled binary is not linux x86_64" >&2
    exit 1
  }
  file "$SUBTITLE_CLI_BIN"
  file "$SUBTITLE_CLI_BIN" | grep -Eq 'ELF 64-bit.*x86-64|ELF 64-bit.*x86_64' || {
    echo "compiled subtitle-cli is not linux x86_64" >&2
    exit 1
  }
  file "$YTDLP_BIN"
  file "$YTDLP_BIN" | grep -Eq 'ELF 64-bit.*x86-64|ELF 64-bit.*x86_64' || {
    echo "bundled yt-dlp is not linux x86_64" >&2
    exit 1
  }
fi

log "checking fpk template"
require_file "$PKG_DIR/manifest"
require_file "$PKG_DIR/CHANGELOG.md"
require_file "$PKG_DIR/app/CHANGELOG.md"
require_file "$PKG_DIR/config/privilege"
require_file "$PKG_DIR/config/resource"
require_file "$PKG_DIR/cmd/main"
require_file "$PKG_DIR/wizard/install"
require_file "$PKG_DIR/app/ui/config"
require_file "$SUBTITLE_CLI_BIN"
require_file "$YTDLP_BIN"
require_file "$SUBTITLE_RUNTIME_DIR/whisper-cli"
require_file "$SUBTITLE_RUNTIME_DIR/ffmpeg"
require_file "$SUBTITLE_RUNTIME_DIR/models/ggml-small.bin"
require_file "$PKG_DIR/ICON.PNG"
require_file "$PKG_DIR/ICON_256.PNG"
require_file "$PKG_DIR/app/ui/images/icon-source.svg"
require_file "$PKG_DIR/app/ui/images/icon-source.png"
require_file "$PKG_DIR/app/ui/images/icon-64.png"
require_file "$PKG_DIR/app/ui/images/icon-256.png"
chmod +x "$PKG_DIR"/cmd/*

log "checking app package does not contain non-portable runtime files"
if [ -e "$PKG_DIR/app/bilibili-downloader" ]; then
  echo "Go native downloader is built into localbilibili; remove app/bilibili-downloader from FPK template" >&2
  exit 1
fi
if find "$PKG_DIR" -type f -name '*localdouyin*' -print -quit | grep -q .; then
  echo "packaging contains LocalDouyin residue; refusing to build fnOS package" >&2
  find "$PKG_DIR" -type f -name '*localdouyin*' -print >&2
  exit 1
fi
if find "$PKG_DIR/app" -type l -print -quit | grep -q .; then
  echo "packaging app contains symlinks; refusing to build fnOS package" >&2
  find "$PKG_DIR/app" -type l -print >&2
  exit 1
fi
for exe in "$SUBTITLE_RUNTIME_DIR/whisper-cli" "$SUBTITLE_RUNTIME_DIR/ffmpeg" "$YTDLP_BIN"; do
  if [ ! -x "$exe" ]; then
    echo "packaged runtime executable is not executable: $exe" >&2
    exit 1
  fi
done
if find "$PKG_DIR/app" \( -type d -name __pycache__ -o -type d -name venv -o -type d -name .venv -o -type f -name '*.pyc' -o -type f -name '*.pyo' -o -type f -name '.cookies.json' -o -type f -name 'dy_downloader.db' \) -print -quit | grep -q .; then
  echo "packaging app contains non-portable runtime files" >&2
  find "$PKG_DIR/app" \( -type d -name __pycache__ -o -type d -name venv -o -type d -name .venv -o -type f -name '*.pyc' -o -type f -name '*.pyo' -o -type f -name '.cookies.json' -o -type f -name 'dy_downloader.db' \) -print >&2
  exit 1
fi

mkdir -p "$DIST_DIR"
rm -f "$OUT_FPK" "$VERSIONED_FPK" "$ROOT_DIR/localbilibili.fpk" "$PKG_DIR"/*.fpk

FNPACK="$(resolve_fnpack)"
log "packing fpk with $FNPACK"
(
  cd "$ROOT_DIR"
  "$FNPACK" build --directory "$PKG_DIR"
)

GENERATED=""
if [ -f "$ROOT_DIR/localbilibili.fpk" ]; then
  GENERATED="$ROOT_DIR/localbilibili.fpk"
elif [ -f "$PKG_DIR/localbilibili.fpk" ]; then
  GENERATED="$PKG_DIR/localbilibili.fpk"
fi
if [ -z "$GENERATED" ]; then
  echo "fnpack completed but no .fpk file was found" >&2
  exit 1
fi
mv "$GENERATED" "$VERSIONED_FPK"
cp "$VERSIONED_FPK" "$OUT_FPK"
log "created $VERSIONED_FPK"
log "updated latest alias $OUT_FPK"
