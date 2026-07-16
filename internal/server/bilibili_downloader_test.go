package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBilibiliDownloaderReadyUsesYTDLPOverride(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 2026.01.01; exit 0; fi\nexit 2\n"
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	if err := (BilibiliDownloader{}).Ready(context.Background()); err != nil {
		t.Fatalf("Ready() = %v", err)
	}
}

func TestBilibiliDownloaderReadyReportsBrokenYTDLP(t *testing.T) {
	t.Setenv("LOCALBILIBILI_YTDLP", filepath.Join(t.TempDir(), "missing-yt-dlp"))
	err := (BilibiliDownloader{}).Ready(context.Background())
	if err == nil || !strings.Contains(err.Error(), "yt-dlp 不可用") {
		t.Fatalf("Ready() err = %v", err)
	}
}

func TestBilibiliDownloaderFetchesSpaceCardProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/web-interface/card" || r.URL.Query().Get("mid") != "54887875" {
			t.Fatalf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"card": map[string]any{
				"mid":  "54887875",
				"name": "阿辉白到发光",
				"face": "https://i.example/avatar.jpg",
				"sign": "八年徒手健身经验",
			}},
		})
	}))
	t.Cleanup(server.Close)

	profile, err := (BilibiliDownloader{BaseURL: server.URL, HTTPClient: server.Client()}).fetchSpaceCardProfile(t.Context(), "https://space.bilibili.com/54887875?spm_id_from=333.337.0.0", "SESSDATA=test")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "阿辉白到发光" || profile.UID != "54887875" || profile.ProfileURL != "https://space.bilibili.com/54887875" || profile.AvatarURL == "" || profile.Bio == "" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestBilibiliDownloaderYTDLPWritesLocalBilibiliLayout(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--dump-single-json" ]; then
    cat <<'JSON'
Deprecated Feature: Python 3.9 support is deprecated
{"id":"12345","title":"测试UP","uploader":"测试UP","uploader_id":"12345","webpage_url":"https://space.bilibili.com/12345","entries":[{"id":"BV1abc","title":"长视频","webpage_url":"https://www.bilibili.com/video/BV1abc","duration":600,"timestamp":1782892800},{"id":"BV1short","title":"短视频","webpage_url":"https://www.bilibili.com/video/BV1short","duration":60,"timestamp":1782979200}]}
JSON
    exit 0
  fi
done
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
    break
  fi
  prev="$arg"
done
if [ -z "$out" ]; then
  echo "missing output" >&2
  exit 2
fi
dir=$(dirname "$out")
mkdir -p "$dir"
printf video > "$dir/media_001.mp4"
printf '{"id":"ok"}' > "$dir/media_001.info.json"
printf cover > "$dir/media_001.jpg"
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	outputDir := filepath.Join(t.TempDir(), "测试UP")
	result, err := (BilibiliDownloader{}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 2 || result.CreatorName != "测试UP" {
		t.Fatalf("result = %#v", result)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("yt-dlp downloader should write flat files, got directory %s", entry.Name())
		}
	}
	for _, mediaPath := range []string{
		filepath.Join(outputDir, "2026-07-01_长视频_BV1abc.mp4"),
		filepath.Join(outputDir, "2026-07-02_短视频_BV1short.mp4"),
	} {
		if err := os.WriteFile(mediaPath, testMP4WithMoovAfterMdat(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, metadataPath := range []string{
		filepath.Join(outputDir, "2026-07-01_长视频_BV1abc_data.json"),
		filepath.Join(outputDir, "2026-07-02_短视频_BV1short_data.json"),
	} {
		if _, err := os.Stat(metadataPath); err != nil {
			t.Fatalf("expected flat metadata %s: %v", metadataPath, err)
		}
	}
	identity, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if identity.SecUID != "12345" || identity.ProfileURL != "https://space.bilibili.com/12345" {
		t.Fatalf("identity = %#v", identity)
	}
	if len(works) != 2 || works[0].WorkType != "video_short" || works[1].WorkType != "video_long" {
		t.Fatalf("works = %#v", works)
	}
}

func TestBilibiliDownloaderFallsBackToPlayURLWhenYTDLPVideoPageParsingFails(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--dump-single-json" ]; then
    cat <<'JSON'
{"id":"12345","title":"测试UP","uploader":"测试UP","uploader_id":"12345","webpage_url":"https://space.bilibili.com/12345","entries":[{"id":"BV1fallback","title":"BV1fallback","webpage_url":"https://www.bilibili.com/video/BV1fallback","duration":1951,"timestamp":1694087483}]}
JSON
    exit 0
  fi
done
echo "ERROR: [BiliBili] 1fallback: Unable to extract initial state" >&2
exit 3
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/view":
			if r.URL.Query().Get("bvid") != "BV1fallback" {
				t.Fatalf("unexpected view request: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"bvid":    "BV1fallback",
					"cid":     int64(1260157455),
					"title":   "投资是怎样赚钱的？",
					"desc":    "孟岩视频",
					"pubdate": int64(1694087483),
				},
			})
		case "/x/player/playurl":
			if r.URL.Query().Get("bvid") != "BV1fallback" || r.URL.Query().Get("cid") != "1260157455" || r.URL.Query().Get("fnval") != "0" {
				t.Fatalf("unexpected playurl request: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"quality": 64,
					"durl": []any{
						map[string]any{"url": server.URL + "/media/BV1fallback.mp4", "size": len(testMP4WithMoovAfterMdat()), "length": 1951000},
					},
				},
			})
		case "/media/BV1fallback.mp4":
			if got := r.Header.Get("Referer"); got != "https://www.bilibili.com/video/BV1fallback" {
				t.Fatalf("referer = %q", got)
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(testMP4WithMoovAfterMdat())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	outputDir := filepath.Join(t.TempDir(), "测试UP")
	result, err := (BilibiliDownloader{VideoAPIBaseURL: server.URL}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 1 {
		t.Fatalf("result = %#v", result)
	}
	_, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(works) != 1 || works[0].Title != "投资是怎样赚钱的？" || works[0].Description != "孟岩视频" {
		t.Fatalf("works = %#v", works)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "2023-09-07_投资是怎样赚钱的？_BV1fallback.mp4")); err != nil {
		t.Fatalf("expected fallback mp4: %v", err)
	}
}

func TestBilibiliDownloaderSkipsYTDLPPageParsingFailureWhenPlayURLUnavailable(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--dump-single-json" ]; then
    cat <<'JSON'
{"id":"12345","title":"测试UP","uploader":"测试UP","uploader_id":"12345","webpage_url":"https://space.bilibili.com/12345","entries":[{"id":"BV1blocked","title":"BV1blocked","webpage_url":"https://www.bilibili.com/video/BV1blocked","duration":1615,"timestamp":1728524510},{"id":"BV1nextok","title":"后续可下载","webpage_url":"https://www.bilibili.com/video/BV1nextok","duration":60,"timestamp":1728610910}]}
JSON
    exit 0
  fi
done
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
    break
  fi
  prev="$arg"
done
case "$*" in
  *BV1blocked*)
    printf 'ERROR: [BiliBili] 1blocked: ' >&2
    i=0
    while [ "$i" -lt 1200 ]; do
      printf x >&2
      i=$((i + 1))
    done
    echo "Unable to extract initial state" >&2
    exit 3
    ;;
esac
dir=$(dirname "$out")
mkdir -p "$dir"
printf video > "$dir/media_001.mp4"
printf '{"id":"BV1nextok","title":"后续可下载"}' > "$dir/media_001.info.json"
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/view":
			bvid := r.URL.Query().Get("bvid")
			data := map[string]any{"bvid": bvid, "cid": int64(26221546299), "title": bvid, "pubdate": int64(1728524510)}
			if bvid == "BV1nextok" {
				data["title"] = "后续可下载"
				data["pubdate"] = int64(1728610910)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
		case "/x/player/playurl":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 87008, "message": "87008"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	outputDir := filepath.Join(t.TempDir(), "测试UP")
	var messages []string
	result, err := (BilibiliDownloader{VideoAPIBaseURL: server.URL}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  outputDir,
		Progress: func(progress DownloadProgress) {
			messages = append(messages, progress.Message)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 1 {
		t.Fatalf("result = %#v", result)
	}
	if !containsString(messages, "跳过不可下载作品") {
		t.Fatalf("messages = %#v", messages)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "2024-10-11_后续可下载_BV1nextok.mp4"), testMP4WithMoovAfterMdat(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(works) != 1 || works[0].Title != "后续可下载" {
		t.Fatalf("works = %#v", works)
	}
}

func TestBilibiliDownloaderYTDLPDoesNotLimitPlaylistByDefault(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--playlist-end" ]; then
    echo "unexpected playlist limit" >&2
    exit 7
  fi
done
for arg in "$@"; do
  if [ "$arg" = "--dump-single-json" ]; then
    cat <<'JSON'
{"id":"12345","title":"测试UP","uploader":"测试UP","uploader_id":"12345","webpage_url":"https://space.bilibili.com/12345","entries":[{"id":"BV1first","title":"第一条","webpage_url":"https://www.bilibili.com/video/BV1first","duration":60,"timestamp":1782892800}]}
JSON
    exit 0
  fi
done
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
    break
  fi
  prev="$arg"
done
dir=$(dirname "$out")
mkdir -p "$dir"
printf video > "$dir/media_001.mp4"
printf '{"id":"BV1first","title":"第一条"}' > "$dir/media_001.info.json"
printf cover > "$dir/media_001.jpg"
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	t.Setenv("LOCALBILIBILI_YTDLP_MAX_WORKS", "")
	result, err := (BilibiliDownloader{}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  filepath.Join(t.TempDir(), "测试UP"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestYTDLPMaxWorksDefaultsToUnlimited(t *testing.T) {
	t.Setenv("LOCALBILIBILI_YTDLP_MAX_WORKS", "")
	if got := ytdlpMaxWorks(); got != 0 {
		t.Fatalf("default ytdlpMaxWorks() = %d, want unlimited 0", got)
	}
	t.Setenv("LOCALBILIBILI_YTDLP_MAX_WORKS", "3")
	if got := ytdlpMaxWorks(); got != 3 {
		t.Fatalf("configured ytdlpMaxWorks() = %d, want 3", got)
	}
	t.Setenv("LOCALBILIBILI_YTDLP_MAX_WORKS", "not-a-number")
	if got := ytdlpMaxWorks(); got != 0 {
		t.Fatalf("invalid ytdlpMaxWorks() = %d, want unlimited 0", got)
	}
	t.Setenv("LOCALBILIBILI_YTDLP_MAX_WORKS", "500")
	if got := ytdlpMaxWorks(); got != 500 {
		t.Fatalf("large configured ytdlpMaxWorks() = %d, want 500", got)
	}
}

func TestBilibiliDownloaderYTDLPTimeoutIsSkippable(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := "#!/bin/sh\nsleep 10\n"
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	t.Setenv("LOCALBILIBILI_YTDLP_TIMEOUT_SECONDS", "1")
	start := time.Now()
	_, err := (BilibiliDownloader{}).runYTDLP(context.Background(), "--no-playlist", "https://www.bilibili.com/video/BV1timeout")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
	if !isPermanentYTDLPError(err) {
		t.Fatalf("timeout should be skippable, err = %v", err)
	}
}

func TestBilibiliDownloaderTreatsTruncatedBilibiliExtractionErrorAsSkippable(t *testing.T) {
	err := errors.New("yt-dlp 执行失败: [BiliBili] Extracting URL: https://www.bilibili.com/video/BV1ch2MYmEHm [BiliBili] 1ch2MYmEHm: Downloading webpage [BiliBili] BV1ch2MYmEHm: Extracti...")
	if !isPermanentYTDLPError(err) {
		t.Fatalf("truncated extraction error should be skippable: %v", err)
	}
}

func TestBilibiliDownloaderFallsBackToSpaceArchiveWhenYTDLPPlaylistBlocked(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--dump-single-json" ]; then
    echo "ERROR: [BilibiliSpaceVideo] 12345: Request is blocked by server (412)" >&2
    exit 3
  fi
done
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
    break
  fi
  prev="$arg"
done
if [ -z "$out" ]; then
  echo "missing output" >&2
  exit 2
fi
dir=$(dirname "$out")
mkdir -p "$dir"
printf video > "$dir/media_001.mp4"
printf '{"id":"ok"}' > "$dir/media_001.info.json"
printf cover > "$dir/media_001.jpg"
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	requestedPages := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/space/arc/search":
			if r.URL.Query().Get("mid") != "12345" || r.URL.Query().Get("ps") != "50" {
				t.Fatalf("unexpected space request: %s?%s", r.URL.Path, r.URL.RawQuery)
			}
			requestedPages = append(requestedPages, r.URL.Query().Get("pn"))
			page := r.URL.Query().Get("pn")
			videos := []any{}
			if page == "1" {
				videos = []any{
					map[string]any{"bvid": "BV1spaceA", "title": "空间第一页", "description": "简介 A", "author": "测试UP", "mid": 12345, "created": 1782892800, "length": "12:34", "pic": "http://i0.hdslb.com/a.jpg"},
					map[string]any{"bvid": "BV1skip", "title": "互动视频跳过", "author": "测试UP", "mid": 12345, "created": 1782892801, "length": "01:00", "is_steins_gate": 1},
				}
			} else if page == "2" {
				videos = []any{
					map[string]any{"bvid": "BV1spaceB", "title": "空间第二页", "description": "简介 B", "author": "测试UP", "mid": 12345, "created": 1782979200, "length": "1:02:03", "pic": "//i0.hdslb.com/b.jpg"},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"list": map[string]any{"vlist": videos},
					"page": map[string]any{"count": 100, "pn": len(requestedPages), "ps": 50},
				},
			})
		case "/x/web-interface/view":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": -404, "message": "not found"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	outputDir := filepath.Join(t.TempDir(), "测试UP")
	var progressMessages []string
	result, err := (BilibiliDownloader{SpaceAPIBaseURL: server.URL, VideoAPIBaseURL: server.URL}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  outputDir,
		Progress: func(progress DownloadProgress) {
			progressMessages = append(progressMessages, progress.Message)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 2 || result.CreatorName != "测试UP" {
		t.Fatalf("result = %#v", result)
	}
	if strings.Join(requestedPages, ",") != "1,2" {
		t.Fatalf("requested pages = %#v", requestedPages)
	}
	if !containsString(progressMessages, "yt-dlp列表获取失败，已改用B站空间分页列表") {
		t.Fatalf("progress messages = %#v", progressMessages)
	}
	for _, mediaPath := range []string{
		filepath.Join(outputDir, "2026-07-01_空间第一页_BV1spaceA.mp4"),
		filepath.Join(outputDir, "2026-07-02_空间第二页_BV1spaceB.mp4"),
	} {
		if err := os.WriteFile(mediaPath, testMP4WithMoovAfterMdat(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	identity, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if identity.SecUID != "12345" || identity.ProfileURL != "https://space.bilibili.com/12345" {
		t.Fatalf("identity = %#v", identity)
	}
	titles := map[string]bool{}
	for _, work := range works {
		titles[work.Title] = true
	}
	if len(works) != 2 || !titles["空间第一页"] || !titles["空间第二页"] {
		t.Fatalf("works = %#v", works)
	}
}

func TestBilibiliDownloaderFallsBackToSeasonArchivesWhenSpaceArchiveBlocked(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--dump-single-json" ]; then
    echo "ERROR: [BilibiliSpaceVideo] 12345: Request is blocked by server (412)" >&2
    exit 3
  fi
done
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
    break
  fi
  prev="$arg"
done
if [ -z "$out" ]; then
  echo "missing output" >&2
  exit 2
fi
dir=$(dirname "$out")
mkdir -p "$dir"
printf video > "$dir/media_001.mp4"
printf '{"id":"ok"}' > "$dir/media_001.info.json"
printf cover > "$dir/media_001.jpg"
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	requestedSeasonPages := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/space/arc/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": -799, "message": "请求过于频繁，请稍后再试"})
		case "/x/polymer/web-space/seasons_series_list":
			if r.URL.Query().Get("mid") != "12345" {
				t.Fatalf("unexpected season list request: %s?%s", r.URL.Path, r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"items_lists": map[string]any{
						"seasons_list": []any{
							map[string]any{"meta": map[string]any{"mid": 12345, "season_id": 809657, "title": "商业分析", "total": 3}},
						},
					},
				},
			})
		case "/x/polymer/web-space/seasons_archives_list":
			if r.URL.Query().Get("mid") != "12345" || r.URL.Query().Get("season_id") != "809657" || r.URL.Query().Get("page_size") != "30" {
				t.Fatalf("unexpected season archive request: %s?%s", r.URL.Path, r.URL.RawQuery)
			}
			page := r.URL.Query().Get("page_num")
			requestedSeasonPages = append(requestedSeasonPages, page)
			videos := []any{}
			if page == "1" {
				videos = []any{
					map[string]any{"bvid": "BV1seasonA", "title": "合集第一页", "author": "测试UP", "mid": 12345, "ctime": 1782892800, "duration": 754, "pic": "http://i0.hdslb.com/a.jpg"},
				}
				for i := 0; i < 29; i++ {
					videos = append(videos, map[string]any{"bvid": "BV1interactive" + strconv.Itoa(i), "title": "互动视频跳过", "author": "测试UP", "mid": 12345, "ctime": 1782892801, "duration": 60, "interactive_video": true})
				}
			} else if page == "2" {
				videos = []any{
					map[string]any{"bvid": "BV1seasonB", "title": "合集第二页", "author": "测试UP", "mid": 12345, "pubdate": 1782979200, "duration": 3661, "pic": "//i0.hdslb.com/b.jpg"},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"archives": videos,
					"page":     map[string]any{"page_num": len(requestedSeasonPages), "page_size": 30, "total": 31},
				},
			})
		case "/x/web-interface/view":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": -404, "message": "not found"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	outputDir := filepath.Join(t.TempDir(), "测试UP")
	var progressMessages []string
	result, err := (BilibiliDownloader{SpaceAPIBaseURL: server.URL, VideoAPIBaseURL: server.URL}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  outputDir,
		Progress: func(progress DownloadProgress) {
			progressMessages = append(progressMessages, progress.Message)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 2 || result.CreatorName != "测试UP" {
		t.Fatalf("result = %#v", result)
	}
	if strings.Join(requestedSeasonPages, ",") != "1,2" {
		t.Fatalf("requested season pages = %#v", requestedSeasonPages)
	}
	if !containsString(progressMessages, "yt-dlp列表获取失败，已改用B站合集分页列表") {
		t.Fatalf("progress messages = %#v", progressMessages)
	}
	for _, mediaPath := range []string{
		filepath.Join(outputDir, "2026-07-01_合集第一页_BV1seasonA.mp4"),
		filepath.Join(outputDir, "2026-07-02_合集第二页_BV1seasonB.mp4"),
	} {
		if err := os.WriteFile(mediaPath, testMP4WithMoovAfterMdat(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	titles := map[string]bool{}
	for _, work := range works {
		titles[work.Title] = true
	}
	if len(works) != 2 || !titles["合集第一页"] || !titles["合集第二页"] {
		t.Fatalf("works = %#v", works)
	}
}

func TestBilibiliDownloaderFallsBackToDynamicFeedWhenArchiveAndSeasonUnavailable(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--dump-single-json" ]; then
    echo "ERROR: [BilibiliSpaceVideo] 12345: Request is blocked by server (412)" >&2
    exit 3
  fi
done
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
    break
  fi
  prev="$arg"
done
if [ -z "$out" ]; then
  echo "missing output" >&2
  exit 2
fi
dir=$(dirname "$out")
mkdir -p "$dir"
printf video > "$dir/media_001.mp4"
printf '{"id":"ok"}' > "$dir/media_001.info.json"
printf cover > "$dir/media_001.jpg"
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	requestedDynamicOffsets := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/space/arc/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": -799, "message": "请求过于频繁，请稍后再试"})
		case "/x/polymer/web-space/seasons_series_list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"items_lists": map[string]any{"seasons_list": []any{}}},
			})
		case "/x/polymer/web-dynamic/desktop/v1/feed/space":
			if r.URL.Query().Get("host_mid") != "12345" {
				t.Fatalf("unexpected dynamic request: %s?%s", r.URL.Path, r.URL.RawQuery)
			}
			offset := r.URL.Query().Get("offset")
			requestedDynamicOffsets = append(requestedDynamicOffsets, offset)
			items := []any{}
			hasMore := false
			nextOffset := ""
			if offset == "" {
				items = []any{
					map[string]any{"modules": map[string]any{"module_dynamic": map[string]any{"major": map[string]any{"archive": map[string]any{"bvid": "BV1dynamicA"}}}}},
					map[string]any{"basic": map[string]any{"comment_id_str": "BV1dynamicA"}},
				}
				hasMore = true
				nextOffset = "next-page"
			} else if offset == "next-page" {
				items = []any{
					map[string]any{"modules": map[string]any{"module_dynamic": map[string]any{"major": map[string]any{"archive": map[string]any{"bvid": "BV1dynamicB"}}}}},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"items": items, "has_more": hasMore, "offset": nextOffset},
			})
		case "/x/web-interface/view":
			switch r.URL.Query().Get("bvid") {
			case "BV1dynamicA":
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"bvid": "BV1dynamicA", "title": "动态第一页", "desc": "A", "pubdate": 1782892800}})
			case "BV1dynamicB":
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"bvid": "BV1dynamicB", "title": "动态第二页", "desc": "B", "pubdate": 1782979200}})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{"code": -404, "message": "not found"})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	outputDir := filepath.Join(t.TempDir(), "测试UP")
	var progressMessages []string
	result, err := (BilibiliDownloader{SpaceAPIBaseURL: server.URL, VideoAPIBaseURL: server.URL}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  outputDir,
		Progress: func(progress DownloadProgress) {
			progressMessages = append(progressMessages, progress.Message)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 2 || result.CreatorName != "测试UP" {
		t.Fatalf("result = %#v", result)
	}
	if strings.Join(requestedDynamicOffsets, ",") != ",next-page" {
		t.Fatalf("requested dynamic offsets = %#v", requestedDynamicOffsets)
	}
	if !containsString(progressMessages, "yt-dlp列表获取失败，已改用B站动态流列表") {
		t.Fatalf("progress messages = %#v", progressMessages)
	}
	for _, mediaPath := range []string{
		filepath.Join(outputDir, "2026-07-01_动态第一页_BV1dynamicA.mp4"),
		filepath.Join(outputDir, "2026-07-02_动态第二页_BV1dynamicB.mp4"),
	} {
		if err := os.WriteFile(mediaPath, testMP4WithMoovAfterMdat(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	titles := map[string]bool{}
	for _, work := range works {
		titles[work.Title] = true
	}
	if len(works) != 2 || !titles["动态第一页"] || !titles["动态第二页"] {
		t.Fatalf("works = %#v", works)
	}
}

func TestBilibiliDownloaderFailsWhenRemotePlaylistBlockedAndNoMetadataRepaired(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--dump-single-json" ]; then
    echo "ERROR: Request is blocked by server (412)" >&2
    exit 3
  fi
done
exit 2
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/space/arc/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": -799, "message": "请求过于频繁，请稍后再试"})
		case "/x/polymer/web-space/seasons_series_list":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": -799, "message": "请求过于频繁，请稍后再试"})
		case "/x/web-interface/view":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": -404, "message": "not found"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	outputDir := filepath.Join(t.TempDir(), "测试UP")
	workDir := filepath.Join(outputDir, "2026-07-01_旧作品_BV1old")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := localBilibiliDownloadMetadata{
		WorkType:  "video_short",
		BVID:      "BV1old",
		Title:     "旧作品",
		SourceURL: "https://www.bilibili.com/video/BV1old",
		Creator:   localBilibiliDownloadCreatorMeta{MID: "12345", Name: "测试UP", SpaceURL: "https://space.bilibili.com/12345"},
		Media:     []localBilibiliDownloadMediaMeta{{Type: "video", File: "media_001.mp4"}},
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "metadata.localbilibili.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	_, err = (BilibiliDownloader{SpaceAPIBaseURL: server.URL, VideoAPIBaseURL: server.URL}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  outputDir,
	})
	if err == nil {
		t.Fatal("expected playlist failure to be visible")
	}
	if !strings.Contains(err.Error(), "B站空间分页列表失败") || !strings.Contains(err.Error(), "B站合集分页列表失败") {
		t.Fatalf("err = %v", err)
	}
}

func TestBilibiliDownloaderYTDLPUsesVideoDetailTitle(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--dump-single-json" ]; then
    cat <<'JSON'
{"id":"12345","title":"测试UP","uploader":"测试UP","uploader_id":"12345","webpage_url":"https://space.bilibili.com/12345","entries":[{"id":"BV1fallback","title":"BV1fallback","webpage_url":"https://www.bilibili.com/video/BV1fallback","duration":42,"timestamp":1782892800}]}
JSON
    exit 0
  fi
done
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
    break
  fi
  prev="$arg"
done
dir=$(dirname "$out")
mkdir -p "$dir"
printf video > "$dir/media_001.mp4"
printf '{"id":"BV1fallback","title":"完整 info 标题"}' > "$dir/media_001.info.json"
printf cover > "$dir/media_001.jpg"
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/web-interface/view" || r.URL.Query().Get("bvid") != "BV1fallback" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"bvid":    "BV1fallback",
				"title":   "真实 B站标题",
				"desc":    "真实简介",
				"pubdate": int64(1782892800),
			},
		})
	}))
	defer server.Close()
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	outputDir := filepath.Join(t.TempDir(), "测试UP")
	_, err := (BilibiliDownloader{VideoAPIBaseURL: server.URL}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "2026-07-01_真实 B站标题_BV1fallback.mp4"), testMP4WithMoovAfterMdat(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(works) != 1 {
		t.Fatalf("works = %#v", works)
	}
	if works[0].Title != "真实 B站标题" || works[0].Description != "真实简介" || works[0].PublishedAt != "2026-07-01T08:00:00Z" {
		t.Fatalf("work metadata = %#v", works[0])
	}
}

func TestBilibiliDownloaderRepairsExistingBVIDTitleMetadata(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--dump-single-json" ]; then
    cat <<'JSON'
{"id":"12345","title":"测试UP","uploader":"测试UP","uploader_id":"12345","webpage_url":"https://space.bilibili.com/12345","entries":[{"id":"BV1repair","title":"BV1repair","webpage_url":"https://www.bilibili.com/video/BV1repair","duration":42,"timestamp":1782892800}]}
JSON
    exit 0
  fi
done
echo "download should not run" >&2
exit 9
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(t.TempDir(), "测试UP")
	workDir := filepath.Join(outputDir, "2026-07-01_BV1repair_BV1repair")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "media_001.mp4"), testMP4WithMoovAfterMdat(), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := `{"work_type":"video_short","bvid":"BV1repair","title":"BV1repair","source_url":"https://www.bilibili.com/video/BV1repair","creator":{"mid":"12345","name":"测试UP","space_url":"https://space.bilibili.com/12345"},"media":[{"type":"video","file":"media_001.mp4"}]}`
	if err := os.WriteFile(filepath.Join(workDir, "metadata.localbilibili.json"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/web-interface/view" || r.URL.Query().Get("bvid") != "BV1repair" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"bvid":    "BV1repair",
				"title":   "修复后的标题",
				"desc":    "修复后的简介",
				"pubdate": int64(1782892800),
			},
		})
	}))
	defer server.Close()
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	result, err := (BilibiliDownloader{VideoAPIBaseURL: server.URL}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 0 {
		t.Fatalf("result = %#v", result)
	}
	_, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(works) != 1 || works[0].Title != "修复后的标题" || works[0].Description != "修复后的简介" {
		t.Fatalf("works = %#v", works)
	}
}

func TestBilibiliDownloaderRepairsExistingMetadataWhenPlaylistBlocked(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
set -eu
for arg in "$@"; do
  if [ "$arg" = "--version" ]; then
    echo 2026.01.01
    exit 0
  fi
  if [ "$arg" = "--dump-single-json" ]; then
    echo "ERROR: [BilibiliSpaceVideo] 12345: Request is blocked by server (412)" >&2
    exit 3
  fi
done
echo "download should not run" >&2
exit 9
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(t.TempDir(), "测试UP")
	workDir := filepath.Join(outputDir, "2026-07-01_BV1blocked_BV1blocked")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "media_001.mp4"), testMP4WithMoovAfterMdat(), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := `{"work_type":"video_short","bvid":"BV1blocked","title":"BV1blocked","source_url":"https://www.bilibili.com/video/BV1blocked","creator":{"mid":"12345","name":"测试UP","space_url":"https://space.bilibili.com/12345"},"media":[{"type":"video","file":"media_001.mp4"}]}`
	if err := os.WriteFile(filepath.Join(workDir, "metadata.localbilibili.json"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/web-interface/view" || r.URL.Query().Get("bvid") != "BV1blocked" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"bvid":    "BV1blocked",
				"title":   "列表失败后的修复标题",
				"desc":    "列表失败后的修复简介",
				"pubdate": int64(1782892800),
			},
		})
	}))
	defer server.Close()
	t.Setenv("LOCALBILIBILI_YTDLP", tool)
	result, err := (BilibiliDownloader{VideoAPIBaseURL: server.URL}).UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "测试UP"},
		ProfileURL: "https://space.bilibili.com/12345",
		OutputDir:  outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 0 {
		t.Fatalf("result = %#v", result)
	}
	_, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(works) != 1 || works[0].Title != "列表失败后的修复标题" || works[0].Description != "列表失败后的修复简介" {
		t.Fatalf("works = %#v", works)
	}
}

func TestBilibiliDownloaderOpusWritesImageTextLayout(t *testing.T) {
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/opus/961688906112172040":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html>
<html><head><title>GOurDladle的动态 - 哔哩哔哩</title></head>
<body>
<span class="opus-module-title__text">图文无关</span>
<div class="opus-module-author__pub__text">2024年08月04日 13:24</div>
<script>
window.__INITIAL_STATE__ = {
  "opus": {"id": "961688906112172040", "pub_ts": "1722749047", "pub_time": "2024年08月04日 13:24"},
  "author": {"mid": "14784985", "name": "GOurDladle"},
  "module_title": {"text": "图文无关"},
  "module_content": {"paragraphs": [{"text": {"nodes": [{"word": {"words": "分享图片"}}]}}]}
};
</script>
<img src="` + base + `/bfs/new_dyn/one.jpg@80w_80h_1c">
<img src="` + base + `/bfs/new_dyn/two.jpg@80w_80h_1c">
</body></html>`))
		case "/bfs/new_dyn/one.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("image-one"))
		case "/bfs/new_dyn/two.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("image-two"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	base = server.URL

	outputDir := filepath.Join(t.TempDir(), "opus")
	result, err := (BilibiliDownloader{HTTPClient: server.Client()}).UpdateCreator(context.Background(), DownloadTask{
		ProfileURL: server.URL + "/opus/961688906112172040",
		OutputDir:  outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 1 || result.CreatorName != "GOurDladle" {
		t.Fatalf("result = %#v", result)
	}
	workDir := filepath.Join(outputDir, "2024-08-04_图文无关_961688906112172040")
	for _, name := range []string{"metadata.localbilibili.json", "source.bilibili.json", "cover.jpg", "image_001.jpg", "image_002.jpg"} {
		if _, err := os.Stat(filepath.Join(workDir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
	identity, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if identity.Name != "GOurDladle" || identity.SecUID != "14784985" {
		t.Fatalf("identity = %#v", identity)
	}
	if len(works) != 1 || works[0].WorkType != "image_text" || len(works[0].Media) != 2 {
		t.Fatalf("works = %#v", works)
	}
	if works[0].Title != "图文无关" || works[0].Description != "分享图片" || works[0].PublishedAt != "2024-08-04T05:24:07Z" {
		t.Fatalf("work metadata = %#v", works[0])
	}
}

func TestBilibiliDownloaderWritesFlatFilesAndScannerReadsThem(t *testing.T) {
	var apiBase string
	mediaHits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/aweme/v1/web/user/profile/other/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]any{
					"uid":      "uid-1",
					"sec_uid":  "sec-flat",
					"nickname": "阿 言/测试",
				},
			})
		case "/aweme/v1/web/aweme/post/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"has_more":   0,
				"max_cursor": 0,
				"aweme_list": []any{
					map[string]any{
						"aweme_id":    "1000000000000000001",
						"desc":        "视频作品 #健身",
						"create_time": int64(1778912745),
						"author": map[string]any{
							"uid":      "uid-1",
							"sec_uid":  "sec-flat",
							"nickname": "阿 言/测试",
							"share_info": map[string]any{
								"share_url": "https://www.bilibili.com/user/sec-flat",
							},
						},
						"music": map[string]any{"title": "BGM"},
						"video": map[string]any{
							"play_addr": map[string]any{
								"url_list": []any{apiBase + "/media/music.mp3"},
							},
							"bit_rate": []any{map[string]any{
								"play_addr": map[string]any{
									"url_list": []any{apiBase + "/media/video.mp4"},
								},
							}},
						},
					},
					map[string]any{
						"aweme_id":    "1000000000000000002",
						"desc":        "图集作品",
						"create_time": int64(1778999145),
						"author": map[string]any{
							"uid":      "uid-1",
							"sec_uid":  "sec-flat",
							"nickname": "阿 言/测试",
						},
						"images": []any{
							map[string]any{
								"url_list": []any{apiBase + "/media/one.webp"},
							},
							map[string]any{
								"display_image": map[string]any{
									"url_list": []any{apiBase + "/media/two.webp"},
								},
							},
						},
					},
				},
			})
		case "/media/video.mp4", "/media/one.webp", "/media/two.webp":
			mediaHits[r.URL.Path]++
			_, _ = w.Write([]byte("media-" + r.URL.Path))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	apiBase = server.URL

	outputDir := filepath.Join(t.TempDir(), "阿言")
	downloader := BilibiliDownloader{BaseURL: server.URL, HTTPClient: server.Client()}
	result, err := downloader.UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "阿言"},
		ProfileURL: "https://www.bilibili.com/user/sec-flat",
		OutputDir:  outputDir,
		Cookie:     "msToken=one; ttwid=two",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 2 {
		t.Fatalf("added works = %d", result.AddedWorks)
	}
	if result.CreatorName != "阿 言 测试" {
		t.Fatalf("creator name = %q", result.CreatorName)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("downloader should write flat files, got directory %s", entry.Name())
		}
	}
	expectedNames := []string{
		"2026-05-16_视频作品 #健身_1000000000000000001.mp4",
		"2026-05-16_视频作品 #健身_1000000000000000001_data.json",
		"2026-05-17_图集作品_1000000000000000002_1.webp",
		"2026-05-17_图集作品_1000000000000000002_2.webp",
		"2026-05-17_图集作品_1000000000000000002_data.json",
	}
	for _, name := range expectedNames {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("expected flat file %s: %v", name, err)
		}
	}

	identity, works, err := scanCreatorWithIdentity(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if identity.SecUID != "sec-flat" || identity.ProfileURL != "https://www.bilibili.com/user/sec-flat" {
		t.Fatalf("identity = %#v", identity)
	}
	if len(works) != 2 {
		t.Fatalf("works len = %d: %#v", len(works), works)
	}
	if works[0].AwemeID == "" || len(works[0].Media) == 0 {
		t.Fatalf("scanner did not read downloaded works: %#v", works)
	}

	result, err = downloader.UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "阿言"},
		ProfileURL: "https://www.bilibili.com/user/sec-flat",
		OutputDir:  outputDir,
		Cookie:     "msToken=one; ttwid=two",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 0 {
		t.Fatalf("second update should skip existing works, added=%d", result.AddedWorks)
	}
	for path, hits := range mediaHits {
		if hits != 1 {
			t.Fatalf("media %s downloaded %d times", path, hits)
		}
	}
}

func TestBilibiliDownloaderRetriesTransientMediaFailure(t *testing.T) {
	var apiBase string
	mediaHits := 0
	var progressMessages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/aweme/v1/web/user/profile/other/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]any{
					"uid":      "uid-retry",
					"sec_uid":  "sec-retry",
					"nickname": "重试作者",
				},
			})
		case "/aweme/v1/web/aweme/post/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"has_more":   0,
				"max_cursor": 0,
				"aweme_list": []any{
					map[string]any{
						"aweme_id":    "7100000000000000001",
						"desc":        "网络抖动作品",
						"create_time": int64(1778912745),
						"author":      map[string]any{"uid": "uid-retry", "sec_uid": "sec-retry", "nickname": "重试作者"},
						"video": map[string]any{
							"play_addr": map[string]any{"url_list": []any{apiBase + "/media/retry.mp4"}},
						},
					},
				},
			})
		case "/media/retry.mp4":
			mediaHits++
			if mediaHits == 1 {
				http.Error(w, "temporary", http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte("video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	apiBase = server.URL

	outputDir := filepath.Join(t.TempDir(), "重试作者")
	downloader := BilibiliDownloader{BaseURL: server.URL, HTTPClient: server.Client()}
	result, err := downloader.UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "重试作者"},
		ProfileURL: "https://www.bilibili.com/user/sec-retry",
		OutputDir:  outputDir,
		Cookie:     "msToken=one; ttwid=two",
		Progress: func(progress DownloadProgress) {
			if progress.Message != "" {
				progressMessages = append(progressMessages, progress.Message)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 1 || mediaHits != 2 {
		t.Fatalf("result=%#v mediaHits=%d", result, mediaHits)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "2026-05-16_网络抖动作品_7100000000000000001.mp4")); err != nil {
		t.Fatalf("expected retried media file: %v", err)
	}
	if !containsDownloaderTestString(progressMessages, "正在重试 2/3") {
		t.Fatalf("expected retry progress message, got %#v", progressMessages)
	}
}

func TestBilibiliDownloaderSkipsForbiddenMediaAndContinues(t *testing.T) {
	var apiBase string
	var lastProgress DownloadProgress
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/aweme/v1/web/user/profile/other/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]any{
					"uid":       "uid-skip",
					"sec_uid":   "sec-skip",
					"nickname":  "跳过作者",
					"signature": "作者签名",
				},
			})
		case "/aweme/v1/web/aweme/post/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"has_more":   0,
				"max_cursor": 0,
				"aweme_list": []any{
					map[string]any{
						"aweme_id":    "7200000000000000001",
						"desc":        "失效作品",
						"create_time": int64(1778912745),
						"author":      map[string]any{"uid": "uid-skip", "sec_uid": "sec-skip", "nickname": "跳过作者"},
						"video": map[string]any{
							"play_addr": map[string]any{"url_list": []any{apiBase + "/media/forbidden.mp4"}},
						},
					},
					map[string]any{
						"aweme_id":    "7200000000000000002",
						"desc":        "后续作品",
						"create_time": int64(1778999145),
						"author":      map[string]any{"uid": "uid-skip", "sec_uid": "sec-skip", "nickname": "跳过作者"},
						"video": map[string]any{
							"play_addr": map[string]any{"url_list": []any{apiBase + "/media/ok.mp4"}},
						},
					},
				},
			})
		case "/media/forbidden.mp4":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/media/ok.mp4":
			_, _ = w.Write([]byte("video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	apiBase = server.URL

	outputDir := filepath.Join(t.TempDir(), "跳过作者")
	downloader := BilibiliDownloader{BaseURL: server.URL, HTTPClient: server.Client()}
	result, err := downloader.UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "跳过作者"},
		ProfileURL: "https://www.bilibili.com/user/sec-skip",
		OutputDir:  outputDir,
		Cookie:     "msToken=one; ttwid=two",
		Progress: func(progress DownloadProgress) {
			lastProgress = progress
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedWorks != 1 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "2026-05-16_失效作品_7200000000000000001_data.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forbidden work should not write data json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "2026-05-17_后续作品_7200000000000000002.mp4")); err != nil {
		t.Fatalf("expected following work to download: %v", err)
	}
	if lastProgress.SkippedWorks != 1 || !strings.Contains(lastProgress.Notice, "失效作品") || !strings.Contains(lastProgress.Notice, "HTTP 403") {
		t.Fatalf("progress should summarize skipped media: %#v", lastProgress)
	}
}

func TestBilibiliDownloaderDefaultClientDoesNotSetWholeBodyTimeout(t *testing.T) {
	if timeout := (BilibiliDownloader{}).client().Timeout; timeout != 0 {
		t.Fatalf("default client should not use whole body timeout, got %s", timeout)
	}
}

func TestBilibiliDownloaderTrimsLongUTF8FileNames(t *testing.T) {
	longTitle := strings.Repeat("创业十年，我们一直在路上；", 20)
	var apiBase string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/aweme/v1/web/user/profile/other/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]any{
					"uid":      "uid-1",
					"sec_uid":  "sec-long",
					"nickname": "长标题作者",
				},
			})
		case "/aweme/v1/web/aweme/post/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"has_more":   0,
				"max_cursor": 0,
				"aweme_list": []any{
					map[string]any{
						"aweme_id":    "7122773957572447529",
						"desc":        longTitle,
						"create_time": int64(1658534400),
						"author": map[string]any{
							"uid":      "uid-1",
							"sec_uid":  "sec-long",
							"nickname": "长标题作者",
						},
						"video": map[string]any{
							"play_addr": map[string]any{
								"url_list": []any{apiBase + "/media/video.mp4"},
							},
						},
					},
				},
			})
		case "/media/video.mp4":
			_, _ = w.Write([]byte("video"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	apiBase = server.URL

	outputDir := filepath.Join(t.TempDir(), "长标题作者")
	downloader := BilibiliDownloader{BaseURL: server.URL, HTTPClient: server.Client()}
	_, err := downloader.UpdateCreator(context.Background(), DownloadTask{
		Creator:    Creator{Name: "占位"},
		ProfileURL: "https://www.bilibili.com/user/sec-long",
		OutputDir:  outputDir,
		Cookie:     "msToken=one; ttwid=two",
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries len = %d", len(entries))
	}
	for _, entry := range entries {
		if len(entry.Name()) >= 255 {
			t.Fatalf("file name too long after trimming: %q (%d bytes)", entry.Name(), len(entry.Name()))
		}
	}
}

func TestBilibiliDownloaderRequiresCookieAndRejectsEmptyResponses(t *testing.T) {
	downloader := BilibiliDownloader{BaseURL: "https://example.invalid"}
	_, err := downloader.UpdateCreator(context.Background(), DownloadTask{
		ProfileURL: "https://www.bilibili.com/user/sec",
		OutputDir:  t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "Cookie") {
		t.Fatalf("expected cookie error, got %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()
	downloader = BilibiliDownloader{BaseURL: server.URL, HTTPClient: server.Client()}
	_, err = downloader.UpdateCreator(context.Background(), DownloadTask{
		ProfileURL: "https://www.bilibili.com/user/sec",
		OutputDir:  t.TempDir(),
		Cookie:     "msToken=one",
	})
	if err == nil || !strings.Contains(err.Error(), "获取用户信息失败") {
		t.Fatalf("expected user info error, got %v", err)
	}
}

func TestBilibiliDownloaderRejectsUnwritableOutputDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to read-only directories")
	}
	outputDir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(outputDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(outputDir, 0o755) })

	downloader := BilibiliDownloader{BaseURL: "https://example.invalid"}
	_, err := downloader.UpdateCreator(context.Background(), DownloadTask{
		ProfileURL: "https://www.bilibili.com/user/sec",
		OutputDir:  outputDir,
		Cookie:     "msToken=one",
	})
	if err == nil || !strings.Contains(err.Error(), "下载目录不可写") || !strings.Contains(err.Error(), "fnOS") {
		t.Fatalf("expected writable directory error, got %v", err)
	}
}

func containsDownloaderTestString(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
