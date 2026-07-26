package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWiseMappingXMLEscapesUnsafeText(t *testing.T) {
	xml, err := wiseMappingXML(MindNode{Title: "A < B & C", Children: []MindNode{{Title: "quoted \"node\""}}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(xml)
	if !strings.Contains(text, `<map name="LocalBilibili" version="tango" theme="prism" layout="mindmap">`) || !strings.Contains(text, `central="true"`) || !strings.Contains(text, `position="260,0"`) || !strings.Contains(text, `text="A &lt; B &amp; C"`) || !strings.Contains(text, "quoted &#34;node&#34;") {
		t.Fatalf("XML was not escaped: %s", text)
	}
}

func TestLearningPasswordUsesPrivateFile(t *testing.T) {
	t.Setenv("LOCALBILIBILI_LEARNING_SECRET_DIR", t.TempDir())
	if err := saveLearningPassword("secret-value"); err != nil {
		t.Fatal(err)
	}
	path := learningSecretPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("password permissions = %o", info.Mode().Perm())
	}
	if filepath.Dir(path) == "" {
		t.Fatal("missing private secret path")
	}
	password, err := learningWiseMappingPassword()
	if err != nil || password != "secret-value" {
		t.Fatalf("password = %q, err = %v", password, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := learningWiseMappingPassword(); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("existing password permissions = %o", info.Mode().Perm())
	}
}

func TestFnOSLifecycleStoresAppConfigInPackageVar(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "packaging", "fnos", "localbilibili", "cmd", "main"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `LOCALBILIBILI_CONFIG="$VAR_DIR/localbilibili.config.json"`) {
		t.Fatal("fnOS lifecycle must keep app configuration in TRIM_PKGVAR")
	}
}

func TestParseCodexDeviceOutput(t *testing.T) {
	url, code := parseCodexDeviceOutput("Open https://auth.openai.com/codex/device then enter ABCD-1234")
	if url != "https://auth.openai.com/codex/device" || code != "ABCD-1234" {
		t.Fatalf("url=%q code=%q", url, code)
	}
}

func TestParseCodexMindNodeAcceptsCommonJSONWrappers(t *testing.T) {
	want := "投资框架"
	for _, raw := range []string{
		`{"title":"投资框架","children":[{"title":"风险"}]}`,
		"```json\n{\"title\":\"投资框架\",\"children\":[{\"title\":\"风险\"}]}\n```",
		"以下是导图：\n{\"title\":\"投资框架\",\"children\":[{\"title\":\"风险\"}]}\n",
	} {
		node, err := parseCodexMindNode([]byte(raw))
		if err != nil || node.Title != want || len(node.Children) != 1 {
			t.Fatalf("parse %q => %#v, %v", raw, node, err)
		}
	}
}

func TestParseCodexMindNodeRejectsUntitledDescendants(t *testing.T) {
	for _, raw := range []string{
		`{"title":"投资框架","children":[{"title":"","children":[{"title":"风险"}]}]}`,
		`{"title":"投资框架","children":[{"text":"风险"}]}`,
		`{"title":"投资框架","children":[{}]}`,
	} {
		if _, err := parseCodexMindNode([]byte(raw)); err == nil {
			t.Fatalf("expected invalid descendant error for %s", raw)
		}
	}
}

func TestCreatorMapFallbackNodePreservesEveryWorkMindMap(t *testing.T) {
	records := []MindMapRecord{
		{WorkID: 101, Title: "测试UP主 - 宏观入门", SummaryJSON: `{"title":"宏观框架","children":[{"title":"利率"}]}`},
		{WorkID: 102, Title: "测试UP主 - 投资方法", SummaryJSON: `{"title":"投资框架","children":[{"title":"估值"}]}`},
	}
	node, err := creatorMapFallbackNode("测试UP主", records)
	if err != nil {
		t.Fatal(err)
	}
	if node.Title != "测试UP主 - 学习思维导图" || node.Category != "学习总结" || len(node.Children) != 2 {
		t.Fatalf("unexpected aggregate root: %#v", node)
	}
	if node.Children[0].Title != "宏观入门" || len(node.Children[0].Children) != 1 || node.Children[0].Children[0].Title != "宏观框架" {
		t.Fatalf("first work was not preserved: %#v", node.Children[0])
	}
	if node.Children[1].Title != "投资方法" || node.Children[1].Children[0].Title != "投资框架" {
		t.Fatalf("second work was not preserved: %#v", node.Children[1])
	}
}

func TestCreatorMapFallbackNodeFitsWiseMappingNodeLimitWithoutDroppingWorks(t *testing.T) {
	const works = 152
	records := make([]MindMapRecord, 0, works)
	for i := 0; i < works; i++ {
		children := make([]MindNode, 0, 80)
		for j := 0; j < 80; j++ {
			children = append(children, MindNode{Title: fmt.Sprintf("要点-%d", j), Children: []MindNode{{Title: "细节"}}})
		}
		outline, err := json.Marshal(MindNode{Title: fmt.Sprintf("作品根-%d", i), Children: children})
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, MindMapRecord{WorkID: int64(i + 1), Title: fmt.Sprintf("测试UP主 - 作品-%03d", i), SummaryJSON: string(outline)})
	}
	node, err := creatorMapFallbackNode("测试UP主", records)
	if err != nil {
		t.Fatal(err)
	}
	if len(node.Children) != works {
		t.Fatalf("works retained=%d want=%d", len(node.Children), works)
	}
	if got := mindNodeCount(node); got > maxCreatorMapNodes {
		t.Fatalf("node count=%d exceeds WiseMapping-safe budget=%d", got, maxCreatorMapNodes)
	}
	xmlDoc, err := wiseMappingXML(node)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(xmlDoc, []byte("<topic")); got > maxCreatorMapNodes {
		t.Fatalf("XML topics=%d exceeds WiseMapping-safe budget=%d", got, maxCreatorMapNodes)
	}
}

func TestCreatorMapWithinNodeBudgetRejectsLegacyOversizedAggregate(t *testing.T) {
	root := MindNode{Title: "旧总图"}
	for i := 0; i < maxCreatorMapNodes; i++ {
		root.Children = append(root.Children, MindNode{Title: fmt.Sprintf("作品-%d", i)})
	}
	if creatorMapWithinNodeBudget(root) {
		t.Fatalf("oversized legacy aggregate was accepted: nodes=%d", mindNodeCount(root))
	}
	if !creatorMapWithinNodeBudget(MindNode{Title: "安全总图", Children: []MindNode{{Title: "作品"}}}) {
		t.Fatal("small aggregate was rejected")
	}
}

func TestCodexPassesFullPromptThroughStdin(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-codex")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
[ "$1" = "exec" ] && [ "$2" = "--skip-git-repo-check" ] && [ "$3" = "-" ] || exit 11
grep -q "__all_creator_subtitles__" || exit 12
printf '%s' '{"title":"完整UP主导图","children":[]}'
`), 0o700); err != nil {
		t.Fatal(err)
	}
	m := &LearningManager{cfg: LearningRuntimeConfig{CodexPath: script}}
	input := "__all_creator_subtitles__" + strings.Repeat("字幕内容", 128*1024)
	node, err := m.codex(context.Background(), input, creatorMindMapInstruction)
	if err != nil || node.Title != "完整UP主导图" {
		t.Fatalf("node=%#v err=%v", node, err)
	}
}

func TestCodexUsesSavedPromptAndAppendsInputWithoutPlaceholder(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-codex")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
input=$(cat)
printf '%s' "$input" | grep -q "__saved_creator_prompt__" || exit 12
printf '%s' "$input" | grep -q "__all_creator_subtitles__" || exit 13
printf '%s' '{"title":"自定义提示词导图","children":[]}'
`), 0o700); err != nil {
		t.Fatal(err)
	}
	m := &LearningManager{cfg: LearningRuntimeConfig{CodexPath: script}}
	node, err := m.codex(context.Background(), "__all_creator_subtitles__", "__saved_creator_prompt__")
	if err != nil || node.Title != "自定义提示词导图" {
		t.Fatalf("node=%#v err=%v", node, err)
	}
}

func TestCodexReturnsSanitizedStderrOnExecutionFailure(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-codex")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
echo 'authentication expired; run codex login again' >&2
exit 23
`), 0o700); err != nil {
		t.Fatal(err)
	}
	m := &LearningManager{cfg: LearningRuntimeConfig{CodexPath: script}}
	_, err := m.codex(context.Background(), "private subtitle input", workMindMapInstruction)
	if err == nil || !strings.Contains(err.Error(), "authentication expired; run codex login again") || !strings.Contains(err.Error(), "exit status 23") {
		t.Fatalf("unexpected Codex failure: %v", err)
	}
}

func TestLearningPromptsNormalizeAndAreSafeToReturn(t *testing.T) {
	defaults := DefaultLearningRuntimeConfig()
	config := learningConfigFromRuntime(LearningRuntimeConfig{WorkPrompt: "  单作品 XX  ", CreatorPrompt: "  UP主 XX  "})
	if config.WorkPrompt != "单作品 XX" || config.CreatorPrompt != "UP主 XX" {
		t.Fatalf("prompts=%#v", config)
	}
	if defaults.WorkPrompt == "" || defaults.CreatorPrompt == "" {
		t.Fatalf("missing prompt defaults: %#v", defaults)
	}
}

func TestLearningMigrationCreatesDefaultQueue(t *testing.T) {
	store, err := OpenStore(t.TempDir() + "/learning.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	queues, err := store.LearningQueues()
	if err != nil {
		t.Fatal(err)
	}
	if len(queues) != 1 || queues[0].Name != "默认学习队列" {
		t.Fatalf("unexpected queues: %#v", queues)
	}
}

func TestLearningRunRecordsQueueProgressAndTimes(t *testing.T) {
	store, err := OpenStore(t.TempDir() + "/learning.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(directory.ID, "队列UP主", nil)
	if err != nil {
		t.Fatal(err)
	}
	queues, err := store.LearningQueues()
	if err != nil || len(queues) == 0 {
		t.Fatalf("queues=%#v err=%v", queues, err)
	}
	run, err := store.CreateLearningRun(queues[0], 1)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.AddLearningJobWithRun(creator.ID, 0, queues[0].ID, run.ID, "schedule")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkLearningRunning(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishLearningJob(job.ID, "succeeded", "完成", 1, 1); err != nil {
		t.Fatal(err)
	}
	got, err := store.LearningRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "completed" || got.TotalJobs != 1 || got.Succeeded != 1 || got.Failed != 0 || got.StartedAt == "" || got.FinishedAt == "" {
		t.Fatalf("run=%#v", got)
	}
}

func TestLearningRestartRecoversInterruptedJob(t *testing.T) {
	store, err := OpenStore(t.TempDir() + "/learning.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(directory.ID, "重启恢复UP主", nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.AddLearningJob(creator.ID, 0, 0, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkLearningRunning(job.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.RecoverInterruptedLearningJobs()
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	got, err := store.LearningJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending" || got.StartedAt != "" || got.FinishedAt != "" || got.ProgressMessage != "应用重启后等待恢复" {
		t.Fatalf("recovered job=%#v", got)
	}
}

func TestNewLearningManagerResumesInterruptedJobAfterRestart(t *testing.T) {
	store, err := OpenStore(t.TempDir() + "/learning.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(directory.ID, "启动恢复UP主", nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.AddLearningJob(creator.ID, 0, 0, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkLearningRunning(job.ID); err != nil {
		t.Fatal(err)
	}
	m := NewLearningManager(store, nil, LearningRuntimeConfig{})
	defer m.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.LearningJob(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == "failed" && strings.Contains(got.Error, "缺少字幕") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := store.LearningJob(job.ID)
	t.Fatalf("recovered manager did not run job: %#v", got)
}

func TestCreatorLearningScopeAllowsAggregateJobAndMap(t *testing.T) {
	store, err := OpenStore(t.TempDir() + "/learning.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(directory.ID, "aggregate creator", nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.AddLearningJob(creator.ID, 0, 0, "manual")
	if err != nil {
		t.Fatalf("create creator job: %v", err)
	}
	if job.WorkID != 0 {
		t.Fatalf("job work id = %d, want creator scope 0", job.WorkID)
	}
	record := MindMapRecord{CreatorID: creator.ID, WorkID: 0, MapID: "creator-map", MapURL: "http://wise/maps/creator-map", Title: "aggregate creator - 学习思维导图", ContentFingerprint: "fingerprint", SyncStatus: "synced"}
	if err := store.UpsertMindMap(record, `{"title":"aggregate"}`); err != nil {
		t.Fatalf("upsert creator map: %v", err)
	}
	mapRecord, err := store.MindMap(creator.ID, 0)
	if err != nil {
		t.Fatalf("read creator map: %v", err)
	}
	if mapRecord.WorkID != 0 || mapRecord.MapID != record.MapID {
		t.Fatalf("aggregate map = %#v", mapRecord)
	}
}

func TestCreatorLearningInputIncludesAllReadySubtitlesAsOneDocument(t *testing.T) {
	store, err := OpenStore(t.TempDir() + "/learning.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(directory.ID, "batch creator", []WorkInput{
		{AwemeID: "one", Title: "one", PublishedAt: "2026-01-02", Media: []MediaInput{{Type: "video", FilePath: "one.mp4", FileName: "one.mp4"}}},
		{AwemeID: "two", Title: "two", PublishedAt: "2026-01-01", Media: []MediaInput{{Type: "video", FilePath: "two.mp4", FileName: "two.mp4"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	videos, err := store.VideoWorksForCreator(creator.ID, 0)
	if err != nil || len(videos) != 2 {
		t.Fatalf("videos=%#v err=%v", videos, err)
	}
	for _, video := range videos {
		path := filepath.Join(t.TempDir(), strconv.FormatInt(video.WorkID, 10)+".txt")
		if err := os.WriteFile(path, []byte("subtitle"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertSubtitleRecord(video.WorkID, video.MediaID, creator.ID, path, "", "", "test", "ready", ""); err != nil {
			t.Fatal(err)
		}
	}
	input, count, err := store.LearningCreatorSubtitles(creator.ID)
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	for _, want := range []string{"作品 ", "one", "two", "subtitle"} {
		if !strings.Contains(input, want) {
			t.Fatalf("full creator input missing %q: %s", want, input)
		}
	}
	job, err := store.AddLearningJob(creator.ID, 0, 0, "manual")
	if err != nil || job.WorkID != 0 {
		t.Fatalf("creator job=%#v err=%v", job, err)
	}
	jobs, err := store.LearningJobs(10)
	if err != nil || len(jobs) != 1 || jobs[0].WorkID != 0 {
		t.Fatalf("jobs=%#v err=%v", jobs, err)
	}
}

func TestLearningMigrationConvertsLegacyCreatorScope(t *testing.T) {
	store, err := OpenStore(t.TempDir() + "/learning.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(directory.ID, "legacy aggregate creator", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`PRAGMA foreign_keys=OFF; DROP TABLE mind_map_records; DROP TABLE learning_jobs;
CREATE TABLE learning_jobs (id INTEGER PRIMARY KEY AUTOINCREMENT, creator_id INTEGER NOT NULL, work_id INTEGER NOT NULL DEFAULT 0, queue_id INTEGER NOT NULL DEFAULT 0, run_id INTEGER NOT NULL DEFAULT 0, retry_of_job_id INTEGER NOT NULL DEFAULT 0, attempt INTEGER NOT NULL DEFAULT 1, source TEXT NOT NULL DEFAULT 'manual', status TEXT NOT NULL DEFAULT 'pending', total_works INTEGER NOT NULL DEFAULT 0, processed_works INTEGER NOT NULL DEFAULT 0, progress_message TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, started_at TEXT NOT NULL DEFAULT '', finished_at TEXT NOT NULL DEFAULT '', FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE, FOREIGN KEY(work_id) REFERENCES works(id) ON DELETE CASCADE);
CREATE TABLE mind_map_records (id INTEGER PRIMARY KEY AUTOINCREMENT, creator_id INTEGER NOT NULL, work_id INTEGER NOT NULL DEFAULT 0, map_id TEXT NOT NULL DEFAULT '', map_url TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', summary_json TEXT NOT NULL DEFAULT '', content_fingerprint TEXT NOT NULL DEFAULT '', prompt_version TEXT NOT NULL DEFAULT 'v1', sync_status TEXT NOT NULL DEFAULT 'pending', updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(creator_id,work_id), FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE, FOREIGN KEY(work_id) REFERENCES works(id) ON DELETE CASCADE);
INSERT INTO learning_jobs(creator_id,work_id) VALUES(` + strconv.FormatInt(creator.ID, 10) + `,0);
INSERT INTO mind_map_records(creator_id,work_id,map_id) VALUES(` + strconv.FormatInt(creator.ID, 10) + `,0,'legacy-map'); PRAGMA foreign_keys=ON;`); err != nil {
		t.Fatal(err)
	}
	if err := store.migrateLearning(); err != nil {
		t.Fatal(err)
	}
	job, err := store.LearningJob(1)
	if err != nil || job.WorkID != 0 {
		t.Fatalf("legacy job = %#v, err=%v", job, err)
	}
	mapRecord, err := store.MindMap(creator.ID, 0)
	if err != nil || mapRecord.MapID != "legacy-map" {
		t.Fatalf("legacy map = %#v, err=%v", mapRecord, err)
	}
}

func TestLearningConfigDoesNotContainSecret(t *testing.T) {
	cfg := learningConfigFromRuntime(LearningRuntimeConfig{WiseMappingURL: "http://wise", WiseMappingUser: "ted", DefaultCron: "0 4 * * *"})
	if cfg.WiseMappingUser != "ted" || cfg.PasswordConfigured {
		t.Fatalf("unexpected public config: %#v", cfg)
	}
}

func TestLearningConfigResponseNeverReturnsPassword(t *testing.T) {
	t.Setenv("LOCALBILIBILI_LEARNING_SECRET_DIR", t.TempDir())
	if err := saveLearningPassword("do-not-return"); err != nil {
		t.Fatal(err)
	}
	config := learningConfigFromRuntime(LearningRuntimeConfig{WiseMappingUser: "ted", DefaultCron: "0 4 * * *"})
	if config.WiseMappingPassword != "" || !config.PasswordConfigured {
		t.Fatalf("unsafe config response: %#v", config)
	}
}

func TestWiseMappingTokenUsesEmailAndAcceptsPlainTextJWT(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/restful/authenticate" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["email"] != "person@example.com" || body["password"] != "correct-password" {
			t.Fatalf("unexpected auth body: %#v", body)
		}
		if _, found := body["username"]; found {
			t.Fatalf("authentication body must not use username: %#v", body)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("jwt-from-wisemapping"))
	}))
	defer server.Close()

	token, err := wiseMappingToken(context.Background(), LearningRuntimeConfig{WiseMappingURL: server.URL, WiseMappingUser: "person@example.com"}, "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if token != "jwt-from-wisemapping" {
		t.Fatalf("token = %q", token)
	}
}

func TestWiseMappingCreateUsesCurrentFrontendProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/restful/maps" || r.URL.Query().Get("title") != "作品导图" || r.URL.Query().Get("description") != "" || r.URL.Query().Get("theme") != "prism" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(body)) != "" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("create body/content type = %q/%q", body, r.Header.Get("Content-Type"))
		}
		w.Header().Set("resourceid", "123")
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	id, err := wiseMappingCreate(context.Background(), LearningRuntimeConfig{WiseMappingURL: server.URL}, "token", "作品导图")
	if err != nil || id != "123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestWiseMappingCreateReturnsBoundedServerReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("database title validation failed"))
	}))
	defer server.Close()
	_, err := wiseMappingCreate(context.Background(), LearningRuntimeConfig{WiseMappingURL: server.URL}, "token", "作品导图")
	if err == nil || !strings.Contains(err.Error(), "database title validation failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestWiseMappingTitleTruncatesRunesForInstanceLimit(t *testing.T) {
	long := strings.Repeat("长", 300)
	got := wiseMappingTitle(long)
	if len([]rune(got)) != 240 || !strings.HasSuffix(got, "…") {
		t.Fatalf("title length/value = %d/%q", len([]rune(got)), got[len(got)-3:])
	}
}

func TestLearningWorkMapTitleUsesReadableOriginalTitle(t *testing.T) {
	got := learningWorkMapTitle("熊猫的出海攻略", "胡润最新报告：人均资产6100万\n这里是作品描述，不应进入导图名称")
	if got != "熊猫的出海攻略 - 胡润最新报告：人均资产6100万" {
		t.Fatalf("title=%q", got)
	}
	long := learningWorkMapTitle("UP主", strings.Repeat("长", 80))
	if !strings.HasSuffix(long, "…") || len([]rune(strings.TrimPrefix(long, "UP主 - "))) != 72 {
		t.Fatalf("truncated title=%q", long)
	}
}

func TestWiseMappingEditorURLUsesCurrentInstanceRoute(t *testing.T) {
	if got, want := wiseMappingEditorURL("http://wise/", "123"), "http://wise/c/maps/123/edit"; got != want {
		t.Fatalf("url=%q want=%q", got, want)
	}
}

func TestWiseMappingTitleFallsBackToGenericPropertiesRoute(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			if r.Method != http.MethodPut || r.URL.Path != "/api/restful/maps/123/title" || r.Header.Get("Content-Type") != "text/plain" {
				t.Fatalf("dedicated title request = %s %s %q", r.Method, r.URL.String(), r.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusForbidden)
		case 2:
			if r.Method != http.MethodPut || r.URL.Path != "/api/restful/maps/123" || r.URL.Query().Get("minor") != "true" {
				t.Fatalf("fallback request = %s %s", r.Method, r.URL.String())
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["title"] != "短标题" {
				t.Fatalf("fallback body=%#v err=%v", body, err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %d", calls)
		}
	}))
	defer server.Close()
	if err := wiseMappingUpdateTitle(context.Background(), LearningRuntimeConfig{WiseMappingURL: server.URL}, "token", "123", "短标题"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestLearningSyncRecreatesMapDeletedFromWiseMapping(t *testing.T) {
	t.Setenv("LOCALBILIBILI_LEARNING_SECRET_DIR", t.TempDir())
	if err := saveLearningPassword("secret"); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(t.TempDir() + "/learning.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(directory.ID, "恢复测试UP主", nil)
	if err != nil {
		t.Fatal(err)
	}
	source := "未变化的字幕"
	node := MindNode{Title: "恢复后的导图", Children: []MindNode{{Title: "分支"}}}
	if err := store.UpsertMindMap(MindMapRecord{CreatorID: creator.ID, MapID: "deleted-map", MapURL: "http://old/c/maps/deleted-map/edit", Title: "恢复测试UP主 - 学习思维导图", ContentFingerprint: learningContentFingerprint(source), SyncStatus: "synced"}, string(mustJSON(node))); err != nil {
		t.Fatal(err)
	}
	created, updated, nextLabelID := 0, 0, int64(1)
	labels := []wiseMappingLabelInfo{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/restful/authenticate":
			_, _ = w.Write([]byte("token"))
		case r.URL.Path == "/api/restful/maps/deleted-map/document":
			w.WriteHeader(http.StatusForbidden) // This is how the deployed instance masks a deleted private map.
		case r.Method == http.MethodGet && r.URL.Path == "/api/restful/maps/":
			if created == 0 {
				_, _ = w.Write([]byte(`{"mindmapsInfo":[]}`))
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"mindmapsInfo": []map[string]any{{"id": 0, "title": "replacement-map", "labels": labels}}})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/restful/maps":
			created++
			w.Header().Set("resourceid", "replacement-map")
			w.WriteHeader(http.StatusCreated)
		case r.URL.Path == "/api/restful/maps/replacement-map/document":
			updated++
			if r.Method != http.MethodPut {
				t.Errorf("document method = %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/restful/labels/":
			_ = json.NewEncoder(w).Encode(map[string]any{"labels": labels})
		case r.Method == http.MethodGet && r.URL.Path == "/api/restful/folders":
			// Older WiseMapping deployments have no real-folder API. The learning
			// sync must preserve the existing map and label behaviour there.
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/restful/labels":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			labels = append(labels, wiseMappingLabelInfo{ID: nextLabelID, Title: body["title"]})
			w.Header().Set("resourceid", strconv.FormatInt(nextLabelID, 10))
			nextLabelID++
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost && r.URL.Path == "/api/restful/maps/replacement-map/labels":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	m := &LearningManager{store: store, cfg: LearningRuntimeConfig{WiseMappingURL: server.URL, WiseMappingUser: "person@example.com"}}
	if err := m.sync(context.Background(), creator.ID, 0, creator.Name, "恢复测试UP主 - 学习思维导图", node, source); err != nil {
		t.Fatal(err)
	}
	if created != 1 || updated != 1 {
		t.Fatalf("created=%d updated=%d, want 1/1", created, updated)
	}
	record, err := store.MindMap(creator.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if record.MapID != "replacement-map" || record.SyncStatus != "synced" || record.SummaryJSON == "" {
		t.Fatalf("recovered record = %#v", record)
	}
}

func TestWiseMappingSyncLabelsUsesDeployedFrontendProtocol(t *testing.T) {
	labels := []wiseMappingLabelInfo{
		{ID: 1, Title: "UP主名-测试UP主", Color: "#4F8EF7"},
		{ID: 2, Title: "内容分类-股市投资", Color: "#4F8EF7"},
	}
	attached := map[int64]bool{1: true, 2: true}
	created, linked, removed := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/restful/labels/":
			_ = json.NewEncoder(w).Encode(map[string]any{"labels": labels})
		case r.Method == http.MethodPost && r.URL.Path == "/api/restful/labels":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["color"] != "#4F8EF7" || body["iconName"] != "smile" || body["title"] == "" {
				t.Fatalf("label request = %#v", body)
			}
			created++
			labels = append(labels, wiseMappingLabelInfo{ID: int64(len(labels) + 1), Title: body["title"]})
			w.Header().Set("resourceid", strconv.Itoa(len(labels)))
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/api/restful/maps/":
			mapLabels := make([]wiseMappingLabelInfo, 0, len(attached))
			for _, label := range labels {
				if attached[label.ID] {
					mapLabels = append(mapLabels, label)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"mindmapsInfo": []map[string]any{{"id": 123, "title": "作品导图", "labels": mapLabels}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/restful/maps/123/labels":
			var id int64
			if err := json.NewDecoder(r.Body).Decode(&id); err != nil {
				t.Fatal(err)
			}
			attached[id] = true
			linked++
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/restful/maps/123/labels/"):
			id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/restful/maps/123/labels/"), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			delete(attached, id)
			removed++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	cfg := LearningRuntimeConfig{WiseMappingURL: server.URL}
	want := []string{"港股", "资产配置"}
	if err := wiseMappingSyncLabels(context.Background(), cfg, "token", "123", want); err != nil {
		t.Fatal(err)
	}
	if created != 2 || linked != 2 || removed != 2 {
		t.Fatalf("created=%d linked=%d removed=%d, want 2/2/2", created, linked, removed)
	}
	if err := wiseMappingSyncLabels(context.Background(), cfg, "token", "123", want); err != nil {
		t.Fatal(err)
	}
	if created != 2 || linked != 2 || removed != 2 {
		t.Fatalf("repeat must be stable: created=%d linked=%d removed=%d", created, linked, removed)
	}
}

func TestWiseMappingSyncFolderCreatesTwoLevelsAndMovesMap(t *testing.T) {
	folders := []wiseMappingFolderInfo{{ID: 8, Name: "LocalDouyin"}}
	var moved struct {
		MapIDs   []int64 `json:"mapIds"`
		FolderID *int64  `json:"folderId"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/restful/folders":
			_ = json.NewEncoder(w).Encode(folders)
		case r.Method == http.MethodPost && r.URL.Path == "/api/restful/folders":
			var request struct {
				Name     string `json:"name"`
				ParentID *int64 `json:"parentId"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Name != "小Lin说" || request.ParentID == nil || *request.ParentID != 8 {
				t.Fatalf("folder request = %#v", request)
			}
			child := wiseMappingFolderInfo{ID: 9, Name: request.Name, ParentID: request.ParentID}
			folders = append(folders, child)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(child)
		case r.Method == http.MethodPut && r.URL.Path == "/api/restful/maps/folder":
			if err := json.NewDecoder(r.Body).Decode(&moved); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	if err := wiseMappingSyncFolder(context.Background(), LearningRuntimeConfig{WiseMappingURL: server.URL}, "token", "123", "LocalDouyin", "小Lin说"); err != nil {
		t.Fatal(err)
	}
	if len(moved.MapIDs) != 1 || moved.MapIDs[0] != 123 || moved.FolderID == nil || *moved.FolderID != 9 {
		t.Fatalf("move request = %#v", moved)
	}
}

func TestWiseMappingSyncFolderTreatsMissingFolderAPIAsCompatible(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	err := wiseMappingSyncFolder(context.Background(), LearningRuntimeConfig{WiseMappingURL: server.URL}, "token", "123", "LocalDouyin", "小Lin说")
	if !errors.Is(err, errWiseMappingFoldersUnsupported) {
		t.Fatalf("err=%v, want unsupported folder API", err)
	}
}

func TestLearningMapLabelsIncludesRequiredAndAIThemeTags(t *testing.T) {
	node := MindNode{Title: "投资框架", Category: "股市投资", Tags: []string{
		"港股",
		"资产配置",
		"港股",
		"UP主名-不应覆盖",
		"长期投资",
		"估值",
		"站点名称-不应覆盖",
		"内容分类-不应覆盖",
	}}
	got := learningMapLabels(node)
	want := []string{"港股", "资产配置", "长期投资"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("labels=%#v want=%#v", got, want)
	}
	if len(got) != 3 {
		t.Fatalf("labels count=%d want=3", len(got))
	}
}

func TestWiseMappingUpdateUsesCurrentFrontendProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/restful/maps/123/document" || r.URL.Query().Get("minor") != "false" {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		var body struct{ ID, XML, Properties string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ID != "123" || body.XML != "<map/>" || body.Properties != "{}" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("body/header = %#v/%q", body, r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := wiseMappingUpdate(context.Background(), LearningRuntimeConfig{WiseMappingURL: server.URL}, "token", "123", []byte("<map/>")); err != nil {
		t.Fatal(err)
	}
}

func TestWiseMappingFindMapIDByTitle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/restful/maps/" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"mindmapsInfo": []map[string]any{{"id": 456, "title": "作品导图"}}})
	}))
	defer server.Close()
	id, err := wiseMappingFindMapID(context.Background(), LearningRuntimeConfig{WiseMappingURL: server.URL}, "token", "作品导图")
	if err != nil || id != "456" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}
