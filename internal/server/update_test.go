package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanExtractsCreatorOnlineIdentity(t *testing.T) {
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"aweme_id":"1001","desc":"作品","author":{"uid":"75233440081","sec_uid":"MS4wLjABAAAA_TEST","share_info":{"share_url":"https://www.bilibili.com/user/MS4wLjABAAAA_TEST"}}}`
	if err := os.WriteFile(filepath.Join(creatorDir, "2026-05-16_作品_1001_data.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(creatorDir, "2026-05-16_作品_1001.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newTestStore(t)
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	scanner := NewScanner(store, NewEventHub())
	scanner.Enqueue(dir.ID)
	waitFor(t, time.Second, func() bool { return !scanner.Status().Running && scanner.Status().State == "completed" })
	creators, err := store.Creators()
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 {
		t.Fatalf("creators = %#v", creators)
	}
	if creators[0].BilibiliUID != "75233440081" || creators[0].BilibiliSecUID != "MS4wLjABAAAA_TEST" || creators[0].BilibiliProfileURL == "" || creators[0].OnlineIdentityStatus != "ready" {
		t.Fatalf("identity not extracted: %#v", creators[0])
	}
}

func TestScanBackfillsSecUIDFromAnyWorkJSON(t *testing.T) {
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(creatorDir, "2026-05-16_作品_1001.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newTestStore(t)
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{AwemeID: "1001", Title: "旧作品", Media: []MediaInput{{Type: "video", FilePath: filepath.Join(creatorDir, "2026-05-16_作品_1001.mp4"), FileName: "2026-05-16_作品_1001.mp4"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(creatorDir, "2026-05-16_作品_1001.json"), []byte(`{"aweme_id":"1001","desc":"作品","author":{"sec_uid":"MS4wLjABAAAA_BACKFILL"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	scanner := NewScanner(store, NewEventHub())
	scanner.Enqueue(dir.ID)
	waitFor(t, time.Second, func() bool { return !scanner.Status().Running && scanner.Status().State == "completed" })
	creators, err := store.Creators()
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 {
		t.Fatalf("creators = %#v", creators)
	}
	if creators[0].BilibiliSecUID != "MS4wLjABAAAA_BACKFILL" || creators[0].BilibiliProfileURL != "https://www.bilibili.com/user/MS4wLjABAAAA_BACKFILL" || creators[0].OnlineIdentityStatus != "ready" {
		t.Fatalf("identity not backfilled: %#v", creators[0])
	}
}

func TestUpdateManagerFailsMissingIdentityAndContinues(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	missing, _, err := store.UpsertCreatorWithWorks(dir.ID, "缺身份", []WorkInput{{AwemeID: "1", Title: "one", Media: []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "a.webp"), FileName: "a.webp"}}}})
	if err != nil {
		t.Fatal(err)
	}
	ready, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "有身份", CreatorIdentity{UID: "uid", SecUID: "sec", ProfileURL: "https://www.bilibili.com/user/sec"}, []WorkInput{{AwemeID: "2", Title: "two", Media: []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "b.webp"), FileName: "b.webp"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), writingDownloader{awemeID: "3001", title: "队列作品"})
	if _, err := manager.EnqueueImmediate([]int64{missing.ID, ready.ID}, 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return !manager.Status().Running && manager.Status().PendingCreators == 0 })
	status := manager.Status()
	if status.SucceededCreators != 1 || status.FailedCreators != 1 || status.AddedWorks != 1 {
		t.Fatalf("status = %#v", status)
	}
	if len(status.Errors) == 0 || status.Errors[0] == "" {
		t.Fatalf("expected missing identity error: %#v", status.Errors)
	}
}

func TestUpdateQueuesCookieAndSchedule(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "阿言", CreatorIdentity{UID: "uid", SecUID: "sec", ProfileURL: "https://www.bilibili.com/user/sec"}, []WorkInput{{AwemeID: "1", Title: "one", Media: []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "a.webp"), FileName: "a.webp"}}}})
	if err != nil {
		t.Fatal(err)
	}
	queue, err := store.CreateUpdateQueue("每日更新", "0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddCreatorsToUpdateQueue(queue.ID, []int64{creator.ID}); err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), fakeDownloader{})
	run, err := manager.EnqueueQueueNow(queue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.QueueID != queue.ID || run.TotalCreators != 1 {
		t.Fatalf("run = %#v", run)
	}
	waitFor(t, time.Second, func() bool { return !manager.Status().Running && manager.Status().PendingCreators == 0 })
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	cookie, err := store.UpdateCookie()
	if err != nil {
		t.Fatal(err)
	}
	if !cookie.Configured {
		t.Fatalf("cookie should be configured before value assertion: %#v", cookie)
	}
	if cookie.Value != "msToken=one; ttwid=two" {
		t.Fatalf("cookie value should be visible to admin API/store caller: %#v", cookie)
	}
	if err := manager.ValidateCookie(context.Background()); err != nil {
		t.Fatal(err)
	}
	cookie, _ = store.UpdateCookie()
	if cookie.ValidationStatus != "valid" {
		t.Fatalf("cookie status = %#v", cookie)
	}
}

func TestUpdateExistingWorksReportsNoAddedWorks(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(creatorDir, "2026-05-16_作品_1001.mp4")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(creatorDir, "2026-05-16_作品_1001.json"), []byte(`{"aweme_id":"1001","desc":"作品","author":{"sec_uid":"sec"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "阿言", CreatorIdentity{SecUID: "sec", ProfileURL: "https://www.bilibili.com/user/sec"}, []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: mediaPath, FileName: filepath.Base(mediaPath)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), fakeDownloader{addedWorks: 0, explicitAddedWorks: true})
	if _, err := manager.EnqueueImmediate([]int64{creator.ID}, 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		jobs, err := store.UpdateJobs(10)
		return err == nil && len(jobs) == 1 && jobs[0].Status == "succeeded"
	})
	status := manager.Status()
	if status.SucceededCreators != 1 || status.FailedCreators != 0 || status.AddedWorks != 0 {
		t.Fatalf("status = %#v", status)
	}
	jobs, err := store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].AddedWorks != 0 {
		t.Fatalf("jobs = %#v", jobs)
	}
}

func TestUpdateDownloadsAndAutoIndexesNewWorks(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "待更新", CreatorIdentity{SecUID: "sec", ProfileURL: "https://www.bilibili.com/user/sec"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), writingDownloader{awemeID: "2001", title: "新作品"})
	if _, err := manager.EnqueueImmediate([]int64{creator.ID}, 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		jobs, err := store.UpdateJobs(10)
		return err == nil && len(jobs) == 1 && jobs[0].Status == "succeeded"
	})
	status := manager.Status()
	if status.SucceededCreators != 1 || status.FailedCreators != 0 || status.AddedWorks != 1 {
		t.Fatalf("status = %#v", status)
	}
	works, err := store.FeedWithOptions(FeedOptions{CreatorID: creator.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 || works[0].AwemeID != "2001" {
		t.Fatalf("works not auto-indexed: %#v", works)
	}
}

func TestUpdatePromotesPlaceholderCreatorNameAndDirectoryToNickname(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, err := store.ImportCreatorPlaceholder(dir.ID, "https://www.bilibili.com/user/MS4wLjABAAAA_5QAQbrgg")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(creator.Name, "待更新") {
		t.Fatalf("placeholder should not contain pending prefix: %#v", creator)
	}
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), writingDownloader{
		awemeID:     "2003",
		title:       "新作品",
		creatorName: "宇菠萝",
	})
	if _, err := manager.EnqueueImmediate([]int64{creator.ID}, 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		jobs, err := store.UpdateJobs(10)
		return err == nil && len(jobs) == 1 && jobs[0].Status == "succeeded"
	})

	updated, err := store.Creator(creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "宇菠萝" {
		t.Fatalf("creator name not promoted: %#v", updated)
	}
	if _, err := os.Stat(filepath.Join(root, "宇菠萝")); err != nil {
		t.Fatalf("renamed directory missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, creator.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old placeholder directory should not remain: %v", err)
	}
	jobs, err := store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].CreatorName != "宇菠萝" {
		t.Fatalf("jobs should show promoted chinese name: %#v", jobs)
	}
}

func TestUpdateResolvesPlaceholderNameBeforeQueueing(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, err := store.ImportCreatorPlaceholder(dir.ID, "https://www.bilibili.com/user/MS4wLjABAAAA_wxKfOR6J")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), writingDownloader{
		awemeID:     "2004",
		title:       "新作品",
		creatorName: "全哥",
	})
	if _, err := manager.EnqueueImmediate([]int64{creator.ID}, 0); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].CreatorName != "全哥" {
		t.Fatalf("queued job should show resolved chinese name before download: %#v", jobs)
	}
	updated, err := store.Creator(creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "全哥" || updated.Bio != "全哥签名" || updated.AvatarURL == "" {
		t.Fatalf("creator profile not resolved before queueing: %#v", updated)
	}
	waitFor(t, time.Second, func() bool { return !manager.Status().Running && manager.Status().PendingCreators == 0 })
}

func TestPromoteCreatorProfileRenamesNumericCreatorWithWorks(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "54887875", CreatorIdentity{UID: "54887875", SecUID: "54887875", ProfileURL: "https://space.bilibili.com/54887875"}, []WorkInput{{
		AwemeID: "BV1test",
		Title:   "测试作品",
		Media:   []MediaInput{{Type: "video", FilePath: filepath.Join(root, "media.mp4"), FileName: "media.mp4"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := promoteCreatorProfile(store, creator, CreatorProfile{
		Name:       "阿辉白到发光",
		UID:        "54887875",
		SecUID:     "54887875",
		ProfileURL: "https://space.bilibili.com/54887875",
		AvatarURL:  "https://i.example/avatar.jpg",
		Bio:        "八年徒手健身经验",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "阿辉白到发光" || updated.AvatarURL == "" || updated.Bio == "" || updated.WorkCount != 1 {
		t.Fatalf("updated = %#v", updated)
	}
}

func TestUpdateProfileResolveFailureCreatesFailedJobWithoutDownload(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, err := store.ImportCreatorPlaceholder(dir.ID, "https://www.bilibili.com/user/MS4wLjABAAAA_r2YsRJsW")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), profileFailDownloader{})
	status, err := manager.EnqueueImmediate([]int64{creator.ID}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if status.PendingCreators != 0 || status.FailedCreators != 1 || status.Running {
		t.Fatalf("status = %#v", status)
	}
	jobs, err := store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Status != "failed" || !strings.Contains(jobs[0].Error, "未能解析B站UP主资料") {
		t.Fatalf("jobs = %#v", jobs)
	}
}

func TestUpdateIndexesPartialDownloadsEvenWhenDownloaderFails(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "部分失败", CreatorIdentity{SecUID: "sec", ProfileURL: "https://www.bilibili.com/user/sec"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), writingDownloader{awemeID: "2002", title: "部分作品", err: errors.New("网络中断")})
	if _, err := manager.EnqueueImmediate([]int64{creator.ID}, 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		jobs, err := store.UpdateJobs(10)
		return err == nil && len(jobs) == 1 && jobs[0].Status == "failed"
	})
	status := manager.Status()
	if status.SucceededCreators != 0 || status.FailedCreators != 1 || status.AddedWorks != 0 {
		t.Fatalf("status = %#v", status)
	}
	works, err := store.FeedWithOptions(FeedOptions{CreatorID: creator.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 || works[0].AwemeID != "2002" {
		t.Fatalf("partial download should be auto-indexed: %#v", works)
	}
	jobs, err := store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Status != "failed" || !strings.Contains(jobs[0].Error, "网络中断") {
		t.Fatalf("job should keep downloader error: %#v", jobs)
	}
}

func TestRetryUpdateJobReusesFailedRecordAndContinues(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "断点续跑", CreatorIdentity{SecUID: "sec", ProfileURL: "https://www.bilibili.com/user/sec"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), writingDownloader{awemeID: "2101", title: "已落盘作品", err: errors.New("网络中断")})
	if _, err := manager.EnqueueImmediate([]int64{creator.ID}, 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		jobs, err := store.UpdateJobs(10)
		return err == nil && len(jobs) == 1 && jobs[0].Status == "failed"
	})
	jobs, err := store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Attempt != 1 {
		t.Fatalf("initial jobs = %#v", jobs)
	}
	originalID := jobs[0].ID

	manager.downloader = writingDownloader{awemeID: "2102", title: "续跑作品"}
	if _, err := manager.RetryJob(originalID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		jobs, err := store.UpdateJobs(10)
		return err == nil && len(jobs) == 1 && jobs[0].ID == originalID && jobs[0].Status == "succeeded"
	})
	jobs, err = store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != originalID || jobs[0].Attempt != 2 || jobs[0].AddedWorks != 1 {
		t.Fatalf("retry should reuse one job with attempt+1: %#v", jobs)
	}
	status := manager.Status()
	if status.TotalCreators != 1 || status.SucceededCreators != 1 || status.FailedCreators != 0 {
		t.Fatalf("retry should not inflate counters: %#v", status)
	}
	works, err := store.FeedWithOptions(FeedOptions{CreatorID: creator.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 2 {
		t.Fatalf("retry should keep indexed partial work and add remaining work: %#v", works)
	}
}

func TestRetryUpdateJobFailureStaysOnSameRecord(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "缺身份", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	queue, err := store.CreateUpdateQueue("断点队列", "0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateUpdateRun(queue, 1)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.AddFailedUpdateJob(creator.ID, queue.ID, run.ID, 0, 1, "首次失败")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), writingDownloader{awemeID: "2103", title: "不会下载"})
	if _, err := manager.RetryJob(job.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		jobs, err := store.UpdateJobs(10)
		return err == nil && len(jobs) == 1 && jobs[0].ID == job.ID && jobs[0].Status == "failed" && jobs[0].Attempt == 2
	})
	jobs, err := store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != job.ID || jobs[0].Attempt != 2 || !strings.Contains(jobs[0].Error, "缺少B站主页信息") {
		t.Fatalf("retry failure should update same failed record: %#v", jobs)
	}
	updatedRun, err := store.UpdateRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedRun.TotalCreators != 1 || updatedRun.FailedCreators != 1 || updatedRun.SucceededCreators != 0 {
		t.Fatalf("retry should not double count run failures: %#v", updatedRun)
	}
}

func TestUpdateFailsWhenAutoIndexFindsNoWorks(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "空下载", CreatorIdentity{SecUID: "sec", ProfileURL: "https://www.bilibili.com/user/sec"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	manager := NewUpdateManager(store, NewEventHub(), noWriteDownloader{})
	if _, err := manager.EnqueueImmediate([]int64{creator.ID}, 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		jobs, err := store.UpdateJobs(10)
		return err == nil && len(jobs) == 1 && jobs[0].Status == "failed"
	})
	status := manager.Status()
	if status.SucceededCreators != 0 || status.FailedCreators != 1 {
		t.Fatalf("status = %#v", status)
	}
	jobs, err := store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || !strings.Contains(jobs[0].Error, "自动入库失败") {
		t.Fatalf("expected auto-index error: %#v", jobs)
	}
}

func TestResetImmediateUpdateQueueCancelsAndClearsJobs(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "阿言", CreatorIdentity{SecUID: "sec", ProfileURL: "https://www.bilibili.com/user/sec"}, []WorkInput{{AwemeID: "1", Title: "one", Media: []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "a.webp"), FileName: "a.webp"}}}})
	if err != nil {
		t.Fatal(err)
	}
	downloader := blockingDownloader{started: make(chan struct{}), released: make(chan struct{})}
	manager := NewUpdateManager(store, NewEventHub(), downloader)
	defer manager.Close()
	if err := store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnqueueImmediate([]int64{creator.ID}, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-downloader.started:
	case <-time.After(time.Second):
		t.Fatal("downloader did not start")
	}
	status, err := manager.ResetImmediate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Running || status.PendingCreators != 0 || status.TotalCreators != 0 || status.State != "idle" {
		t.Fatalf("reset status = %#v", status)
	}
	close(downloader.released)
	waitFor(t, time.Second, func() bool { return !manager.Status().Running })
	jobs, err := store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs should be cleared: %#v", jobs)
	}
}

func TestUpdateJobsPaginationDeleteAndRunningStatus(t *testing.T) {
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	dir, err := app.store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := app.store.UpsertCreatorWithWorksWithIdentity(dir.ID, "阿言", CreatorIdentity{SecUID: "sec", ProfileURL: "https://www.bilibili.com/user/sec"}, []WorkInput{{
		AwemeID: "1",
		Title:   "one",
		Media:   []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "a.webp"), FileName: "a.webp"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		if _, err := app.store.AddFailedUpdateJob(creator.ID, 0, 0, 0, 1, "失败记录"); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")

	resp, err := client.Get(server.URL + "/api/update/jobs?page=2&page_size=5")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var page UpdateJobsPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Page != 2 || page.PageSize != 5 || page.Total != 12 || page.TotalPages != 3 || len(page.Jobs) != 5 {
		t.Fatalf("page = %#v", page)
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/update/jobs/"+strconvID(page.Jobs[0].ID), nil)
	deleteResp, err := client.Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", deleteResp.StatusCode)
	}
	afterDelete, err := app.store.UpdateJobsPage(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if afterDelete.Total != 11 {
		t.Fatalf("total after delete = %d", afterDelete.Total)
	}

	runningJobs, err := app.store.AddUpdateJobs([]int64{creator.ID}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runningJobs) != 1 {
		t.Fatalf("running jobs = %#v", runningJobs)
	}
	if err := app.store.MarkUpdateJobRunning(runningJobs[0].ID); err != nil {
		t.Fatal(err)
	}
	deleteRunningReq, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/update/jobs/"+strconvID(runningJobs[0].ID), nil)
	deleteRunningResp, err := client.Do(deleteRunningReq)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteRunningResp.Body.Close()
	if deleteRunningResp.StatusCode != http.StatusConflict {
		t.Fatalf("delete running status = %d body=%s", deleteRunningResp.StatusCode, readBody(t, deleteRunningResp))
	}

	statusResp, err := client.Get(server.URL + "/api/update/status")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	var statusBody struct {
		Status UpdateStatus `json:"status"`
	}
	if err := json.NewDecoder(statusResp.Body).Decode(&statusBody); err != nil {
		t.Fatal(err)
	}
	if len(statusBody.Status.RunningJobs) != 1 || statusBody.Status.RunningJobs[0].ID != runningJobs[0].ID {
		t.Fatalf("running jobs not returned: %#v", statusBody.Status.RunningJobs)
	}
}

func TestUpdateImmediateRejectsWhenDownloaderUnavailable(t *testing.T) {
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	app.updater = NewUpdateManager(app.store, app.updateHub, unavailableDownloader{})

	dir, err := app.store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := app.store.UpsertCreatorWithWorksWithIdentity(dir.ID, "阿言", CreatorIdentity{SecUID: "sec", ProfileURL: "https://www.bilibili.com/user/sec"}, []WorkInput{{
		AwemeID: "1",
		Title:   "one",
		Media:   []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "a.webp"), FileName: "a.webp"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")

	body, _ := json.Marshal(map[string]any{"creator_ids": []int64{creator.ID}})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/update/immediate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	jobs, err := app.store.UpdateJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs should not be created when downloader is unavailable: %#v", jobs)
	}
}

func TestUpdateAPIsAreAdminOnly(t *testing.T) {
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	adminClient := cookieClient()
	login(t, adminClient, server.URL, "ted", "ted")

	body, _ := json.Marshal(map[string]any{"username": "alice", "password": "secret"})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if resp, err := adminClient.Do(req); err != nil || resp.StatusCode != http.StatusCreated {
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("create user status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}

	userClient := cookieClient()
	login(t, userClient, server.URL, "alice", "secret")
	resp, err := userClient.Get(server.URL + "/api/update/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin update status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}

	resp, err = adminClient.Get(server.URL + "/api/update/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin update status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}

	body, _ = json.Marshal(map[string]string{"cookie": "msToken=one; ttwid=two"})
	req, _ = http.NewRequest(http.MethodPatch, server.URL+"/api/update/cookie", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = adminClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	resp, err = adminClient.Get(server.URL + "/api/update/cookie")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var cookieBody struct {
		Cookie UpdateCookie `json:"cookie"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cookieBody); err != nil {
		t.Fatal(err)
	}
	if cookieBody.Cookie.Value != "msToken=one; ttwid=two" {
		t.Fatalf("cookie value not returned to admin: %#v", cookieBody.Cookie)
	}

	resetReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/update/immediate/reset", nil)
	resetResp, err := userClient.Do(resetReq)
	if err != nil {
		t.Fatal(err)
	}
	defer resetResp.Body.Close()
	if resetResp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin reset status = %d body=%s", resetResp.StatusCode, readBody(t, resetResp))
	}
}

type fakeDownloader struct {
	addedWorks         int
	explicitAddedWorks bool
}

func (d fakeDownloader) UpdateCreator(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	if d.explicitAddedWorks {
		return DownloadResult{AddedWorks: d.addedWorks}, nil
	}
	return DownloadResult{AddedWorks: 1}, nil
}

type blockingDownloader struct {
	started  chan struct{}
	released chan struct{}
}

type unavailableDownloader struct{}

func (unavailableDownloader) UpdateCreator(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	return DownloadResult{}, errors.New("should not run")
}

func (unavailableDownloader) ValidateCookie(ctx context.Context, cookie string) error {
	return nil
}

func (unavailableDownloader) Ready(ctx context.Context) error {
	return errors.New("下载器未安装")
}

func (d blockingDownloader) UpdateCreator(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	close(d.started)
	select {
	case <-ctx.Done():
		return DownloadResult{}, ctx.Err()
	case <-d.released:
		return DownloadResult{AddedWorks: 1}, nil
	}
}

func (blockingDownloader) ValidateCookie(ctx context.Context, cookie string) error {
	if cookie == "" {
		return ErrCookieRequired
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	return nil
}

func (blockingDownloader) ResolveCreatorProfile(ctx context.Context, profileURL, cookie string) (CreatorProfile, error) {
	if strings.TrimSpace(cookie) == "" {
		return CreatorProfile{}, ErrCookieRequired
	}
	return CreatorProfile{Name: "阿言", SecUID: extractBilibiliSecUID(profileURL), ProfileURL: profileURL}, ctx.Err()
}

func (fakeDownloader) ValidateCookie(ctx context.Context, cookie string) error {
	if cookie == "" {
		return ErrCookieRequired
	}
	return nil
}

func (fakeDownloader) ResolveCreatorProfile(ctx context.Context, profileURL, cookie string) (CreatorProfile, error) {
	if strings.TrimSpace(cookie) == "" {
		return CreatorProfile{}, ErrCookieRequired
	}
	return CreatorProfile{Name: "阿言", UID: "uid", SecUID: extractBilibiliSecUID(profileURL), ProfileURL: profileURL, AvatarURL: "https://example.test/avatar.webp", Bio: "签名"}, ctx.Err()
}

type writingDownloader struct {
	awemeID     string
	title       string
	creatorName string
	err         error
}

func (d writingDownloader) UpdateCreator(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	id := d.awemeID
	if id == "" {
		id = "2001"
	}
	title := d.title
	if title == "" {
		title = "新作品"
	}
	prefix := "2026-06-04_" + title + "_" + id
	if err := os.MkdirAll(task.OutputDir, 0o755); err != nil {
		return DownloadResult{}, err
	}
	if err := os.WriteFile(filepath.Join(task.OutputDir, prefix+".mp4"), []byte("video"), 0o644); err != nil {
		return DownloadResult{}, err
	}
	creatorName := d.creatorName
	if creatorName == "" {
		creatorName = "阿言"
	}
	data := `{"aweme_id":"` + id + `","desc":"` + title + `","create_time":1780502400,"author":{"nickname":"` + creatorName + `","sec_uid":"sec","share_info":{"share_url":"https://www.bilibili.com/user/sec"}}}`
	if err := os.WriteFile(filepath.Join(task.OutputDir, prefix+"_data.json"), []byte(data), 0o644); err != nil {
		return DownloadResult{}, err
	}
	if d.err != nil {
		return DownloadResult{AddedWorks: 1, CreatorName: creatorName}, d.err
	}
	return DownloadResult{AddedWorks: 1, CreatorName: creatorName}, ctx.Err()
}

func (d writingDownloader) ResolveCreatorProfile(ctx context.Context, profileURL, cookie string) (CreatorProfile, error) {
	if strings.TrimSpace(cookie) == "" {
		return CreatorProfile{}, ErrCookieRequired
	}
	creatorName := d.creatorName
	if creatorName == "" {
		creatorName = "阿言"
	}
	return CreatorProfile{
		Name:       creatorName,
		UID:        "uid-" + creatorName,
		SecUID:     extractBilibiliSecUID(profileURL),
		ProfileURL: profileURL,
		AvatarURL:  "https://example.test/avatar/" + creatorName + ".webp",
		Bio:        creatorName + "签名",
	}, ctx.Err()
}

func (writingDownloader) ValidateCookie(ctx context.Context, cookie string) error {
	if cookie == "" {
		return ErrCookieRequired
	}
	return ctx.Err()
}

type noWriteDownloader struct{}

func (noWriteDownloader) UpdateCreator(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	return DownloadResult{}, ctx.Err()
}

func (noWriteDownloader) ValidateCookie(ctx context.Context, cookie string) error {
	if cookie == "" {
		return ErrCookieRequired
	}
	return ctx.Err()
}

func (noWriteDownloader) ResolveCreatorProfile(ctx context.Context, profileURL, cookie string) (CreatorProfile, error) {
	if strings.TrimSpace(cookie) == "" {
		return CreatorProfile{}, ErrCookieRequired
	}
	return CreatorProfile{Name: "空下载", SecUID: extractBilibiliSecUID(profileURL), ProfileURL: profileURL}, ctx.Err()
}

type profileFailDownloader struct{}

func (profileFailDownloader) UpdateCreator(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	return DownloadResult{}, errors.New("should not download")
}

func (profileFailDownloader) ValidateCookie(ctx context.Context, cookie string) error {
	return nil
}

func (profileFailDownloader) ResolveCreatorProfile(ctx context.Context, profileURL, cookie string) (CreatorProfile, error) {
	return CreatorProfile{}, errors.New("profile api failed")
}

func waitFor(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
