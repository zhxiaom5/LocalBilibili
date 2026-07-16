package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSubtitleManagerLocalCLIUpsertsRecords(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(creatorDir, "2026-06-22_作品_1001.mp4")
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: videoPath, FileName: filepath.Base(videoPath)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cli := fakeSubtitleCLI(t)
	manager := NewSubtitleManager(store, NewEventHub(), SubtitleRuntimeConfig{
		Mode:        "local",
		Concurrency: 1,
		CLIPath:     cli,
		Language:    "zh",
		GPU:         false,
	})
	defer manager.Close()
	if _, err := manager.EnqueueImmediate([]int64{creator.ID}, "manual"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return !manager.Status().Running && manager.Status().PendingCreators == 0 })
	status := manager.Status()
	if status.SucceededCreators != 1 || status.SubtitleWorks != 1 || status.FailedCreators != 0 {
		t.Fatalf("subtitle status = %#v", status)
	}
	title, text, ready, err := store.SubtitleTextForWork(1, mustSubtitleTokenForWork(t, store, 1))
	if err != nil {
		t.Fatal(err)
	}
	if title != "作品" || !ready || text == "" {
		t.Fatalf("subtitle text not ready: title=%q ready=%v text=%q", title, ready, text)
	}
	creators, err := store.Creators()
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 || creators[0].SubtitleCount != 1 {
		t.Fatalf("subtitle count not aggregated: %#v", creators)
	}
}

func TestSubtitleTokenForCreatorCreatesPendingRecord(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(creatorDir, "2026-06-22_作品_1001.mp4")
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: videoPath, FileName: filepath.Base(videoPath)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.SubtitleTokenForCreator(creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected token")
	}
	text, ready, err := store.SubtitleTextForCreator(creator.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	if ready || text != "" {
		t.Fatalf("new public link should be pending, got ready=%v text=%q", ready, text)
	}
}

func TestSubtitleTextForCreatorPrefixesChaptersWithOriginalDate(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoOne := filepath.Join(creatorDir, "2026-07-10_入库文件A_1001.mp4")
	videoTwo := filepath.Join(creatorDir, "2026-07-10_入库文件B_1002.mp4")
	if err := os.WriteFile(videoOne, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoTwo, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	textOne := filepath.Join(creatorDir, "2026-07-10_入库文件A_1001.txt")
	textTwo := filepath.Join(creatorDir, "2026-07-10_入库文件B_1002.txt")
	if err := os.WriteFile(textOne, []byte("字幕A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(textTwo, []byte("字幕B"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{
		{
			AwemeID:     "1001",
			Title:       "作品A",
			PublishedAt: "2024-05-06T07:08:09Z",
			Media:       []MediaInput{{Type: "video", FilePath: videoOne, FileName: filepath.Base(videoOne)}},
		},
		{
			AwemeID:     "1002",
			Title:       "作品B",
			PublishedAt: "2024-05-07T07:08:09Z",
			Media:       []MediaInput{{Type: "video", FilePath: videoTwo, FileName: filepath.Base(videoTwo)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	works, err := store.FeedWithOptions(FeedOptions{CreatorID: creator.ID, Order: "asc", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 2 {
		t.Fatalf("works len = %d", len(works))
	}
	for _, work := range works {
		var textPath string
		switch work.AwemeID {
		case "1001":
			textPath = textOne
		case "1002":
			textPath = textTwo
		default:
			t.Fatalf("unexpected work = %#v", work)
		}
		if err := store.UpsertSubtitleRecord(work.ID, 0, creator.ID, textPath, "", "", "test", "ready", ""); err != nil {
			t.Fatal(err)
		}
	}
	token, err := store.SubtitleTokenForCreator(creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	text, ready, err := store.SubtitleTextForCreator(creator.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("expected creator subtitles ready")
	}
	if !strings.Contains(text, "## 2024-05-06 作品A\n\n字幕A") || !strings.Contains(text, "## 2024-05-07 作品B\n\n字幕B") {
		t.Fatalf("creator subtitle text missing original dates: %q", text)
	}
	if strings.Index(text, "## 2024-05-07 作品B") > strings.Index(text, "## 2024-05-06 作品A") {
		t.Fatalf("creator subtitles should be ordered by original date desc: %q", text)
	}
	if strings.Contains(text, "2026-07-10 作品") {
		t.Fatalf("creator subtitle text used import/file date instead of original published_at: %q", text)
	}
}

func TestSubtitleManagerScanModeSkipsMissingAndUpsertsExisting(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoOne := filepath.Join(creatorDir, "2026-06-22_作品A_1001.mp4")
	videoTwo := filepath.Join(creatorDir, "2026-06-22_作品B_1002.mp4")
	if err := os.WriteFile(videoOne, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(videoTwo, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(creatorDir, "2026-06-22_作品A_1001.txt"), []byte("existing subtitle"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{
		{
			AwemeID: "1001",
			Title:   "作品A",
			Media:   []MediaInput{{Type: "video", FilePath: videoOne, FileName: filepath.Base(videoOne)}},
		},
		{
			AwemeID: "1002",
			Title:   "作品B",
			Media:   []MediaInput{{Type: "video", FilePath: videoTwo, FileName: filepath.Base(videoTwo)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewSubtitleManager(store, NewEventHub(), SubtitleRuntimeConfig{Mode: "scan", Concurrency: 1})
	defer manager.Close()
	if _, err := manager.EnqueueImmediate([]int64{creator.ID}, "scan"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return !manager.Status().Running && manager.Status().PendingCreators == 0 })
	status := manager.Status()
	if status.SucceededCreators != 1 || status.SubtitleWorks != 1 || status.FailedCreators != 0 {
		t.Fatalf("subtitle status = %#v", status)
	}
	_, text, ready, err := store.SubtitleTextForWork(1, mustSubtitleTokenForWork(t, store, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !ready || strings.TrimSpace(text) != "existing subtitle" {
		t.Fatalf("expected existing subtitle ready, ready=%v text=%q", ready, text)
	}
}

func TestSubtitleScanExistingEndpointEnqueuesVideoCreators(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(creatorDir, "2026-06-22_作品_1001.mp4")
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: videoPath, FileName: filepath.Base(videoPath)}},
	}}); err != nil {
		t.Fatal(err)
	}
	app := mustTestApp(t, store)
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")
	saveBody := strings.NewReader(`{"mode":"scan","concurrency":1,"language":"zh","gpu":false}`)
	assertStatus(t, client, http.MethodPut, server.URL+"/api/subtitles/config", saveBody, http.StatusOK)
	var response struct {
		CreatorCount int `json:"creator_count"`
	}
	decodeJSONResponse(t, client, http.MethodPost, server.URL+"/api/subtitles/scan-existing", nil, http.StatusAccepted, &response)
	if response.CreatorCount != 1 {
		t.Fatalf("creator_count = %d, want 1", response.CreatorCount)
	}
}

func fakeSubtitleCLI(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "subtitle-cli")
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "check" ]; then
  echo "fake subtitle cli ok"
  exit 0
fi
video=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --video) shift; video="$1" ;;
  esac
  shift || true
done
stem="${video%.*}"
printf 'hello subtitle\n' > "$stem.txt"
printf '1\n00:00:00,000 --> 00:00:01,000\nhello subtitle\n' > "$stem.srt"
printf '{"text":"hello subtitle"}\n' > "$stem.json"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustSubtitleTokenForWork(t *testing.T, store *Store, workID int64) string {
	t.Helper()
	token, err := store.SubtitleTokenForWork(workID)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
