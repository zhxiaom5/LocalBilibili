package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func learningConfigFromRuntime(c LearningRuntimeConfig) LearningConfig {
	c = normalizeLearningRuntimeConfig(c)
	return LearningConfig{CodexPath: c.CodexPath, WiseMappingURL: c.WiseMappingURL, WiseMappingUser: c.WiseMappingUser, DefaultCron: c.DefaultCron, PasswordConfigured: learningPasswordConfigured(), WorkPrompt: c.WorkPrompt, CreatorPrompt: c.CreatorPrompt, CredentialMigration: learningCredentialMigrationStatus()}
}
func (a *App) handleLearningConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	a.mu.RLock()
	c := a.config.Learning
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"config": learningConfigFromRuntime(c)})
}
func (a *App) handleSaveLearningConfig(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	var in LearningConfig
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, 400, "invalid json")
		return
	}
	c := normalizeLearningRuntimeConfig(LearningRuntimeConfig{CodexPath: in.CodexPath, WiseMappingURL: in.WiseMappingURL, WiseMappingUser: in.WiseMappingUser, DefaultCron: in.DefaultCron, WorkPrompt: in.WorkPrompt, CreatorPrompt: in.CreatorPrompt})
	if strings.TrimSpace(in.WiseMappingPassword) != "" {
		if err := saveLearningPassword(in.WiseMappingPassword); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	a.mu.Lock()
	cfg := a.config
	cfg.Learning = c
	cfg.DatabaseDir = a.databaseDir
	a.config = cfg
	m := a.learning
	a.mu.Unlock()
	if e := SaveConfig(a.configPath, cfg); e != nil {
		writeError(w, 500, e.Error())
		return
	}
	m.SetConfig(c)
	_ = a.currentStore().RecordAuditEvent(AuditEventInput{ActorUserID: actor.ID, ActorName: actor.Username, EventType: "update_learning_config", TargetType: "config", Message: "更新学习管理配置"})
	writeJSON(w, 200, map[string]any{"config": learningConfigFromRuntime(c)})
}

func (a *App) handleCodexLoginStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"login": a.learning.CodexLoginStatus(r.Context())})
}
func (a *App) handleStartCodexLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	status, err := a.learning.StartCodexLogin()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"login": status})
}
func (a *App) handleCancelCodexLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"login": a.learning.CancelCodexLogin()})
}
func (a *App) handleLogoutCodex(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	if err := a.learning.LogoutCodex(r.Context()); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"login": a.learning.CodexLoginStatus(r.Context())})
}
func (a *App) handleCheckWiseMapping(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	cfg := a.learning.Config()
	password, err := learningWiseMappingPassword()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err = wiseMappingToken(r.Context(), cfg, password)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
func (a *App) handleCheckLearning(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"check": a.learning.Check(r.Context())})
}
func (a *App) handleLearningStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"status": a.learning.Status()})
}
func (a *App) handleLearningImmediate(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var in struct {
		CreatorIDs []int64 `json:"creator_ids"`
		WorkIDs    []int64 `json:"work_ids"`
		Source     string  `json:"source"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, 400, "invalid json")
		return
	}
	var st LearningStatus
	var e error
	for _, id := range in.WorkIDs {
		work, x := a.currentStore().Work(id)
		if x != nil {
			writeError(w, 400, x.Error())
			return
		}
		st, e = a.learning.Enqueue(work.CreatorID, id, 0, in.Source)
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
	}
	for _, id := range in.CreatorIDs {
		st, e = a.learning.Enqueue(id, 0, 0, in.Source)
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": st})
}
func (a *App) handleResetLearning(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	if e := a.learning.Reset(); e != nil {
		writeError(w, 500, e.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": a.learning.Status()})
}
func (a *App) handleLearningJobs(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	v, e := a.currentStore().LearningJobs(50)
	if e != nil {
		writeError(w, 500, e.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"jobs": v})
}
func (a *App) handleLearningRuns(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	v, e := a.currentStore().LearningRuns(50)
	if e != nil {
		writeError(w, 500, e.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"runs": v})
}
func (a *App) handleDeleteLearningJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if e := a.currentStore().DeleteLearningJob(id); e != nil {
		code := 500
		if errors.Is(e, ErrNotFound) {
			code = 404
		}
		writeError(w, code, e.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
func (a *App) handleRetryLearningJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	j, e := a.currentStore().RetryLearningJob(id)
	if e != nil {
		writeError(w, 400, e.Error())
		return
	}
	st, e := a.learning.Enqueue(j.CreatorID, j.WorkID, j.QueueID, "retry")
	if e != nil {
		writeError(w, 400, e.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": st})
}
func (a *App) handleLearningQueues(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	v, e := a.currentStore().LearningQueues()
	if e != nil {
		writeError(w, 500, e.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"queues": v})
}
func (a *App) handleCreateLearningQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
		Cron string `json:"cron"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, 400, "invalid json")
		return
	}
	v, e := a.currentStore().CreateLearningQueue(in.Name, in.Cron)
	if e != nil {
		writeError(w, 400, e.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"queue": v})
}
func (a *App) handleAddLearningQueueCreators(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var in struct {
		CreatorIDs []int64 `json:"creator_ids"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if e := a.currentStore().AddCreatorsToLearningQueue(id, in.CreatorIDs); e != nil {
		writeError(w, 500, e.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
func (a *App) handleRunLearningQueueNow(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	queue, e := a.currentStore().LearningQueue(id)
	if e != nil {
		writeError(w, 404, e.Error())
		return
	}
	cs, e := a.currentStore().CreatorsForLearningQueue(id)
	if e != nil {
		writeError(w, 500, e.Error())
		return
	}
	run, e := a.currentStore().CreateLearningRun(queue, len(cs))
	if e != nil {
		writeError(w, 500, e.Error())
		return
	}
	var st LearningStatus
	for _, c := range cs {
		st, e = a.learning.EnqueueRun(c.ID, 0, id, run.ID, "schedule")
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": st})
}
func (a *App) handleCreatorMindMapLink(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	v, e := a.currentStore().MindMap(id, 0)
	if e == nil && v.MapURL != "" {
		writeJSON(w, 200, map[string]any{"url": v.MapURL})
		return
	}
	c, e := a.currentStore().Creator(id)
	if e != nil {
		writeError(w, 404, e.Error())
		return
	}
	a.mu.RLock()
	base := a.config.Learning.WiseMappingURL
	a.mu.RUnlock()
	if base == "" {
		writeError(w, 404, "该UP主尚未生成思维导图")
		return
	}
	writeJSON(w, 200, map[string]any{"url": strings.TrimRight(base, "/") + "/c/maps?label=" + url.QueryEscape("UP主名-"+c.Name)})
}
func (a *App) handleWorkMindMapLink(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); !ok {
		writeError(w, 401, "unauthorized")
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	work, e := a.currentStore().Work(id)
	if e != nil {
		writeError(w, 404, e.Error())
		return
	}
	v, e := a.currentStore().MindMap(work.CreatorID, id)
	if e != nil {
		writeError(w, 404, "该作品尚未生成思维导图")
		return
	}
	writeJSON(w, 200, map[string]any{"url": v.MapURL, "creator_url": "/api/learning/links/creators/" + strconv.FormatInt(work.CreatorID, 10)})
}
