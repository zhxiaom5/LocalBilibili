package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if _, err := os.Stat(filepath.Join(outputDir, "2026-07-01_长视频_BV1abc", "metadata.localbilibili.json")); err != nil {
		t.Fatalf("expected metadata: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "2026-07-01_长视频_BV1abc", "source.ytdlp.json")); err != nil {
		t.Fatalf("expected source ytdlp json: %v", err)
	}
	for _, mediaPath := range []string{
		filepath.Join(outputDir, "2026-07-01_长视频_BV1abc", "media_001.mp4"),
		filepath.Join(outputDir, "2026-07-02_短视频_BV1short", "media_001.mp4"),
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
	if len(works) != 2 || works[0].WorkType != "video_short" || works[1].WorkType != "video_long" {
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
