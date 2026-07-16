package server

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

var ErrCookieRequired = errors.New("cookie is required")

type DownloadTask struct {
	Creator    Creator
	ProfileURL string
	OutputDir  string
	Cookie     string
	Progress   func(DownloadProgress)
}

type DownloadResult struct {
	AddedWorks  int
	CreatorName string
}

type CreatorProfile struct {
	Name       string
	UID        string
	SecUID     string
	ProfileURL string
	AvatarURL  string
	Bio        string
}

type DownloadProgress struct {
	TotalWorks      int
	ProcessedWorks  int
	DownloadedMedia int
	SkippedWorks    int
	SpeedBps        int64
	Message         string
	Directory       string
	Notice          string
}

type Downloader interface {
	UpdateCreator(context.Context, DownloadTask) (DownloadResult, error)
	ValidateCookie(context.Context, string) error
}

type creatorProfileResolver interface {
	ResolveCreatorProfile(context.Context, string, string) (CreatorProfile, error)
}

type updateQueuePreparation struct {
	ReadyCreatorIDs []int64
	FailedJobs      []UpdateJob
}

type readyDownloader interface {
	Ready(context.Context) error
}

type CommandDownloader struct {
	path string
}

func NewCommandDownloader() CommandDownloader {
	if path := strings.TrimSpace(os.Getenv("LOCALBILIBILI_DOWNLOADER")); path != "" {
		return CommandDownloader{path: path}
	}
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		for _, candidate := range []string{
			filepath.Join(exeDir, "bilibili-downloader", "start_download.sh"),
			filepath.Join(exeDir, "..", "packaging", "fnos", "localbilibili", "app", "bilibili-downloader", "start_download.sh"),
		} {
			if fileExecutable(candidate) {
				return CommandDownloader{path: candidate}
			}
		}
		return CommandDownloader{path: filepath.Join(exeDir, "bilibili-downloader", "start_download.sh")}
	}
	return CommandDownloader{path: filepath.Join("bilibili-downloader", "start_download.sh")}
}

func (d CommandDownloader) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !fileExecutable(d.path) {
		return errors.New("内置下载器不可用，请确认程序目录包含 bilibili-downloader/start_download.sh，或设置 LOCALBILIBILI_DOWNLOADER")
	}
	runtimeDir := filepath.Join(filepath.Dir(d.path), "runtime", "run.py")
	if home := strings.TrimSpace(os.Getenv("BILIBILI_DOWNLOADER_HOME")); home != "" {
		runtimeDir = filepath.Join(home, "run.py")
	}
	if _, err := os.Stat(runtimeDir); err != nil {
		return errors.New("内置下载器运行时不可用，请确认 bilibili-downloader/runtime/run.py 已安装，或设置 BILIBILI_DOWNLOADER_HOME")
	}
	return nil
}

func (d CommandDownloader) UpdateCreator(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	if strings.TrimSpace(task.ProfileURL) == "" {
		return DownloadResult{}, errors.New("missing bilibili profile url")
	}
	if err := d.Ready(ctx); err != nil {
		return DownloadResult{}, err
	}
	cmd := exec.CommandContext(ctx, d.path, task.ProfileURL)
	cmd.Env = append(os.Environ(),
		"LOCALBILIBILI_DOWNLOAD_DIR="+task.OutputDir,
		"LOCALBILIBILI_BILIBILI_COOKIE="+task.Cookie,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return DownloadResult{}, errors.New(message)
	}
	return DownloadResult{}, nil
}

func (d CommandDownloader) ValidateCookie(_ context.Context, cookie string) error {
	if strings.TrimSpace(cookie) == "" {
		return ErrCookieRequired
	}
	return nil
}

func fileExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

type UpdateManager struct {
	store      *Store
	hub        *EventHub
	downloader Downloader

	mu             sync.Mutex
	running        bool
	status         UpdateStatus
	stop           chan struct{}
	currentCancel  context.CancelFunc
	currentCancels map[int64]context.CancelFunc
	resetVersion   int64
	concurrency    int
	activeWorkers  int
}

func NewUpdateManager(store *Store, hub *EventHub, downloader Downloader, concurrency ...int) *UpdateManager {
	if downloader == nil {
		if strings.TrimSpace(os.Getenv("LOCALBILIBILI_DOWNLOADER")) != "" {
			downloader = NewCommandDownloader()
		} else {
			downloader = BilibiliDownloader{}
		}
	}
	manager := &UpdateManager{
		store:          store,
		hub:            hub,
		downloader:     downloader,
		status:         UpdateStatus{State: "idle", Errors: []string{}},
		stop:           make(chan struct{}),
		currentCancels: map[int64]context.CancelFunc{},
		concurrency:    normalizeDownloaderConcurrency(firstConcurrency(concurrency)),
	}
	_, _ = store.FailInterruptedUpdateJobs("服务重启后更新任务已中断，请重新触发更新")
	manager.mu.Lock()
	manager.refreshJobsLocked()
	manager.mu.Unlock()
	go manager.scheduleLoop()
	return manager
}

func firstConcurrency(values []int) int {
	if len(values) == 0 {
		return DefaultDownloaderConcurrency
	}
	return values[0]
}

func (m *UpdateManager) SetConcurrency(value int) {
	m.mu.Lock()
	m.concurrency = normalizeDownloaderConcurrency(value)
	shouldStart := m.running && m.activeWorkers < m.concurrency
	m.mu.Unlock()
	if shouldStart {
		m.startWorkers()
	}
}

func (m *UpdateManager) Close() {
	close(m.stop)
}

func (m *UpdateManager) EnqueueImmediate(creatorIDs []int64, runID int64) (UpdateStatus, error) {
	if err := m.ensureDownloaderReady(context.Background()); err != nil {
		return UpdateStatus{}, err
	}
	prepared, err := m.prepareCreatorsForQueue(context.Background(), creatorIDs, 0, runID, 0, 1, "manual")
	if err != nil {
		return UpdateStatus{}, err
	}
	jobs, err := m.store.AddUpdateJobs(prepared.ReadyCreatorIDs, 0, runID, "manual")
	if err != nil {
		return UpdateStatus{}, err
	}
	m.mu.Lock()
	m.status.TotalCreators += len(jobs) + len(prepared.FailedJobs)
	m.status.PendingCreators += len(jobs)
	m.status.FailedCreators += len(prepared.FailedJobs)
	if len(jobs) > 0 {
		m.status.State = "queued"
	} else if len(prepared.FailedJobs) > 0 {
		m.status.State = "failed"
	}
	for _, failed := range prepared.FailedJobs {
		m.status.Errors = append(m.status.Errors, failed.CreatorName+": "+failed.Error)
	}
	m.recomputeProgressLocked()
	m.refreshJobsLocked()
	status := m.snapshotLocked()
	shouldRun := len(jobs) > 0 && !m.running
	if shouldRun {
		m.running = true
	}
	m.mu.Unlock()
	m.publish("queued", status, nil)
	if len(jobs) > 0 {
		m.startWorkers()
	}
	return status, nil
}

func (m *UpdateManager) EnqueueQueueNow(queueID int64, triggerSource string) (UpdateRun, error) {
	if err := m.ensureDownloaderReady(context.Background()); err != nil {
		return UpdateRun{}, err
	}
	queue, err := m.store.UpdateQueue(queueID)
	if err != nil {
		return UpdateRun{}, err
	}
	creators, err := m.store.CreatorsForUpdateQueue(queueID)
	if err != nil {
		return UpdateRun{}, err
	}
	source := normalizeUpdateTriggerSource(triggerSource)
	run, err := m.store.CreateUpdateRun(queue, len(creators), source)
	if err != nil {
		return UpdateRun{}, err
	}
	ids := make([]int64, 0, len(creators))
	for _, creator := range creators {
		ids = append(ids, creator.ID)
	}
	prepared, err := m.prepareCreatorsForQueue(context.Background(), ids, queueID, run.ID, 0, 1, source)
	if err != nil {
		return UpdateRun{}, err
	}
	jobs, err := m.store.AddUpdateJobs(prepared.ReadyCreatorIDs, queueID, run.ID, source)
	if err != nil {
		return UpdateRun{}, err
	}
	m.mu.Lock()
	m.status.TotalCreators += len(jobs) + len(prepared.FailedJobs)
	m.status.PendingCreators += len(jobs)
	m.status.FailedCreators += len(prepared.FailedJobs)
	if len(jobs) > 0 {
		m.status.State = "queued"
	} else if len(prepared.FailedJobs) > 0 {
		m.status.State = "failed"
	}
	for _, failed := range prepared.FailedJobs {
		m.status.Errors = append(m.status.Errors, failed.CreatorName+": "+failed.Error)
	}
	m.recomputeProgressLocked()
	m.refreshJobsLocked()
	status := m.snapshotLocked()
	shouldRun := len(jobs) > 0 && !m.running
	if shouldRun {
		m.running = true
	}
	m.mu.Unlock()
	m.publish("queue_queued", status, nil)
	if len(jobs) > 0 {
		m.startWorkers()
	}
	return run, nil
}

func (m *UpdateManager) RetryJob(id int64) (UpdateStatus, error) {
	if err := m.ensureDownloaderReady(context.Background()); err != nil {
		return UpdateStatus{}, err
	}
	job, err := m.store.RetryUpdateJob(id)
	if err != nil {
		return UpdateStatus{}, err
	}
	m.mu.Lock()
	if m.status.TotalCreators == 0 {
		m.status.TotalCreators = 1
	}
	if m.status.FailedCreators > 0 {
		m.status.FailedCreators--
	}
	m.status.PendingCreators++
	m.status.State = "queued"
	m.recomputeProgressLocked()
	m.refreshJobsLocked()
	status := m.snapshotLocked()
	if !m.running {
		m.running = true
	}
	m.mu.Unlock()
	m.publish("retry_queued", status, &job)
	m.startWorkers()
	return status, nil
}

func (m *UpdateManager) ensureDownloaderReady(ctx context.Context) error {
	if checker, ok := m.downloader.(readyDownloader); ok {
		return checker.Ready(ctx)
	}
	return nil
}

func (m *UpdateManager) Status() UpdateStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshJobsLocked()
	return m.snapshotLocked()
}

func (m *UpdateManager) ResetImmediate(ctx context.Context) (UpdateStatus, error) {
	m.mu.Lock()
	m.resetVersion++
	for id, cancel := range m.currentCancels {
		cancel()
		delete(m.currentCancels, id)
	}
	m.currentCancel = nil
	m.status = UpdateStatus{State: "idle", Errors: []string{}}
	m.running = false
	m.activeWorkers = 0
	version := m.resetVersion
	m.mu.Unlock()
	if err := m.store.ClearUpdateJobs(); err != nil {
		return UpdateStatus{}, err
	}
	m.mu.Lock()
	if m.resetVersion == version {
		m.refreshJobsLocked()
	}
	status := m.snapshotLocked()
	m.mu.Unlock()
	m.publish("reset", status, nil)
	_ = ctx
	return status, nil
}

func (m *UpdateManager) ValidateCookie(ctx context.Context) error {
	cookie, err := m.store.updateCookieValue()
	if err != nil {
		return err
	}
	if err := m.downloader.ValidateCookie(ctx, cookie); err != nil {
		_ = m.store.MarkUpdateCookieValidation("invalid", err.Error())
		return err
	}
	return m.store.MarkUpdateCookieValidation("valid", "Cookie 可用")
}

func (m *UpdateManager) prepareCreatorsForQueue(ctx context.Context, creatorIDs []int64, queueID, runID, retryOfJobID int64, attempt int, triggerSources ...string) (updateQueuePreparation, error) {
	prepared := updateQueuePreparation{ReadyCreatorIDs: []int64{}, FailedJobs: []UpdateJob{}}
	triggerSource := "manual"
	if len(triggerSources) > 0 {
		triggerSource = normalizeUpdateTriggerSource(triggerSources[0])
	}
	if len(creatorIDs) == 0 {
		return prepared, nil
	}
	resolver, ok := m.downloader.(creatorProfileResolver)
	cookie, cookieErr := m.store.updateCookieValue()
	seen := map[int64]bool{}
	for _, id := range creatorIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		creator, err := m.store.Creator(id)
		if err != nil {
			return prepared, err
		}
		profileURL := strings.TrimSpace(creator.BilibiliProfileURL)
		if profileURL == "" && strings.TrimSpace(creator.BilibiliSecUID) != "" {
			profileURL = "https://www.bilibili.com/user/" + strings.TrimSpace(creator.BilibiliSecUID)
		}
		fail := func(message string) error {
			job, err := m.store.AddFailedUpdateJob(creator.ID, queueID, runID, retryOfJobID, attempt, message, triggerSource)
			if err != nil {
				return err
			}
			prepared.FailedJobs = append(prepared.FailedJobs, job)
			return nil
		}
		if !ok {
			if err := fail("未能解析B站UP主资料，请检查 Cookie 或主页链接: 当前下载器不支持资料解析"); err != nil {
				return prepared, err
			}
			continue
		}
		if cookieErr != nil || strings.TrimSpace(cookie) == "" {
			if err := fail("未能解析B站UP主资料，请检查 Cookie 或主页链接: Cookie 为空或不可读取"); err != nil {
				return prepared, err
			}
			continue
		}
		if profileURL == "" {
			if err := fail("未能解析B站UP主资料，请检查 Cookie 或主页链接: 缺少B站主页信息"); err != nil {
				return prepared, err
			}
			continue
		}
		profile, err := resolveBulkImportProfile(ctx, resolver, profileURL, cookie)
		if err != nil {
			if err := fail(err.Error()); err != nil {
				return prepared, err
			}
			continue
		}
		if _, err := promoteCreatorProfile(m.store, creator, profile); err != nil {
			if err := fail("未能解析B站UP主资料，请检查 Cookie 或主页链接: " + err.Error()); err != nil {
				return prepared, err
			}
			continue
		}
		prepared.ReadyCreatorIDs = append(prepared.ReadyCreatorIDs, creator.ID)
	}
	return prepared, nil
}

func (m *UpdateManager) startWorkers() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	for m.activeWorkers < m.concurrency {
		m.activeWorkers++
		go m.run()
	}
	m.mu.Unlock()
}

func (m *UpdateManager) run() {
	defer func() {
		m.mu.Lock()
		if m.activeWorkers > 0 {
			m.activeWorkers--
		}
		m.mu.Unlock()
	}()
	for {
		job, err := m.claimNextPendingJob()
		if errors.Is(err, sql.ErrNoRows) {
			m.mu.Lock()
			if m.activeWorkers <= 1 {
				m.running = false
				m.status.Running = false
				m.status.PendingCreators = 0
				if m.status.State != "failed" {
					m.status.State = "idle"
				}
				m.refreshJobsLocked()
				status := m.snapshotLocked()
				m.mu.Unlock()
				m.publish("completed", status, nil)
				return
			}
			m.mu.Unlock()
			return
		}
		if err != nil {
			m.recordError("读取更新任务失败: " + err.Error())
			continue
		}
		m.execute(job)
	}
}

func (m *UpdateManager) claimNextPendingJob() (UpdateJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, err := m.store.NextPendingUpdateJob()
	if err != nil {
		return UpdateJob{}, err
	}
	if err := m.store.MarkUpdateJobRunning(job.ID); err != nil {
		return UpdateJob{}, err
	}
	job.Status = "running"
	job.StartedAt = time.Now().UTC().Format(time.RFC3339)
	return job, nil
}

func (m *UpdateManager) execute(job UpdateJob) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.status.Running = true
	m.status.State = "running"
	m.status.CurrentCreator = job.CreatorName
	m.currentCancel = cancel
	m.currentCancels[job.ID] = cancel
	version := m.resetVersion
	if m.status.PendingCreators > 0 {
		m.status.PendingCreators--
	}
	m.refreshJobsLocked()
	status := m.snapshotLocked()
	m.mu.Unlock()
	m.publish("started", status, &job)

	addedWorks, err := m.updateCreator(ctx, job)
	cancel()
	m.mu.Lock()
	if m.resetVersion == version {
		delete(m.currentCancels, job.ID)
		if len(m.currentCancels) == 0 {
			m.currentCancel = nil
		}
	}
	if m.resetVersion != version {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	if err != nil {
		message := summarizeUpdateError(err)
		_ = m.store.FinishUpdateJob(job.ID, "failed", 0, message)
		m.mu.Lock()
		m.status.FailedCreators++
		m.status.Errors = append(m.status.Errors, job.CreatorName+": "+message)
		m.recomputeProgressLocked()
		m.refreshJobsLocked()
		status := m.snapshotLocked()
		m.mu.Unlock()
		m.publish("failed", status, &job)
		return
	}
	_ = m.store.FinishUpdateJob(job.ID, "succeeded", addedWorks, "")
	m.mu.Lock()
	m.status.SucceededCreators++
	m.status.AddedWorks += addedWorks
	m.recomputeProgressLocked()
	m.refreshJobsLocked()
	status = m.snapshotLocked()
	m.mu.Unlock()
	m.publish("succeeded", status, &job)
}

func (m *UpdateManager) updateCreator(ctx context.Context, job UpdateJob) (int, error) {
	creator, err := m.store.Creator(job.CreatorID)
	if err != nil {
		return 0, err
	}
	profileURL := strings.TrimSpace(creator.BilibiliProfileURL)
	if profileURL == "" && strings.TrimSpace(creator.BilibiliSecUID) != "" {
		profileURL = "https://www.bilibili.com/user/" + strings.TrimSpace(creator.BilibiliSecUID)
	}
	if profileURL == "" {
		return 0, errors.New("缺少B站主页信息，请手动补充或重新扫描带 author 元数据的作品")
	}
	dir, err := m.store.Directory(creator.DirectoryID)
	if err != nil {
		return 0, err
	}
	outputDir := resolveCreatorOutputDir(dir.Path, creator)
	if err := ensureWritableDirectory(outputDir); err != nil {
		return 0, err
	}
	beforeCount, err := m.store.CreatorWorkCount(creator.ID)
	if err != nil {
		return 0, err
	}
	cookie, err := m.store.updateCookieValue()
	if err != nil {
		return 0, err
	}
	progress := func(progress DownloadProgress) {
		_ = m.store.UpdateUpdateJobProgress(job.ID, progress.TotalWorks, progress.ProcessedWorks, progress.DownloadedMedia, progress.SkippedWorks, progress.SpeedBps, progress.Message, progress.Directory, progress.Notice)
		m.mu.Lock()
		m.refreshJobsLocked()
		status := m.snapshotLocked()
		m.mu.Unlock()
		m.publish("progress", status, nil)
	}
	progress(DownloadProgress{Message: "准备下载", Directory: outputDir})
	result, err := m.downloader.UpdateCreator(ctx, DownloadTask{Creator: creator, ProfileURL: profileURL, OutputDir: outputDir, Cookie: cookie, Progress: progress})
	downloadErr := err
	if renamedDir, renamedName, renameErr := maybePromoteCreatorNameAndDirectory(m.store, creator, dir.Path, outputDir, result.CreatorName); renameErr == nil {
		if renamedDir != "" {
			outputDir = renamedDir
		}
		if renamedName != "" {
			creator.Name = renamedName
		}
	}
	identity, works, warnings, err := scanCreatorWithIdentityAndWarnings(outputDir)
	if err != nil {
		return 0, combineDownloadAndIndexErrors(downloadErr, "自动入库失败，请检查文件命名、目录权限或扫描器识别规则: "+err.Error())
	}
	if len(works) == 0 {
		return 0, combineDownloadAndIndexErrors(downloadErr, "自动入库失败：下载目录中没有识别到可入库作品，请检查下载输出是否为平铺结构")
	}
	if len(warnings) > 0 {
		_ = m.store.RecordSystemEvent(SystemEventInput{
			EventType:  "video_seekability_warning",
			Severity:   "warning",
			Message:    strings.Join(warnings, "；"),
			DetailJSON: `{"creator_id":` + strconv.FormatInt(creator.ID, 10) + `}`,
		})
	}
	updatedCreator, _, err := m.store.UpsertCreatorWithWorksWithIdentity(dir.ID, creator.Name, identity, works)
	if err != nil {
		return 0, combineDownloadAndIndexErrors(downloadErr, "自动入库失败，写入数据库失败: "+err.Error())
	}
	afterCount, err := m.store.CreatorWorkCount(updatedCreator.ID)
	if err != nil {
		return 0, combineDownloadAndIndexErrors(downloadErr, "自动入库失败，读取作品统计失败: "+err.Error())
	}
	if afterCount > beforeCount && afterCount-beforeCount > result.AddedWorks {
		result.AddedWorks = afterCount - beforeCount
	}
	if downloadErr != nil {
		return result.AddedWorks, downloadErr
	}
	return result.AddedWorks, nil
}

func maybePromoteCreatorNameAndDirectory(store *Store, creator Creator, rootDir, currentDir, desiredName string) (string, string, error) {
	desiredName = preferredCreatorName(desiredName)
	if !shouldPromotePlaceholderName(creator, desiredName) {
		return currentDir, creator.Name, nil
	}
	targetDir := filepath.Join(rootDir, desiredName)
	if currentDir != targetDir {
		if _, err := os.Stat(targetDir); err == nil {
			return currentDir, creator.Name, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return currentDir, creator.Name, err
		}
		if err := os.Rename(currentDir, targetDir); err != nil {
			return currentDir, creator.Name, err
		}
		currentDir = targetDir
	}
	updated, err := store.RenameCreator(creator.ID, desiredName)
	if err != nil {
		return currentDir, creator.Name, err
	}
	return currentDir, updated.Name, nil
}

func resolveCreatorOutputDir(rootDir string, creator Creator) string {
	name := strings.TrimSpace(creator.Name)
	if name == "" {
		name = "B站UP主"
	}
	primary := filepath.Join(rootDir, name)
	if hasLocalBilibiliMetadata(primary) {
		return primary
	}
	for _, candidate := range []string{creator.BilibiliUID, creator.BilibiliSecUID} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		path := filepath.Join(rootDir, candidate)
		if path != primary && hasLocalBilibiliMetadata(path) {
			return path
		}
	}
	return primary
}

func hasLocalBilibiliMetadata(dir string) bool {
	existing, err := existingLocalBilibiliMetadataPaths(dir)
	return err == nil && len(existing) > 0
}

func promoteCreatorProfile(store *Store, creator Creator, profile CreatorProfile) (Creator, error) {
	identity := CreatorIdentity{
		UID:        strings.TrimSpace(profile.UID),
		SecUID:     strings.TrimSpace(profile.SecUID),
		ProfileURL: strings.TrimSpace(profile.ProfileURL),
		AvatarURL:  strings.TrimSpace(profile.AvatarURL),
		Bio:        strings.TrimSpace(profile.Bio),
	}
	if identity.ProfileURL == "" && identity.SecUID != "" {
		identity.ProfileURL = "https://www.bilibili.com/user/" + identity.SecUID
	}
	if err := store.UpdateCreatorOnlineProfile(creator.ID, identity); err != nil {
		return creator, err
	}
	updated, err := store.Creator(creator.ID)
	if err != nil {
		return creator, err
	}
	desiredName := preferredCreatorName(profile.Name)
	if shouldPromotePlaceholderName(updated, desiredName) {
		return store.RenameCreator(updated.ID, desiredName)
	}
	return updated, nil
}

func shouldPromotePlaceholderName(creator Creator, desiredName string) bool {
	currentName := strings.TrimSpace(creator.Name)
	desiredName = preferredCreatorName(desiredName)
	if currentName == "" || desiredName == "" || currentName == desiredName {
		return false
	}
	return isLikelyPlaceholderCreator(creator)
}

func isLikelyPlaceholderCreator(creator Creator) bool {
	currentName := strings.TrimSpace(creator.Name)
	if currentName == "" {
		return false
	}
	for _, r := range currentName {
		if unicode.Is(unicode.Han, r) {
			return false
		}
	}
	secUID := strings.TrimSpace(creator.BilibiliSecUID)
	if secUID != "" {
		suffix := secUID
		runes := []rune(secUID)
		if len(runes) > 8 {
			suffix = string(runes[len(runes)-8:])
		}
		if currentName == suffix || strings.HasPrefix(currentName, suffix+"-") {
			return true
		}
	}
	return len([]rune(currentName)) <= 12 && creator.WorkCount == 0
}

func preferredCreatorName(raw string) string {
	return sanitizeCreatorDisplayName(raw)
}

func sanitizeCreatorDisplayName(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer("/", " ", "\\", " ", ":", " ", "*", " ", "?", " ", "\"", " ", "<", " ", ">", " ", "|", " ", "\n", " ", "\r", " ", "\t", " ")
	value = replacer.Replace(value)
	value = strings.Join(strings.Fields(value), " ")
	value = trimUTF8Bytes(value, 72)
	return strings.TrimSpace(value)
}

func combineDownloadAndIndexErrors(downloadErr error, indexMessage string) error {
	if downloadErr != nil {
		return errors.New(downloadErr.Error() + "；" + indexMessage)
	}
	return errors.New(indexMessage)
}

func summarizeUpdateError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.Join(strings.Fields(err.Error()), " ")
	runes := []rune(message)
	if len(runes) <= 360 {
		return message
	}
	return string(runes[:360]) + "..."
}

func (m *UpdateManager) recordError(message string) {
	m.mu.Lock()
	m.status.Errors = append(m.status.Errors, message)
	m.status.State = "failed"
	status := m.snapshotLocked()
	m.mu.Unlock()
	m.publish("error", status, nil)
}

func (m *UpdateManager) scheduleLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			m.enqueueDueQueues(now.UTC())
		}
	}
}

func (m *UpdateManager) enqueueDueQueues(now time.Time) {
	queues, err := m.store.UpdateQueues()
	if err != nil {
		m.recordError("读取定时队列失败: " + err.Error())
		return
	}
	for _, queue := range queues {
		if !queue.Enabled || strings.TrimSpace(queue.NextRunAt) == "" {
			continue
		}
		next, err := time.Parse(time.RFC3339, queue.NextRunAt)
		if err != nil || next.After(now) {
			continue
		}
		_, _ = m.EnqueueQueueNow(queue.ID, "scheduled")
		nextRun := NextCronRun(queue.Cron, now)
		_, _ = m.store.UpdateUpdateQueue(queue.ID, "", queue.Cron, nil, nil)
		if !nextRun.IsZero() {
			_, _ = m.store.db.Exec(`UPDATE update_queues SET next_run_at=? WHERE id=?`, nextRun.Format(time.RFC3339), queue.ID)
		}
	}
}

func (m *UpdateManager) publish(kind string, status UpdateStatus, job *UpdateJob) {
	if m.hub != nil {
		m.hub.PublishUpdate(UpdateEvent{Type: kind, Status: status, Job: job})
	}
}

func (m *UpdateManager) refreshJobsLocked() {
	jobs, err := m.store.UpdateJobs(30)
	if err == nil {
		m.status.Jobs = jobs
	}
	runningJobs, err := m.store.RunningUpdateJobs()
	if err == nil {
		m.status.RunningJobs = runningJobs
	}
}

func (m *UpdateManager) recomputeProgressLocked() {
	done := m.status.SucceededCreators + m.status.FailedCreators
	if m.status.TotalCreators > 0 {
		m.status.Progress = int(float64(done) / float64(m.status.TotalCreators) * 100)
	}
}

func (m *UpdateManager) snapshotLocked() UpdateStatus {
	status := m.status
	status.Errors = append([]string(nil), m.status.Errors...)
	status.RunningJobs = append([]UpdateJob(nil), m.status.RunningJobs...)
	status.Jobs = append([]UpdateJob(nil), m.status.Jobs...)
	return status
}
