package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type App struct {
	mu           sync.RWMutex
	store        *Store
	scanner      *Scanner
	hub          *EventHub
	updater      *UpdateManager
	updateHub    *EventHub
	subtitles    *SubtitleManager
	subtitleHub  *EventHub
	learning     *LearningManager
	learningHub  *EventHub
	browser      FileBrowser
	web          WebAssets
	accessLog    *AccessLogger
	configPath   string
	config       AppConfig
	databaseDir  string
	databasePath string
}

type contextKey string

const currentUserContextKey contextKey = "current-user"

const currentViewUserContextKey contextKey = "current-view-user"
const viewUserCookieName = "localbilibili_view_user"

type WebAssets struct {
	Dir string
	FS  fs.FS
}

func NewApp(dbPath string, webDir string) (*App, error) {
	return NewAppWithAssets(dbPath, WebAssets{Dir: webDir})
}

func NewAppWithAssets(dbPath string, web WebAssets) (*App, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	hub := NewEventHub()
	updateHub := NewEventHub()
	subtitleHub := NewEventHub()
	learningHub := NewEventHub()
	cleanDB := filepath.Clean(dbPath)
	logger, err := NewAccessLogger(accessLogDir(filepath.Dir(cleanDB)))
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	subtitleCfg := DefaultSubtitleRuntimeConfig()
	learningCfg := DefaultLearningRuntimeConfig()
	return &App{
		store:        store,
		scanner:      NewScanner(store, hub),
		hub:          hub,
		updater:      NewUpdateManager(store, updateHub, nil, DefaultDownloaderConcurrency),
		updateHub:    updateHub,
		subtitles:    NewSubtitleManager(store, subtitleHub, subtitleCfg),
		subtitleHub:  subtitleHub,
		learning:     NewLearningManager(store, learningHub, learningCfg),
		learningHub:  learningHub,
		browser:      NewFileBrowserFromEnv(),
		web:          web,
		accessLog:    logger,
		config:       AppConfig{DatabaseDir: filepath.Dir(cleanDB), DownloaderConcurrency: DefaultDownloaderConcurrency, Subtitle: subtitleCfg, Learning: learningCfg},
		databaseDir:  filepath.Dir(cleanDB),
		databasePath: cleanDB,
	}, nil
}

func NewConfiguredApp(configPath string, defaultDatabaseDir string, webDir string) (*App, error) {
	return NewConfiguredAppWithAssets(configPath, defaultDatabaseDir, WebAssets{Dir: webDir})
}

func NewConfiguredAppWithAssets(configPath string, defaultDatabaseDir string, web WebAssets) (*App, error) {
	cfg, err := LoadConfig(configPath, defaultDatabaseDir)
	if err != nil {
		return nil, err
	}
	if err := migrateLearningCredentialsOnce(configPath, &cfg); err != nil {
		return nil, err
	}
	dbInfo, err := InspectDatabaseDirectory(cfg.DatabaseDir)
	if err != nil {
		return nil, err
	}
	app, err := NewAppWithAssets(dbInfo.Path, web)
	if err != nil {
		return nil, err
	}
	app.configPath = configPath
	app.config = cfg
	app.databaseDir = dbInfo.Directory
	app.databasePath = dbInfo.Path
	app.updater.SetConcurrency(cfg.DownloaderConcurrency)
	app.subtitles.SetConfig(cfg.Subtitle)
	app.learning.SetConfig(cfg.Learning)
	if err := SaveConfig(configPath, AppConfig{DatabaseDir: dbInfo.Directory, DownloaderConcurrency: cfg.DownloaderConcurrency, Subtitle: cfg.Subtitle, Learning: cfg.Learning}); err != nil {
		_ = app.Close()
		return nil, err
	}
	return app, nil
}

func (a *App) Close() error {
	a.mu.RLock()
	updater := a.updater
	subtitles := a.subtitles
	learning := a.learning
	accessLog := a.accessLog
	store := a.store
	a.mu.RUnlock()
	if updater != nil {
		updater.Close()
	}
	if subtitles != nil {
		subtitles.Close()
	}
	if learning != nil {
		learning.Close()
	}
	if accessLog != nil {
		_ = accessLog.Close()
	}
	return store.Close()
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("GET /public/subtitles/creators/{id}", a.handlePublicCreatorSubtitles)
	mux.HandleFunc("GET /public/subtitles/works/{id}", a.handlePublicWorkSubtitle)
	mux.Handle("/api/", a.authMiddleware(http.HandlerFunc(a.handleAuthedRoutes)))
	mux.Handle("/", http.HandlerFunc(a.handleWeb))
	if a.accessLog != nil {
		return a.accessLog.Middleware(cors(mux))
	}
	return cors(mux)
}

func (a *App) currentStore() *Store {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.store
}

func (a *App) handleAuthedRoutes(w http.ResponseWriter, r *http.Request) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/me", a.handleMe)
	mux.HandleFunc("GET /api/users/view-context", a.handleViewContext)
	mux.HandleFunc("PUT /api/users/view-context", a.handleSetViewContext)
	mux.HandleFunc("DELETE /api/users/view-context", a.handleClearViewContext)
	mux.HandleFunc("GET /api/about", a.handleAbout)
	mux.HandleFunc("GET /api/logs/audit", a.handleAuditLogs)
	mux.HandleFunc("GET /api/logs/system", a.handleSystemLogs)
	mux.HandleFunc("GET /api/diagnostics", a.handleDiagnostics)
	mux.HandleFunc("GET /api/fs/roots", a.handleFSRoots)
	mux.HandleFunc("GET /api/fs/list", a.handleFSList)
	mux.HandleFunc("GET /api/config/database", a.handleDatabaseConfig)
	mux.HandleFunc("PUT /api/config/database", a.handleUpdateDatabaseConfig)
	mux.HandleFunc("GET /api/config/runtime", a.handleRuntimeConfig)
	mux.HandleFunc("PUT /api/config/runtime", a.handleUpdateRuntimeConfig)
	mux.HandleFunc("GET /api/directories", a.handleDirectories)
	mux.HandleFunc("POST /api/directories", a.handleCreateDirectory)
	mux.HandleFunc("DELETE /api/directories/{id}", a.handleDeleteDirectory)
	mux.HandleFunc("POST /api/directories/{id}/scan", a.handleScanDirectory)
	mux.HandleFunc("POST /api/directories/scan-all", a.handleScanAll)
	mux.HandleFunc("GET /api/events", a.handleEvents)
	mux.HandleFunc("GET /api/scan/status", a.handleScanStatus)
	mux.HandleFunc("GET /api/scan/events", a.handleScanEvents)
	mux.HandleFunc("GET /api/creators", a.handleCreators)
	mux.HandleFunc("DELETE /api/creators/{id}", a.handleDeleteCreator)
	mux.HandleFunc("GET /api/creators/{id}/avatar", a.handleCreatorAvatar)
	mux.HandleFunc("GET /api/creators/{id}/links", a.handleCreatorLinks)
	mux.HandleFunc("PUT /api/creators/{id}/links", a.handleSaveCreatorLinks)
	mux.HandleFunc("POST /api/creators/resolve-placeholders", a.handleResolveCreatorPlaceholders)
	mux.HandleFunc("POST /api/creators/bulk-import/preview", a.handlePreviewBulkImportCreators)
	mux.HandleFunc("POST /api/creators/bulk-import", a.handleBulkImportCreators)
	mux.HandleFunc("GET /api/feed", a.handleFeed)
	mux.HandleFunc("GET /api/update/status", a.handleUpdateStatus)
	mux.HandleFunc("GET /api/update/events", a.handleUpdateEvents)
	mux.HandleFunc("POST /api/update/immediate", a.handleUpdateImmediate)
	mux.HandleFunc("POST /api/update/immediate/reset", a.handleResetUpdateImmediate)
	mux.HandleFunc("GET /api/update/jobs", a.handleUpdateJobs)
	mux.HandleFunc("DELETE /api/update/jobs/{id}", a.handleDeleteUpdateJob)
	mux.HandleFunc("POST /api/update/jobs/{id}/retry", a.handleRetryUpdateJob)
	mux.HandleFunc("GET /api/update/queues", a.handleUpdateQueues)
	mux.HandleFunc("POST /api/update/queues", a.handleCreateUpdateQueue)
	mux.HandleFunc("PATCH /api/update/queues/{id}", a.handlePatchUpdateQueue)
	mux.HandleFunc("DELETE /api/update/queues/{id}", a.handleDeleteUpdateQueue)
	mux.HandleFunc("GET /api/update/queues/{id}/creators", a.handleUpdateQueueCreators)
	mux.HandleFunc("POST /api/update/queues/{id}/creators", a.handleAddUpdateQueueCreators)
	mux.HandleFunc("DELETE /api/update/queues/{id}/creators/{creator_id}", a.handleRemoveUpdateQueueCreator)
	mux.HandleFunc("POST /api/update/queues/{id}/run-now", a.handleRunUpdateQueueNow)
	mux.HandleFunc("GET /api/update/cookie", a.handleUpdateCookie)
	mux.HandleFunc("PATCH /api/update/cookie", a.handleSaveUpdateCookie)
	mux.HandleFunc("POST /api/update/cookie/validate", a.handleValidateUpdateCookie)
	mux.HandleFunc("GET /api/subtitles/config", a.handleSubtitleConfig)
	mux.HandleFunc("PUT /api/subtitles/config", a.handleSaveSubtitleConfig)
	mux.HandleFunc("POST /api/subtitles/check", a.handleCheckSubtitles)
	mux.HandleFunc("GET /api/subtitles/status", a.handleSubtitleStatus)
	mux.HandleFunc("GET /api/subtitles/events", a.handleSubtitleEvents)
	mux.HandleFunc("POST /api/subtitles/immediate", a.handleSubtitleImmediate)
	mux.HandleFunc("POST /api/subtitles/immediate/reset", a.handleResetSubtitleImmediate)
	mux.HandleFunc("POST /api/subtitles/scan-existing", a.handleScanExistingSubtitles)
	mux.HandleFunc("GET /api/subtitles/jobs", a.handleSubtitleJobs)
	mux.HandleFunc("DELETE /api/subtitles/jobs/{id}", a.handleDeleteSubtitleJob)
	mux.HandleFunc("POST /api/subtitles/jobs/{id}/retry", a.handleRetrySubtitleJob)
	mux.HandleFunc("GET /api/subtitles/queues", a.handleSubtitleQueues)
	mux.HandleFunc("POST /api/subtitles/queues", a.handleCreateSubtitleQueue)
	mux.HandleFunc("PATCH /api/subtitles/queues/{id}", a.handlePatchSubtitleQueue)
	mux.HandleFunc("DELETE /api/subtitles/queues/{id}", a.handleDeleteSubtitleQueue)
	mux.HandleFunc("GET /api/subtitles/queues/{id}/creators", a.handleSubtitleQueueCreators)
	mux.HandleFunc("POST /api/subtitles/queues/{id}/creators", a.handleAddSubtitleQueueCreators)
	mux.HandleFunc("DELETE /api/subtitles/queues/{id}/creators/{creator_id}", a.handleRemoveSubtitleQueueCreator)
	mux.HandleFunc("POST /api/subtitles/queues/{id}/run-now", a.handleRunSubtitleQueueNow)
	mux.HandleFunc("GET /api/subtitles/links/creators/{id}", a.handleCreatorSubtitleLink)
	mux.HandleFunc("GET /api/subtitles/links/works/{id}", a.handleWorkSubtitleLink)
	mux.HandleFunc("GET /api/learning/config", a.handleLearningConfig)
	mux.HandleFunc("PUT /api/learning/config", a.handleSaveLearningConfig)
	mux.HandleFunc("POST /api/learning/check", a.handleCheckLearning)
	mux.HandleFunc("POST /api/learning/wisemapping/check", a.handleCheckWiseMapping)
	mux.HandleFunc("GET /api/learning/codex/login", a.handleCodexLoginStatus)
	mux.HandleFunc("POST /api/learning/codex/login", a.handleStartCodexLogin)
	mux.HandleFunc("POST /api/learning/codex/login/cancel", a.handleCancelCodexLogin)
	mux.HandleFunc("POST /api/learning/codex/logout", a.handleLogoutCodex)
	mux.HandleFunc("GET /api/learning/status", a.handleLearningStatus)
	mux.HandleFunc("POST /api/learning/immediate", a.handleLearningImmediate)
	mux.HandleFunc("POST /api/learning/immediate/reset", a.handleResetLearning)
	mux.HandleFunc("GET /api/learning/jobs", a.handleLearningJobs)
	mux.HandleFunc("GET /api/learning/runs", a.handleLearningRuns)
	mux.HandleFunc("DELETE /api/learning/jobs/{id}", a.handleDeleteLearningJob)
	mux.HandleFunc("POST /api/learning/jobs/{id}/retry", a.handleRetryLearningJob)
	mux.HandleFunc("GET /api/learning/queues", a.handleLearningQueues)
	mux.HandleFunc("POST /api/learning/queues", a.handleCreateLearningQueue)
	mux.HandleFunc("POST /api/learning/queues/{id}/creators", a.handleAddLearningQueueCreators)
	mux.HandleFunc("POST /api/learning/queues/{id}/run-now", a.handleRunLearningQueueNow)
	mux.HandleFunc("GET /api/learning/links/creators/{id}", a.handleCreatorMindMapLink)
	mux.HandleFunc("GET /api/learning/links/works/{id}", a.handleWorkMindMapLink)
	mux.HandleFunc("GET /api/favorite-folders", a.handleFavoriteFolders)
	mux.HandleFunc("POST /api/favorite-folders", a.handleCreateFavoriteFolder)
	mux.HandleFunc("PATCH /api/favorite-folders/{id}", a.handleUpdateFavoriteFolder)
	mux.HandleFunc("GET /api/favorites/status", a.handleFavoriteStatus)
	mux.HandleFunc("GET /api/favorites/works", a.handleFavoriteWorks)
	mux.HandleFunc("POST /api/favorites/works", a.handleAddWorkFavorite)
	mux.HandleFunc("DELETE /api/favorites/works/{id}", a.handleRemoveWorkFavorite)
	mux.HandleFunc("GET /api/favorites/creators", a.handleFavoriteCreators)
	mux.HandleFunc("POST /api/favorites/creators", a.handleAddCreatorFavorite)
	mux.HandleFunc("DELETE /api/favorites/creators/{id}", a.handleRemoveCreatorFavorite)
	mux.HandleFunc("GET /api/works/{id}", a.handleWork)
	mux.HandleFunc("GET /api/works/{id}/cover", a.handleWorkCover)
	mux.HandleFunc("POST /api/works/{id}/view", a.handleRecordWorkView)
	mux.HandleFunc("GET /api/media/{id}", a.handleMedia)
	mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
	mux.HandleFunc("POST /api/auth/password", a.handleChangePassword)
	mux.HandleFunc("GET /api/users", a.handleUsers)
	mux.HandleFunc("POST /api/users", a.handleCreateUser)
	mux.HandleFunc("POST /api/users/{id}/reset-password", a.handleResetUserPassword)
	mux.HandleFunc("DELETE /api/users/{id}", a.handleDeleteUser)
	mux.ServeHTTP(w, r)
}

func (a *App) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.currentStore().SessionFromRequest(r)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), currentUserContextKey, user)
		viewUser := user
		if user.IsSuperAdmin {
			if cookie, err := r.Cookie(viewUserCookieName); err == nil {
				if id, parseErr := strconv.ParseInt(strings.TrimSpace(cookie.Value), 10, 64); parseErr == nil && id > 0 {
					if candidate, userErr := a.currentStore().User(id); userErr == nil {
						viewUser = candidate
					}
				}
			}
		}
		ctx = context.WithValue(ctx, currentViewUserContextKey, viewUser)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *App) currentUser(r *http.Request) (User, bool) {
	user, ok := r.Context().Value(currentUserContextKey).(User)
	return user, ok
}

func (a *App) currentViewUser(r *http.Request) (User, bool) {
	user, ok := r.Context().Value(currentViewUserContextKey).(User)
	if ok {
		return user, true
	}
	return a.currentUser(r)
}

func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) (User, bool) {
	user, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return User{}, false
	}
	if !user.IsAdmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return User{}, false
	}
	return user, true
}

func (a *App) requireSuperAdmin(w http.ResponseWriter, r *http.Request) (User, bool) {
	user, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return User{}, false
	}
	if !user.IsSuperAdmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return User{}, false
	}
	return user, true
}

func (a *App) requireUpdater(w http.ResponseWriter, r *http.Request) (User, bool) {
	user, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return User{}, false
	}
	if !user.CanUpdate {
		writeError(w, http.StatusForbidden, "forbidden")
		return User{}, false
	}
	return user, true
}

func (a *App) handleFSRoots(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"roots": a.browser.Roots()})
}

func (a *App) handleFSList(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	entries, err := a.browser.List(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (a *App) handleDirectories(w http.ResponseWriter, r *http.Request) {
	dirs, err := a.currentStore().Directories()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"directories": dirs})
}

func (a *App) handleCreateDirectory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.browser.Validate(body.Path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, err := a.currentStore().SaveDirectory(body.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"directory": dir})
}

func (a *App) handleDeleteDirectory(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if err := a.currentStore().DeleteDirectory(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleScanDirectory(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if _, err := a.currentStore().Directory(id); err != nil {
		status := http.StatusNotFound
		if !errors.Is(err, sql.ErrNoRows) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": a.scanner.Enqueue(id)})
}

func (a *App) handleScanAll(w http.ResponseWriter, r *http.Request) {
	dirs, err := a.currentStore().Directories()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ids := make([]int64, 0, len(dirs))
	for _, dir := range dirs {
		ids = append(ids, dir.ID)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": a.scanner.Enqueue(ids...)})
}

func (a *App) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": a.scanner.Status()})
}

func (a *App) handleScanEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ch, unsubscribe := a.hub.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			_, _ = fmt.Fprintf(w, "event: scan\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

type realtimeEventMessage struct {
	name    string
	payload []byte
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
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

	events := make(chan realtimeEventMessage, 16)
	scanCh, scanUnsubscribe := a.hub.Subscribe()
	defer scanUnsubscribe()
	bridgeRealtimeEvents(r.Context(), events, "scan", scanCh)
	if user.IsAdmin {
		updateCh, updateUnsubscribe := a.updateHub.Subscribe()
		defer updateUnsubscribe()
		subtitleCh, subtitleUnsubscribe := a.subtitleHub.Subscribe()
		defer subtitleUnsubscribe()
		bridgeRealtimeEvents(r.Context(), events, "update", updateCh)
		bridgeRealtimeEvents(r.Context(), events, "subtitle", subtitleCh)
		if a.learningHub != nil {
			learningCh, learningUnsubscribe := a.learningHub.Subscribe()
			defer learningUnsubscribe()
			bridgeRealtimeEvents(r.Context(), events, "learning", learningCh)
		}
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, event.payload)
			flusher.Flush()
		}
	}
}

func bridgeRealtimeEvents(ctx context.Context, out chan<- realtimeEventMessage, name string, source <-chan []byte) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case payload, ok := <-source:
				if !ok {
					return
				}
				select {
				case <-ctx.Done():
					return
				case out <- realtimeEventMessage{name: name, payload: payload}:
				}
			}
		}
	}()
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	store := a.currentStore()
	user, err := store.Authenticate(body.Username, body.Password)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	token, err := store.CreateSession(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.SetCookie(w, store.SessionCookie(token))
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	store := a.currentStore()
	if err := store.MustDeleteSession(r); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.SetCookie(w, store.ExpiredSessionCookie())
	http.SetCookie(w, &http.Cookie{Name: viewUserCookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	updated, err := a.currentStore().ChangePassword(user.ID, body.OldPassword, body.NewPassword)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": updated})
}

func (a *App) handleUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireSuperAdmin(w, r); !ok {
		return
	}
	users, err := a.currentStore().Users()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (a *App) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireSuperAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	created, err := a.currentStore().CreateUser(body.Username, body.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = a.currentStore().RecordAuditEvent(AuditEventInput{ActorUserID: actor.ID, ActorName: actor.Username, EventType: "create_user", TargetType: "user", TargetID: created.ID, Message: "创建用户 " + created.Username})
	writeJSON(w, http.StatusCreated, map[string]any{"user": created})
}

func (a *App) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireSuperAdmin(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	updated, err := a.currentStore().ResetUserPassword(id, body.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = a.currentStore().RecordAuditEvent(AuditEventInput{ActorUserID: actor.ID, ActorName: actor.Username, EventType: "reset_password", TargetType: "user", TargetID: updated.ID, Message: "重置用户密码 " + updated.Username})
	writeJSON(w, http.StatusOK, map[string]any{"user": updated})
}

func (a *App) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireSuperAdmin(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if id == actor.ID {
		writeError(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	if err := a.currentStore().DeleteUser(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = a.currentStore().RecordAuditEvent(AuditEventInput{ActorUserID: actor.ID, ActorName: actor.Username, EventType: "delete_user", TargetType: "user", TargetID: id, Message: "删除用户"})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleViewContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireSuperAdmin(w, r)
	if !ok {
		return
	}
	viewUser, _ := a.currentViewUser(r)
	writeJSON(w, http.StatusOK, ViewContext{ActorUser: actor, ViewUser: viewUser})
}

func (a *App) handleSetViewContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireSuperAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		UserID int64 `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	target, err := a.currentStore().User(body.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     viewUserCookieName,
		Value:    strconv.FormatInt(target.ID, 10),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionLifetime.Seconds()),
	})
	_ = a.currentStore().RecordAuditEvent(AuditEventInput{ActorUserID: actor.ID, ActorName: actor.Username, EventType: "set_view_context", TargetType: "user", TargetID: target.ID, Message: "切换查看视角到 " + target.Username})
	writeJSON(w, http.StatusOK, ViewContext{ActorUser: actor, ViewUser: target})
}

func (a *App) handleClearViewContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireSuperAdmin(w, r)
	if !ok {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: viewUserCookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	_ = a.currentStore().RecordAuditEvent(AuditEventInput{ActorUserID: actor.ID, ActorName: actor.Username, EventType: "clear_view_context", TargetType: "user", TargetID: actor.ID, Message: "清除查看视角"})
	writeJSON(w, http.StatusOK, ViewContext{ActorUser: actor, ViewUser: actor})
}

func (a *App) handleAbout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"about": About(a.currentStore().Path())})
}

func (a *App) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireSuperAdmin(w, r); !ok {
		return
	}
	page, pageSize := parsePageArgs(r)
	events, err := a.currentStore().AuditEvents(page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (a *App) handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireSuperAdmin(w, r); !ok {
		return
	}
	page, pageSize := parsePageArgs(r)
	events, err := a.currentStore().SystemEvents(page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (a *App) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	viewUser, _ := a.currentViewUser(r)
	database, err := InspectDatabaseDirectory(a.databaseDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	directories, err := a.currentStore().Directories()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cookie, _ := a.currentStore().UpdateCookie()
	playback, _ := a.currentStore().PlaybackDiagnostics(20)
	diagnostics := Diagnostics{
		About:           About(a.currentStore().Path()),
		CurrentUser:     currentUser,
		CurrentViewUser: viewUser,
		Database:        database,
		ConfiguredDirs:  directories,
		CookieStatus:    cookie,
		ScanStatus:      a.scanner.Status(),
		UpdateStatus:    a.updater.Status(),
		AccessibleRoots: a.browser.Roots(),
		Playback:        playback,
	}
	if len(diagnostics.ScanStatus.Errors) > 0 {
		diagnostics.LatestScanError = diagnostics.ScanStatus.Errors[len(diagnostics.ScanStatus.Errors)-1]
	}
	if len(diagnostics.UpdateStatus.Errors) > 0 {
		diagnostics.LatestUpdateError = diagnostics.UpdateStatus.Errors[len(diagnostics.UpdateStatus.Errors)-1]
	}
	writeJSON(w, http.StatusOK, map[string]any{"diagnostics": diagnostics})
}

func (a *App) handleDatabaseConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireSuperAdmin(w, r); !ok {
		return
	}
	database, err := InspectDatabaseDirectory(a.databaseDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"database": database})
}

func (a *App) handleUpdateDatabaseConfig(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireSuperAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Directory string `json:"directory"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	dbInfo, err := InspectDatabaseDirectory(body.Directory)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !dbInfo.Writable {
		writeError(w, http.StatusBadRequest, "database directory is not writable")
		return
	}
	replacementStore, err := OpenStore(dbInfo.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.mu.RLock()
	currentConfig := a.config
	a.mu.RUnlock()
	currentConfig.DatabaseDir = dbInfo.Directory
	if err := SaveConfig(a.configPath, currentConfig); err != nil {
		_ = replacementStore.Close()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	newHub := NewEventHub()
	newUpdateHub := NewEventHub()
	newSubtitleHub := NewEventHub()
	newLearningHub := NewEventHub()
	newScanner := NewScanner(replacementStore, newHub)
	newUpdater := NewUpdateManager(replacementStore, newUpdateHub, nil, currentConfig.DownloaderConcurrency)
	newSubtitles := NewSubtitleManager(replacementStore, newSubtitleHub, currentConfig.Subtitle)
	newLearning := NewLearningManager(replacementStore, newLearningHub, currentConfig.Learning)
	a.mu.Lock()
	oldStore := a.store
	oldUpdater := a.updater
	oldSubtitles := a.subtitles
	oldLearning := a.learning
	a.store = replacementStore
	a.scanner = newScanner
	a.hub = newHub
	a.updateHub = newUpdateHub
	a.updater = newUpdater
	a.subtitleHub = newSubtitleHub
	a.subtitles = newSubtitles
	a.learningHub = newLearningHub
	a.learning = newLearning
	a.config = currentConfig
	a.databaseDir = dbInfo.Directory
	a.databasePath = dbInfo.Path
	a.mu.Unlock()
	if oldUpdater != nil {
		oldUpdater.Close()
	}
	if oldSubtitles != nil {
		oldSubtitles.Close()
	}
	if oldLearning != nil {
		oldLearning.Close()
	}
	if oldStore != nil {
		_ = oldStore.Close()
	}
	_ = a.currentStore().RecordAuditEvent(AuditEventInput{ActorUserID: actor.ID, ActorName: actor.Username, EventType: "switch_database", TargetType: "database", Message: "切换数据库目录到 " + dbInfo.Directory})
	writeJSON(w, http.StatusOK, map[string]any{"database": dbInfo})
}

func (a *App) handleRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireSuperAdmin(w, r); !ok {
		return
	}
	a.mu.RLock()
	cfg := a.config.Runtime()
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"runtime": cfg})
}

func (a *App) handleUpdateRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireSuperAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		DownloaderConcurrency int `json:"downloader_concurrency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	concurrency := normalizeDownloaderConcurrency(body.DownloaderConcurrency)
	a.mu.Lock()
	cfg := a.config
	cfg.DownloaderConcurrency = concurrency
	cfg.DatabaseDir = a.databaseDir
	updater := a.updater
	a.config = cfg
	a.mu.Unlock()
	if err := SaveConfig(a.configPath, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if updater != nil {
		updater.SetConcurrency(concurrency)
	}
	_ = a.currentStore().RecordAuditEvent(AuditEventInput{ActorUserID: actor.ID, ActorName: actor.Username, EventType: "update_runtime_config", TargetType: "config", Message: "更新运行配置"})
	writeJSON(w, http.StatusOK, map[string]any{"runtime": cfg.Runtime()})
}

func (a *App) handleCreators(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	opts := CreatorListOptions{
		Search: query.Get("search"),
		Sort:   query.Get("sort"),
		Order:  query.Get("order"),
	}
	if raw := strings.TrimSpace(query.Get("min_work_count")); raw != "" {
		min, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || min < 0 {
			writeError(w, http.StatusBadRequest, "invalid min_work_count")
			return
		}
		opts.MinWorkCount = &min
	}
	creators, err := a.currentStore().CreatorsWithOptions(opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"creators": creators})
}

func (a *App) handleCreatorAvatar(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	path, err := a.currentStore().CreatorAvatarFile(id)
	if err != nil {
		status := http.StatusNotFound
		if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, os.ErrNotExist) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err.Error())
		return
	}
	http.ServeFile(w, r, path)
}

func (a *App) handleDeleteCreator(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct {
		DeleteMedia bool `json:"delete_media"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.currentStore().DeleteCreator(id, body.DeleteMedia); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	mediaScope := "仅删除索引"
	if body.DeleteMedia {
		mediaScope = "删除索引和媒体目录"
	}
	_ = a.currentStore().RecordAuditEvent(AuditEventInput{ActorUserID: actor.ID, ActorName: actor.Username, EventType: "delete_creator", TargetType: "creator", TargetID: id, Message: "删除UP主：" + mediaScope})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleCreatorLinks(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	links, err := a.currentStore().CreatorLinks(id)
	if err != nil {
		status := http.StatusNotFound
		if !errors.Is(err, sql.ErrNoRows) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, links)
}

func (a *App) handleSaveCreatorLinks(w http.ResponseWriter, r *http.Request) {
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
	if err := a.currentStore().SetCreatorLinks(id, body.CreatorIDs); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	links, err := a.currentStore().CreatorLinks(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, links)
}

func (a *App) handlePreviewBulkImportCreators(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var body struct {
		DirectoryID int64  `json:"directory_id"`
		Text        string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.DirectoryID <= 0 {
		writeError(w, http.StatusBadRequest, "directory_id is required")
		return
	}
	if _, err := a.store.Directory(body.DirectoryID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	config := BulkImportPreviewConfig{}
	if resolver, ok := a.updater.downloader.(creatorProfileResolver); ok {
		config.Resolver = resolver
		config.Cookie, _ = a.store.updateCookieValue()
	}
	items := PreviewBulkImportCreators(r.Context(), a.store, body.DirectoryID, body.Text, redirectResolvingClient(), config)
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *App) handleBulkImportCreators(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var body struct {
		DirectoryID       int64    `json:"directory_id"`
		BilibiliSpaceURLs []string `json:"bilibili_space_urls"`
		LegacyProfileURLs []string `json:"profile_urls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.DirectoryID <= 0 {
		writeError(w, http.StatusBadRequest, "directory_id is required")
		return
	}
	profileURLs := body.BilibiliSpaceURLs
	if len(profileURLs) == 0 {
		profileURLs = body.LegacyProfileURLs
	}
	if len(profileURLs) == 0 {
		writeError(w, http.StatusBadRequest, "bilibili_space_urls is required")
		return
	}
	profiles, items := a.resolveBulkImportProfiles(r.Context(), profileURLs)
	creators, err := a.store.ImportResolvedCreatorProfiles(body.DirectoryID, profiles)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"creators": creators, "items": items})
}

func (a *App) handleResolveCreatorPlaceholders(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	creators, err := a.store.PlaceholderCreators(200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	profileURLs := make([]string, 0, len(creators))
	for _, creator := range creators {
		profileURL := strings.TrimSpace(creator.BilibiliProfileURL)
		if profileURL == "" && strings.TrimSpace(creator.BilibiliSecUID) != "" {
			profileURL = "https://www.bilibili.com/user/" + strings.TrimSpace(creator.BilibiliSecUID)
		}
		if profileURL != "" {
			profileURLs = append(profileURLs, profileURL)
		}
	}
	profiles, items := a.resolveBulkImportProfiles(r.Context(), profileURLs)
	updatedByID := map[int64]Creator{}
	for _, creator := range creators {
		updatedByID[creator.ID] = creator
	}
	if len(profiles) > 0 {
		for _, profile := range profiles {
			secUID := strings.TrimSpace(profile.SecUID)
			profileURL := strings.TrimSpace(profile.ProfileURL)
			for _, creator := range creators {
				if creator.BilibiliSecUID == secUID || creator.BilibiliProfileURL == profileURL {
					next, err := promoteCreatorProfile(a.store, creator, profile)
					if err == nil {
						updatedByID[creator.ID] = next
					}
				}
			}
		}
	}
	updated := make([]Creator, 0, len(updatedByID))
	for _, creator := range updatedByID {
		if !isLikelyPlaceholderCreator(creator) {
			updated = append(updated, creator)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"creators": updated, "items": items})
}

func (a *App) resolveBulkImportProfiles(ctx context.Context, profileURLs []string) ([]CreatorProfile, []BulkImportItem) {
	resolver, ok := a.updater.downloader.(creatorProfileResolver)
	cookie, _ := a.store.updateCookieValue()
	profiles := []CreatorProfile{}
	items := []BulkImportItem{}
	seen := map[string]bool{}
	for _, raw := range profileURLs {
		profileURL, secUID, err := normalizeBilibiliProfileURL(ctx, raw, redirectResolvingClient())
		if err != nil {
			items = append(items, BulkImportItem{Input: raw, Status: "error", ResolveStatus: "failed", Error: err.Error()})
			continue
		}
		if seen[profileURL] {
			continue
		}
		seen[profileURL] = true
		item := BulkImportItem{Input: raw, ProfileURL: profileURL, SecUID: secUID, Status: "error", ResolveStatus: "failed"}
		if !ok {
			item.Error = "未能解析B站UP主资料，请检查 Cookie 或主页链接: 当前下载器不支持资料解析"
			items = append(items, item)
			continue
		}
		profile, err := resolveBulkImportProfile(ctx, resolver, profileURL, cookie)
		if err != nil {
			item.Error = err.Error()
			items = append(items, item)
			continue
		}
		item.Status = "ready"
		item.ResolveStatus = "resolved"
		item.Error = ""
		item.UID = profile.UID
		item.SecUID = firstNonEmpty(profile.SecUID, secUID)
		item.ProfileURL = firstNonEmpty(profile.ProfileURL, profileURL)
		item.DisplayName = profile.Name
		item.AvatarURL = profile.AvatarURL
		item.Bio = profile.Bio
		profiles = append(profiles, profile)
		items = append(items, item)
	}
	return profiles, items
}

func (a *App) handleFeed(w http.ResponseWriter, r *http.Request) {
	creatorID, _ := strconv.ParseInt(r.URL.Query().Get("creator_id"), 10, 64)
	directoryID, _ := strconv.ParseInt(r.URL.Query().Get("directory_id"), 10, 64)
	favoriteFolderID, _ := strconv.ParseInt(r.URL.Query().Get("favorite_folder_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	recentMode := FeedRecentMode(r.URL.Query().Get("recent_mode"))
	if !IsFeedRecentMode(recentMode) {
		writeError(w, http.StatusBadRequest, "invalid recent_mode")
		return
	}
	works, err := a.store.FeedWithOptions(FeedOptions{
		CreatorID:        creatorID,
		DirectoryID:      directoryID,
		FavoriteFolderID: favoriteFolderID,
		Search:           r.URL.Query().Get("search"),
		Order:            r.URL.Query().Get("order"),
		Limit:            limit,
		RecentMode:       recentMode,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"works": works})
}

func (a *App) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": a.updater.Status()})
}

func (a *App) handleUpdateEvents(w http.ResponseWriter, r *http.Request) {
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
	ch, unsubscribe := a.updateHub.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			_, _ = fmt.Fprintf(w, "event: update\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (a *App) handleUpdateImmediate(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var body struct {
		CreatorIDs []int64 `json:"creator_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	status, err := a.updater.EnqueueImmediate(body.CreatorIDs, 0)
	if err != nil {
		writeError(w, updateErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status})
}

func (a *App) handleResetUpdateImmediate(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	status, err := a.updater.ResetImmediate(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}

func (a *App) handleRetryUpdateJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	status, err := a.updater.RetryJob(id)
	if err != nil {
		writeError(w, updateErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status})
}

func (a *App) handleUpdateJobs(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	page, pageSize := parsePageArgs(r)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch status {
	case "", "pending", "running", "succeeded", "failed":
	default:
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	jobs, err := a.store.UpdateJobsPage(page, pageSize, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (a *App) handleDeleteUpdateJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if err := a.store.DeleteUpdateJob(id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "update job not found")
			return
		}
		if strings.Contains(err.Error(), "running update job") {
			writeError(w, http.StatusConflict, "运行中的任务不能删除")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleUpdateQueues(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	queues, err := a.store.UpdateQueues()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queues": queues})
}

func (a *App) handleCreateUpdateQueue(w http.ResponseWriter, r *http.Request) {
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
	queue, err := a.store.CreateUpdateQueue(body.Name, body.Cron)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"queue": queue})
}

func (a *App) handlePatchUpdateQueue(w http.ResponseWriter, r *http.Request) {
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
	queue, err := a.store.UpdateUpdateQueue(id, body.Name, body.Cron, body.IsDefault, body.Enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queue": queue})
}

func (a *App) handleDeleteUpdateQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if err := a.store.DeleteUpdateQueue(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleUpdateQueueCreators(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	creators, err := a.store.CreatorsForUpdateQueue(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"creators": creators})
}

func (a *App) handleAddUpdateQueueCreators(w http.ResponseWriter, r *http.Request) {
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
	if err := a.store.AddCreatorsToUpdateQueue(id, body.CreatorIDs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleRemoveUpdateQueueCreator(w http.ResponseWriter, r *http.Request) {
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
	if err := a.store.RemoveCreatorFromUpdateQueue(queueID, creatorID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleRunUpdateQueueNow(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	run, err := a.updater.EnqueueQueueNow(id, "manual")
	if err != nil {
		writeError(w, updateErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"run": run, "status": a.updater.Status()})
}

func (a *App) handleUpdateCookie(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	cookie, err := a.store.UpdateCookie()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cookie": cookie})
}

func (a *App) handleSaveUpdateCookie(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var body struct {
		Cookie string `json:"cookie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.store.SaveUpdateCookie(body.Cookie); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cookie, _ := a.store.UpdateCookie()
	writeJSON(w, http.StatusOK, map[string]any{"cookie": cookie})
}

func (a *App) handleValidateUpdateCookie(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	if err := a.updater.ValidateCookie(r.Context()); err != nil {
		cookie, _ := a.store.UpdateCookie()
		writeJSON(w, http.StatusOK, map[string]any{"cookie": cookie, "ok": false})
		return
	}
	cookie, _ := a.store.UpdateCookie()
	writeJSON(w, http.StatusOK, map[string]any{"cookie": cookie, "ok": true})
}

func (a *App) handleFavoriteFolders(w http.ResponseWriter, r *http.Request) {
	viewUser, ok := a.currentViewUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	folders, err := a.currentStore().FavoriteFolders(viewUser.ID, r.URL.Query().Get("type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

func (a *App) handleCreateFavoriteFolder(w http.ResponseWriter, r *http.Request) {
	viewUser, ok := a.currentViewUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	folder, err := a.currentStore().CreateFavoriteFolder(viewUser.ID, body.Type, body.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"folder": folder})
}

func (a *App) handleUpdateFavoriteFolder(w http.ResponseWriter, r *http.Request) {
	viewUser, ok := a.currentViewUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct {
		Name      string `json:"name"`
		IsDefault *bool  `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	folder, err := a.currentStore().UpdateFavoriteFolder(viewUser.ID, id, body.Name, body.IsDefault)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folder": folder})
}

func (a *App) handleFavoriteStatus(w http.ResponseWriter, r *http.Request) {
	viewUser, ok := a.currentViewUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	workID, _ := strconv.ParseInt(r.URL.Query().Get("work_id"), 10, 64)
	creatorID, _ := strconv.ParseInt(r.URL.Query().Get("creator_id"), 10, 64)
	status, err := a.currentStore().FavoriteStatus(viewUser.ID, workID, creatorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}

func (a *App) handleFavoriteWorks(w http.ResponseWriter, r *http.Request) {
	folderID, _ := strconv.ParseInt(r.URL.Query().Get("folder_id"), 10, 64)
	works, err := a.store.FeedWithOptions(FeedOptions{FavoriteFolderID: folderID, Limit: 100})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"works": works})
}

func (a *App) handleAddWorkFavorite(w http.ResponseWriter, r *http.Request) {
	viewUser, ok := a.currentViewUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		WorkID   int64 `json:"work_id"`
		FolderID int64 `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.WorkID <= 0 {
		writeError(w, http.StatusBadRequest, "work_id is required")
		return
	}
	if err := a.currentStore().AddWorkFavorite(viewUser.ID, body.WorkID, body.FolderID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, _ := a.currentStore().FavoriteStatus(viewUser.ID, body.WorkID, 0)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

func (a *App) handleRemoveWorkFavorite(w http.ResponseWriter, r *http.Request) {
	viewUser, ok := a.currentViewUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	workID, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	folderID, _ := strconv.ParseInt(r.URL.Query().Get("folder_id"), 10, 64)
	if err := a.currentStore().RemoveWorkFavorite(viewUser.ID, workID, folderID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, _ := a.currentStore().FavoriteStatus(viewUser.ID, workID, 0)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

func (a *App) handleFavoriteCreators(w http.ResponseWriter, r *http.Request) {
	viewUser, ok := a.currentViewUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	folderID, _ := strconv.ParseInt(r.URL.Query().Get("folder_id"), 10, 64)
	creators, err := a.currentStore().FavoriteCreators(viewUser.ID, folderID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"creators": creators})
}

func (a *App) handleAddCreatorFavorite(w http.ResponseWriter, r *http.Request) {
	viewUser, ok := a.currentViewUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		CreatorID int64 `json:"creator_id"`
		FolderID  int64 `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.CreatorID <= 0 {
		writeError(w, http.StatusBadRequest, "creator_id is required")
		return
	}
	if err := a.currentStore().AddCreatorFavorite(viewUser.ID, body.CreatorID, body.FolderID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, _ := a.currentStore().FavoriteStatus(viewUser.ID, 0, body.CreatorID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

func (a *App) handleRemoveCreatorFavorite(w http.ResponseWriter, r *http.Request) {
	viewUser, ok := a.currentViewUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	creatorID, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	folderID, _ := strconv.ParseInt(r.URL.Query().Get("folder_id"), 10, 64)
	if err := a.currentStore().RemoveCreatorFavorite(viewUser.ID, creatorID, folderID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, _ := a.currentStore().FavoriteStatus(viewUser.ID, 0, creatorID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

func (a *App) handleWork(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	work, err := a.store.Work(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"work": work})
}

func (a *App) handleWorkCover(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	path, err := a.currentStore().WorkCoverFile(id)
	if err != nil {
		status := http.StatusNotFound
		if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, os.ErrNotExist) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err.Error())
		return
	}
	http.ServeFile(w, r, path)
}

func (a *App) handleRecordWorkView(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct {
		EventType   string  `json:"event_type"`
		PositionSec float64 `json:"position_sec"`
		DurationSec float64 `json:"duration_sec"`
		MediaType   string  `json:"media_type"`
		Detail      string  `json:"detail"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := a.currentStore().RecordWorkView(user.ID, id, body.EventType, body.PositionSec, body.DurationSec, body.MediaType, body.Detail); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (a *App) handleMedia(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	path, name, mediaType, err := a.store.MediaPath(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	openStart := time.Now()
	file, err := os.Open(path)
	openTime := time.Since(openStart)
	if err != nil {
		accessLogSetMedia(r, accessLogMedia{
			ID:       id,
			Type:     mediaType,
			Range:    r.Header.Get("Range"),
			OpenTime: openTime,
		})
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	defer file.Close()
	statStart := time.Now()
	info, err := file.Stat()
	statTime := time.Since(statStart)
	if err != nil {
		accessLogSetMedia(r, accessLogMedia{
			ID:       id,
			Type:     mediaType,
			Range:    r.Header.Get("Range"),
			OpenTime: openTime,
			StatTime: statTime,
		})
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	accessLogSetMedia(r, accessLogMedia{
		ID:       id,
		Type:     mediaType,
		Range:    r.Header.Get("Range"),
		FileSize: info.Size(),
		OpenTime: openTime,
		StatTime: statTime,
	})
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if mediaType == "video" {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	http.ServeContent(w, r, name, info.ModTime(), mediaContentReadSeeker(file, info))
}

func mediaContentReadSeeker(file *os.File, info os.FileInfo) io.ReadSeeker {
	return io.NewSectionReader(file, 0, info.Size())
}

func (a *App) handleWeb(w http.ResponseWriter, r *http.Request) {
	isStaticAsset := strings.HasPrefix(r.URL.Path, "/assets/")
	if a.web.Dir != "" {
		path := filepath.Join(a.web.Dir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			http.ServeFile(w, r, path)
			return
		}
		if isStaticAsset {
			http.NotFound(w, r)
			return
		}
		index := filepath.Join(a.web.Dir, "index.html")
		if _, err := os.Stat(index); err == nil {
			http.ServeFile(w, r, index)
			return
		}
	}
	if a.web.FS != nil && a.serveEmbeddedWeb(w, r, strings.TrimPrefix(filepath.Clean(r.URL.Path), string(filepath.Separator))) {
		return
	}
	if isStaticAsset {
		http.NotFound(w, r)
		return
	}
	if a.web.FS != nil && a.serveEmbeddedWeb(w, r, "index.html") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "LocalBilibili API is running. Build web assets with npm run build in web/."})
}

func (a *App) serveEmbeddedWeb(w http.ResponseWriter, r *http.Request, name string) bool {
	if name == "" || name == "." || !fs.ValidPath(name) {
		name = "index.html"
	}
	info, err := fs.Stat(a.web.FS, name)
	if err != nil || info.IsDir() {
		return false
	}
	data, err := fs.ReadFile(a.web.FS, name)
	if err != nil {
		return false
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), bytes.NewReader(data))
	return true
}

func parseID(w http.ResponseWriter, raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func parsePageArgs(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page_size")))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	return page, pageSize
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func updateErrorStatus(err error) int {
	if err == nil {
		return http.StatusAccepted
	}
	message := err.Error()
	if strings.Contains(message, "下载器") {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadRequest
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Vary", "Origin")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Range")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
