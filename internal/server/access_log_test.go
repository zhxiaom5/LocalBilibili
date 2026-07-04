package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestAccessLogWritesAllHTTPRequestsToFile(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("LOCALBILIBILI_LOG_DIR", logDir)
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Routes()

	staticReq := httptest.NewRequest(http.MethodGet, "/assets/app.js?token=secret&x=1", nil)
	staticReq.RemoteAddr = "198.51.100.7:4321"
	staticReq.Header.Set("User-Agent", `LocalBilibili Test "Agent"`)
	staticReq.Header.Set("Referer", "http://example.test/from")
	staticRec := httptest.NewRecorder()
	handler.ServeHTTP(staticRec, staticReq)
	if staticRec.Code != http.StatusOK {
		t.Fatalf("static status = %d", staticRec.Code)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	apiRec := httptest.NewRecorder()
	handler.ServeHTTP(apiRec, apiReq)
	if apiRec.Code != http.StatusUnauthorized {
		t.Fatalf("api status = %d", apiRec.Code)
	}

	optionsReq := httptest.NewRequest(http.MethodOptions, "/api/feed", nil)
	optionsReq.Header.Set("Origin", "http://example.test")
	optionsRec := httptest.NewRecorder()
	handler.ServeHTTP(optionsRec, optionsReq)
	if optionsRec.Code != http.StatusNoContent {
		t.Fatalf("options status = %d", optionsRec.Code)
	}

	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(logDir, accessLogFileName))
	if err != nil {
		t.Fatal(err)
	}
	logs := string(data)
	assertContains(t, logs, `198.51.100.7 - - [`)
	assertContains(t, logs, `"GET /assets/app.js?token=REDACTED&x=1 HTTP/1.1" 200 23 "http://example.test/from" "LocalBilibili Test \"Agent\""`)
	assertContains(t, logs, `"GET /api/auth/me HTTP/1.1" 401`)
	assertContains(t, logs, `"OPTIONS /api/feed HTTP/1.1" 204`)
	assertHasRequestTime(t, logs)
	if strings.Contains(logs, "secret") {
		t.Fatalf("access log leaked sensitive query value: %s", logs)
	}
}

func TestAccessLogRecordsPartialContentStatusAndBytes(t *testing.T) {
	logDir := t.TempDir()
	logger, err := NewAccessLogger(logDir)
	if err != nil {
		t.Fatal(err)
	}
	handler := logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accessLogSetMedia(r, accessLogMedia{
			ID:       2600,
			Type:     "video",
			Range:    r.Header.Get("Range"),
			FileSize: 123456,
			OpenTime: 2 * time.Millisecond,
			StatTime: time.Millisecond,
		})
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("video"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/media/2600?play_session_id=play-abc-123&work_id=88", nil)
	req.Header.Set("Range", "bytes=0-")
	req.Header.Set("Referer", "http://office.app.5ddd.com:35125/creator/88")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(logDir, accessLogFileName))
	if err != nil {
		t.Fatal(err)
	}
	logs := string(data)
	assertContains(t, logs, `"GET /api/media/2600?play_session_id=REDACTED&work_id=88 HTTP/1.1" 206 5`)
	assertContains(t, logs, `media_id=2600`)
	assertContains(t, logs, `media_type=video`)
	assertContains(t, logs, `play_session_id=play-abc-123`)
	assertContains(t, logs, `work_id=88`)
	assertContains(t, logs, `client_entry=fnos_app_entry`)
	assertContains(t, logs, `range="bytes=0-"`)
	assertContains(t, logs, `range_start=0`)
	assertContains(t, logs, `range_end=-`)
	assertContains(t, logs, `file_size=123456`)
	assertContains(t, logs, `open_ms=2.000`)
	assertContains(t, logs, `stat_ms=1.000`)
	assertContains(t, logs, `write_err="-"`)
	assertHasRequestTime(t, logs)
	assertHasFirstByteTime(t, logs)
}

func assertContains(t *testing.T, haystack string, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected %q in:\n%s", needle, haystack)
	}
}

func assertHasRequestTime(t *testing.T, logs string) {
	t.Helper()
	if !regexp.MustCompile(`\brt=\d+\.\d{3}\b`).MatchString(logs) {
		t.Fatalf("expected nginx-style request time in:\n%s", logs)
	}
}

func assertHasFirstByteTime(t *testing.T, logs string) {
	t.Helper()
	if !regexp.MustCompile(`\bttfb_ms=\d+\.\d{3}\b`).MatchString(logs) {
		t.Fatalf("expected first body write time in:\n%s", logs)
	}
}
