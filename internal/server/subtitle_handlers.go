package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (a *App) handleSubtitleConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	a.mu.RLock()
	cfg := a.config.Subtitle
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"config": subtitleConfigFromRuntime(cfg)})
}

func (a *App) handleSaveSubtitleConfig(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	var body SubtitleConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	cfg := normalizeSubtitleRuntimeConfig(SubtitleRuntimeConfig{
		Mode:        body.Mode,
		Concurrency: body.Concurrency,
		CLIPath:     body.CLIPath,
		APIURL:      body.APIURL,
		WhisperPath: body.WhisperPath,
		FFmpegPath:  body.FFmpegPath,
		ModelPath:   body.ModelPath,
		Language:    body.Language,
		GPU:         body.GPU,
	})
	a.mu.Lock()
	appCfg := a.config
	appCfg.Subtitle = cfg
	appCfg.DatabaseDir = a.databaseDir
	manager := a.subtitles
	a.config = appCfg
	a.mu.Unlock()
	if err := SaveConfig(a.configPath, appCfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if manager != nil {
		manager.SetConfig(cfg)
	}
	_ = a.currentStore().RecordAuditEvent(AuditEventInput{ActorUserID: actor.ID, ActorName: actor.Username, EventType: "update_subtitle_config", TargetType: "config", Message: "更新字幕配置"})
	writeJSON(w, http.StatusOK, map[string]any{"config": subtitleConfigFromRuntime(cfg)})
}

func (a *App) handleCheckSubtitles(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(string(body)) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"check": a.subtitles.Check(r.Context())})
		return
	}
	var input SubtitleConfig
	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	cfg := normalizeSubtitleRuntimeConfig(SubtitleRuntimeConfig{
		Mode:        input.Mode,
		Concurrency: input.Concurrency,
		CLIPath:     input.CLIPath,
		APIURL:      input.APIURL,
		WhisperPath: input.WhisperPath,
		FFmpegPath:  input.FFmpegPath,
		ModelPath:   input.ModelPath,
		Language:    input.Language,
		GPU:         input.GPU,
	})
	writeJSON(w, http.StatusOK, map[string]any{"check": checkSubtitleRuntime(r.Context(), cfg)})
}

func (a *App) handleSubtitleStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": a.subtitles.Status()})
}

func (a *App) handleSubtitleEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ch, unsubscribe := a.subtitleHub.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			_, _ = fmt.Fprintf(w, "event: subtitle\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (a *App) handleSubtitleImmediate(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var body struct {
		CreatorIDs []int64 `json:"creator_ids"`
		WorkIDs    []int64 `json:"work_ids"`
		Source     string  `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	source := body.Source
	if source == "" {
		source = "manual"
	}
	status := SubtitleStatus{}
	var err error
	if len(body.WorkIDs) > 0 {
		for _, workID := range body.WorkIDs {
			status, err = a.subtitles.EnqueueWork(workID, source)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	} else {
		status, err = a.subtitles.EnqueueImmediate(body.CreatorIDs, source)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status})
}

func (a *App) handleResetSubtitleImmediate(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	status, err := a.subtitles.ResetImmediate(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}

func (a *App) handleScanExistingSubtitles(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	if a.subtitles.Config().Mode != "scan" {
		writeError(w, http.StatusBadRequest, "请先切换到 CLI 扫描模式并保存配置")
		return
	}
	ids, err := a.store.CreatorIDsWithVideoWorks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status, err := a.subtitles.EnqueueImmediate(ids, "scan")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status, "creator_count": len(ids)})
}

func (a *App) handleSubtitleJobs(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	page, pageSize := parsePageArgs(r)
	jobs, err := a.store.SubtitleJobsPage(page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (a *App) handleDeleteSubtitleJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if err := a.store.DeleteSubtitleJob(id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "running") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleRetrySubtitleJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	status, err := a.subtitles.RetryJob(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status})
}

func (a *App) handleSubtitleQueues(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	queues, err := a.store.SubtitleQueues()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queues": queues})
}

func (a *App) handleCreateSubtitleQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
		Cron string `json:"cron"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	queue, err := a.store.CreateSubtitleQueue(body.Name, body.Cron)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"queue": queue})
}

func (a *App) handlePatchSubtitleQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct {
		Name      string `json:"name"`
		Cron      string `json:"cron"`
		IsDefault *bool  `json:"is_default"`
		Enabled   *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	queue, err := a.store.UpdateSubtitleQueue(id, body.Name, body.Cron, body.IsDefault, body.Enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queue": queue})
}

func (a *App) handleDeleteSubtitleQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if err := a.store.DeleteSubtitleQueue(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleSubtitleQueueCreators(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	creators, err := a.store.CreatorsForSubtitleQueue(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"creators": creators})
}

func (a *App) handleAddSubtitleQueueCreators(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct {
		CreatorIDs []int64 `json:"creator_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.store.AddCreatorsToSubtitleQueue(id, body.CreatorIDs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleRemoveSubtitleQueueCreator(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	queueID, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	creatorID, ok := parseID(w, r.PathValue("creator_id"))
	if !ok {
		return
	}
	if err := a.store.RemoveCreatorFromSubtitleQueue(queueID, creatorID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleRunSubtitleQueueNow(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	creators, err := a.store.CreatorsForSubtitleQueue(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ids := make([]int64, 0, len(creators))
	for _, creator := range creators {
		ids = append(ids, creator.ID)
	}
	status, err := a.subtitles.EnqueueImmediate(ids, "schedule")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status})
}

func (a *App) handleCreatorSubtitleLink(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	token, err := a.store.SubtitleTokenForCreator(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/public/subtitles/creators/" + strconv.FormatInt(id, 10) + "?token=" + url.QueryEscape(token)})
}

func (a *App) handleWorkSubtitleLink(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	token, err := a.store.SubtitleTokenForWork(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/public/subtitles/works/" + strconv.FormatInt(id, 10) + "?token=" + url.QueryEscape(token)})
}

func (a *App) handlePublicWorkSubtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	title, text, ready, err := a.store.SubtitleTextForWork(id, token)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !ready {
		_, _ = a.subtitles.EnqueueWork(id, "api")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("字幕生成中：" + title))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(text))
}

func (a *App) handlePublicCreatorSubtitles(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	text, ready, err := a.store.SubtitleTextForCreator(id, token)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !ready {
		_, _ = a.subtitles.EnqueueImmediate([]int64{id}, "api")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("字幕生成中"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(text))
}

func subtitleConfigFromRuntime(cfg SubtitleRuntimeConfig) SubtitleConfig {
	cfg = normalizeSubtitleRuntimeConfig(cfg)
	return SubtitleConfig{
		Mode:        cfg.Mode,
		Concurrency: cfg.Concurrency,
		CLIPath:     cfg.CLIPath,
		APIURL:      cfg.APIURL,
		WhisperPath: cfg.WhisperPath,
		FFmpegPath:  cfg.FFmpegPath,
		ModelPath:   cfg.ModelPath,
		Language:    cfg.Language,
		GPU:         cfg.GPU,
	}
}
