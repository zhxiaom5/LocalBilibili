package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SubtitleManager struct {
	store *Store
	hub   *EventHub

	mu             sync.Mutex
	status         SubtitleStatus
	stop           chan struct{}
	currentCancels map[int64]context.CancelFunc
	concurrency    int
	activeWorkers  int
	config         SubtitleRuntimeConfig
}

func NewSubtitleManager(store *Store, hub *EventHub, cfg SubtitleRuntimeConfig) *SubtitleManager {
	cfg = normalizeSubtitleRuntimeConfig(cfg)
	manager := &SubtitleManager{
		store:          store,
		hub:            hub,
		status:         SubtitleStatus{State: "idle", Errors: []string{}},
		stop:           make(chan struct{}),
		currentCancels: map[int64]context.CancelFunc{},
		concurrency:    cfg.Concurrency,
		config:         cfg,
	}
	go manager.scheduleLoop()
	return manager
}

func (m *SubtitleManager) Close() {
	close(m.stop)
}

func (m *SubtitleManager) SetConfig(cfg SubtitleRuntimeConfig) {
	cfg = normalizeSubtitleRuntimeConfig(cfg)
	m.mu.Lock()
	m.config = cfg
	m.concurrency = cfg.Concurrency
	shouldStart := m.status.Running && m.activeWorkers < m.concurrency
	m.mu.Unlock()
	if shouldStart {
		m.startWorkers()
	}
}

func (m *SubtitleManager) Config() SubtitleRuntimeConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config
}

func (m *SubtitleManager) Check(ctx context.Context) SubtitleCheck {
	cfg := m.Config()
	return checkSubtitleRuntime(ctx, cfg)
}

func checkSubtitleRuntime(ctx context.Context, cfg SubtitleRuntimeConfig) SubtitleCheck {
	check := SubtitleCheck{
		Mode:        cfg.Mode,
		CLIPath:     resolveSubtitleCLIPath(cfg.CLIPath),
		APIURL:      cfg.APIURL,
		WhisperPath: cfg.WhisperPath,
		FFmpegPath:  cfg.FFmpegPath,
		ModelPath:   cfg.ModelPath,
	}
	switch cfg.Mode {
	case "api":
		if strings.TrimSpace(cfg.APIURL) == "" {
			check.Message = "外部 API 地址未配置"
			return check
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.APIURL, "/")+"/api/check", nil)
		if err != nil {
			check.Message = err.Error()
			return check
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			check.Message = err.Error()
			return check
		}
		defer resp.Body.Close()
		check.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
		if !check.OK {
			check.Message = resp.Status
		}
		return check
	case "scan":
		check.OK = true
		check.Message = "CLI 扫描模式可用：只扫描已存在的同名字幕文件"
		return check
	default:
		cli := resolveSubtitleCLIPath(cfg.CLIPath)
		if !fileExecutable(cli) {
			check.Message = "subtitle-cli 不可用，请检查路径或 fnOS 内置运行时"
			return check
		}
		args := []string{"check"}
		args = appendSubtitleConfigFlags(args, cfg)
		cmd := exec.CommandContext(ctx, cli, args...)
		output, err := cmd.CombinedOutput()
		check.Message = strings.TrimSpace(string(output))
		check.OK = err == nil
		if err != nil && check.Message == "" {
			check.Message = err.Error()
		}
		return check
	}
}

func (m *SubtitleManager) EnqueueImmediate(creatorIDs []int64, source string) (SubtitleStatus, error) {
	jobs, err := m.store.AddSubtitleJobs(creatorIDs, 0, 0, normalizeSubtitleSource(source))
	if err != nil {
		return SubtitleStatus{}, err
	}
	m.mu.Lock()
	m.status.TotalCreators += len(jobs)
	m.status.PendingCreators += len(jobs)
	if len(jobs) > 0 {
		m.status.Running = true
		m.status.State = "queued"
	}
	m.refreshJobsLocked()
	status := m.snapshotLocked()
	m.mu.Unlock()
	m.publish("queued", status, nil)
	m.startWorkers()
	return status, nil
}

func (m *SubtitleManager) EnqueueWork(workID int64, source string) (SubtitleStatus, error) {
	work, err := m.store.Work(workID)
	if err != nil {
		return SubtitleStatus{}, err
	}
	if !workHasVideo(work) {
		return SubtitleStatus{}, errors.New("非视频作品不支持转字幕")
	}
	job, err := m.store.AddSubtitleWorkJob(work.CreatorID, workID, normalizeSubtitleSource(source))
	if err != nil {
		return SubtitleStatus{}, err
	}
	m.mu.Lock()
	m.status.TotalCreators++
	if job.Status == "pending" {
		m.status.PendingCreators++
	}
	m.status.Running = true
	m.status.State = "queued"
	m.refreshJobsLocked()
	status := m.snapshotLocked()
	m.mu.Unlock()
	m.publish("queued", status, &job)
	m.startWorkers()
	return status, nil
}

func (m *SubtitleManager) RetryJob(id int64) (SubtitleStatus, error) {
	job, err := m.store.RetrySubtitleJob(id)
	if err != nil {
		return SubtitleStatus{}, err
	}
	m.mu.Lock()
	m.status.TotalCreators++
	m.status.PendingCreators++
	m.status.Running = true
	m.status.State = "queued"
	m.refreshJobsLocked()
	status := m.snapshotLocked()
	m.mu.Unlock()
	m.publish("retry", status, &job)
	m.startWorkers()
	return status, nil
}

func (m *SubtitleManager) ResetImmediate(ctx context.Context) (SubtitleStatus, error) {
	m.mu.Lock()
	for _, cancel := range m.currentCancels {
		cancel()
	}
	m.currentCancels = map[int64]context.CancelFunc{}
	m.status = SubtitleStatus{State: "idle", Errors: []string{}}
	m.mu.Unlock()
	if err := m.store.ClearSubtitleJobs(); err != nil {
		return SubtitleStatus{}, err
	}
	status := m.Status()
	m.publish("reset", status, nil)
	return status, ctx.Err()
}

func (m *SubtitleManager) Status() SubtitleStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshJobsLocked()
	m.recomputeProgressLocked()
	return m.snapshotLocked()
}

func (m *SubtitleManager) startWorkers() {
	m.mu.Lock()
	for m.activeWorkers < m.concurrency {
		m.activeWorkers++
		go m.run()
	}
	m.mu.Unlock()
}

func (m *SubtitleManager) run() {
	defer func() {
		m.mu.Lock()
		m.activeWorkers--
		m.mu.Unlock()
	}()
	for {
		select {
		case <-m.stop:
			return
		default:
		}
		job, err := m.claimNextPendingJob()
		if errors.Is(err, sql.ErrNoRows) {
			m.mu.Lock()
			m.refreshJobsLocked()
			if len(m.status.RunningJobs) == 0 {
				m.status.Running = false
				m.status.State = "idle"
			}
			status := m.snapshotLocked()
			m.mu.Unlock()
			m.publish("drain", status, nil)
			return
		}
		if err != nil {
			m.recordError(err.Error())
			time.Sleep(500 * time.Millisecond)
			continue
		}
		m.execute(job)
	}
}

func (m *SubtitleManager) claimNextPendingJob() (SubtitleJob, error) {
	job, err := m.store.NextPendingSubtitleJob()
	if err != nil {
		return SubtitleJob{}, err
	}
	if err := m.store.MarkSubtitleJobRunning(job.ID); err != nil {
		return SubtitleJob{}, err
	}
	job, _ = m.store.SubtitleJob(job.ID)
	m.mu.Lock()
	m.status.Running = true
	m.status.State = "running"
	m.status.CurrentCreator = job.CreatorName
	m.refreshJobsLocked()
	status := m.snapshotLocked()
	m.mu.Unlock()
	m.publish("start", status, &job)
	return job, nil
}

func (m *SubtitleManager) execute(job SubtitleJob) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.currentCancels[job.ID] = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.currentCancels, job.ID)
		m.mu.Unlock()
	}()
	count, err := m.processJob(ctx, job)
	statusValue := "succeeded"
	message := ""
	if err != nil {
		statusValue = "failed"
		message = err.Error()
	}
	_ = m.store.FinishSubtitleJob(job.ID, statusValue, count, message)
	updated, _ := m.store.SubtitleJob(job.ID)
	m.mu.Lock()
	if statusValue == "succeeded" {
		m.status.SucceededCreators++
		m.status.SubtitleWorks += count
	} else {
		m.status.FailedCreators++
		if message != "" {
			m.status.Errors = append(m.status.Errors, message)
		}
	}
	if m.status.PendingCreators > 0 {
		m.status.PendingCreators--
	}
	m.refreshJobsLocked()
	m.recomputeProgressLocked()
	status := m.snapshotLocked()
	m.mu.Unlock()
	m.publish(statusValue, status, &updated)
}

func (m *SubtitleManager) processJob(ctx context.Context, job SubtitleJob) (int, error) {
	videos, err := m.store.VideoWorksForCreator(job.CreatorID, job.WorkID)
	if err != nil {
		return 0, err
	}
	if len(videos) == 0 {
		return 0, errors.New("没有可转字幕的视频作品")
	}
	total := len(videos)
	done := 0
	skipped := 0
	scanOnly := m.Config().Mode == "scan"
	outputDir := filepath.Dir(videos[0].FilePath)
	for index, video := range videos {
		select {
		case <-ctx.Done():
			return done, ctx.Err()
		default:
		}
		textPath, srtPath, jsonPath := subtitleOutputPaths(video.FilePath)
		if fileNonEmpty(textPath) {
			skipped++
			done++
			_ = m.store.UpsertSubtitleRecord(video.WorkID, video.MediaID, video.CreatorID, textPath, srtPath, jsonPath, job.Source, "ready", "")
			_ = m.store.UpdateSubtitleJobProgress(job.ID, total, index+1, done, skipped, "已存在字幕，跳过", outputDir)
			continue
		}
		if scanOnly {
			skipped++
			_ = m.store.UpdateSubtitleJobProgress(job.ID, total, index+1, done, skipped, "未发现同名字幕，跳过", outputDir)
			continue
		}
		if err := m.transcribe(ctx, video); err != nil {
			_ = m.store.UpsertSubtitleRecord(video.WorkID, video.MediaID, video.CreatorID, textPath, srtPath, jsonPath, job.Source, "failed", err.Error())
			return done, err
		}
		if !fileNonEmpty(textPath) {
			return done, errors.New("转写完成但没有生成同名 txt 字幕")
		}
		done++
		_ = m.store.UpsertSubtitleRecord(video.WorkID, video.MediaID, video.CreatorID, textPath, srtPath, jsonPath, job.Source, "ready", "")
		_ = m.store.UpdateSubtitleJobProgress(job.ID, total, index+1, done, skipped, "字幕已生成："+video.Title, outputDir)
		updated, _ := m.store.SubtitleJob(job.ID)
		m.mu.Lock()
		m.refreshJobsLocked()
		status := m.snapshotLocked()
		m.mu.Unlock()
		m.publish("progress", status, &updated)
	}
	return done, nil
}

func (m *SubtitleManager) transcribe(ctx context.Context, video SubtitleVideo) error {
	cfg := m.Config()
	switch cfg.Mode {
	case "scan":
		textPath, _, _ := subtitleOutputPaths(video.FilePath)
		if !fileNonEmpty(textPath) {
			return errors.New("CLI 扫描模式下没有找到同名字幕文件")
		}
		return nil
	case "api":
		return transcribeViaAPI(ctx, cfg, video)
	default:
		return transcribeViaCLI(ctx, cfg, video)
	}
}

func transcribeViaCLI(ctx context.Context, cfg SubtitleRuntimeConfig, video SubtitleVideo) error {
	cli := resolveSubtitleCLIPath(cfg.CLIPath)
	if !fileExecutable(cli) {
		return errors.New("subtitle-cli 不可用")
	}
	args := []string{"transcribe", "--video", video.FilePath}
	args = appendSubtitleConfigFlags(args, cfg)
	cmd := exec.CommandContext(ctx, cli, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return errors.New(message)
	}
	return nil
}

func transcribeViaAPI(ctx context.Context, cfg SubtitleRuntimeConfig, video SubtitleVideo) error {
	if strings.TrimSpace(cfg.APIURL) == "" {
		return errors.New("外部字幕 API 地址未配置")
	}
	body, _ := json.Marshal(map[string]string{
		"video_path": video.FilePath,
		"language":   cfg.Language,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.APIURL, "/")+"/api/transcribe", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(resp.Status)
	}
	return nil
}

func (m *SubtitleManager) scheduleLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			m.enqueueDueQueues(now)
		}
	}
}

func (m *SubtitleManager) enqueueDueQueues(now time.Time) {
	queues, err := m.store.SubtitleQueues()
	if err != nil {
		m.recordError(err.Error())
		return
	}
	for _, queue := range queues {
		if !queue.Enabled || strings.TrimSpace(queue.NextRunAt) == "" {
			continue
		}
		next, err := time.Parse(time.RFC3339, queue.NextRunAt)
		if err != nil || now.Before(next) {
			continue
		}
		creators, err := m.store.CreatorsForSubtitleQueue(queue.ID)
		if err != nil {
			m.recordError(err.Error())
			continue
		}
		ids := make([]int64, 0, len(creators))
		for _, creator := range creators {
			ids = append(ids, creator.ID)
		}
		_, _ = m.store.AddSubtitleJobs(ids, queue.ID, 0, "schedule")
		_, _ = m.store.db.Exec(`UPDATE subtitle_queues SET next_run_at=?, last_run_summary=?, updated_at=? WHERE id=?`,
			NextCronRun(queue.Cron, now.Add(time.Minute)).Format(time.RFC3339),
			"已加入立即字幕队列："+strconv.Itoa(len(ids))+" 个UP主",
			time.Now().UTC().Format(time.RFC3339),
			queue.ID,
		)
		m.startWorkers()
	}
}

func (m *SubtitleManager) publish(kind string, status SubtitleStatus, job *SubtitleJob) {
	if m.hub != nil {
		m.hub.PublishAny(SubtitleEvent{Type: kind, Status: status, Job: job})
	}
}

func (m *SubtitleManager) refreshJobsLocked() {
	jobs, _ := m.store.SubtitleJobs(30)
	runningJobs, _ := m.store.RunningSubtitleJobs()
	m.status.Jobs = jobs
	m.status.RunningJobs = runningJobs
}

func (m *SubtitleManager) recomputeProgressLocked() {
	total := m.status.PendingCreators + len(m.status.RunningJobs) + m.status.SucceededCreators + m.status.FailedCreators
	m.status.TotalCreators = total
	if total <= 0 {
		m.status.Progress = 0
		return
	}
	done := m.status.SucceededCreators + m.status.FailedCreators
	m.status.Progress = int(float64(done) / float64(total) * 100)
}

func (m *SubtitleManager) snapshotLocked() SubtitleStatus {
	status := m.status
	status.Errors = append([]string(nil), m.status.Errors...)
	status.RunningJobs = append([]SubtitleJob(nil), m.status.RunningJobs...)
	status.Jobs = append([]SubtitleJob(nil), m.status.Jobs...)
	return status
}

func (m *SubtitleManager) recordError(message string) {
	m.mu.Lock()
	m.status.Errors = append(m.status.Errors, message)
	m.mu.Unlock()
}

func subtitleOutputPaths(videoPath string) (textPath, srtPath, jsonPath string) {
	ext := filepath.Ext(videoPath)
	stem := strings.TrimSuffix(videoPath, ext)
	return stem + ".txt", stem + ".srt", stem + ".json"
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func workHasVideo(work Work) bool {
	for _, media := range work.Media {
		if media.Type == "video" {
			return true
		}
	}
	return false
}

func normalizeSubtitleSource(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "manual"
	}
	return value
}

func resolveSubtitleCLIPath(path string) string {
	if strings.TrimSpace(path) != "" {
		return strings.TrimSpace(path)
	}
	if env := strings.TrimSpace(os.Getenv("LOCALBILIBILI_SUBTITLE_CLI")); env != "" {
		return env
	}
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		for _, candidate := range []string{
			filepath.Join(exeDir, "subtitle-cli"),
			filepath.Join(exeDir, "subtitle-runtime", "subtitle-cli"),
			filepath.Join(exeDir, "..", "tools", "subtitle-cli", "subtitle-cli"),
		} {
			if fileExecutable(candidate) {
				return candidate
			}
		}
		return filepath.Join(exeDir, "subtitle-cli")
	}
	return "subtitle-cli"
}

func appendSubtitleConfigFlags(args []string, cfg SubtitleRuntimeConfig) []string {
	if strings.TrimSpace(cfg.WhisperPath) != "" {
		args = append(args, "-whisper-cli", cfg.WhisperPath)
	}
	if strings.TrimSpace(cfg.FFmpegPath) != "" {
		args = append(args, "-ffmpeg", cfg.FFmpegPath)
	}
	if strings.TrimSpace(cfg.ModelPath) != "" {
		args = append(args, "-model", cfg.ModelPath)
	}
	if strings.TrimSpace(cfg.Language) != "" {
		args = append(args, "-language", cfg.Language)
	}
	if !cfg.GPU {
		args = append(args, "-no-gpu")
	}
	return args
}
