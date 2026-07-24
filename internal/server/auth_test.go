package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("LOCALBILIBILI_BOOTSTRAP_USERNAME", "ted")
	_ = os.Setenv("LOCALBILIBILI_BOOTSTRAP_PASSWORD", "ted")
	os.Exit(m.Run())
}

func TestStoreSeedsConfiguredBootstrapAdminUser(t *testing.T) {
	store := newTestStore(t)
	users, err := store.Users()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("users len = %d, want 1", len(users))
	}
	if users[0].Username != "ted" || !users[0].IsAdmin {
		t.Fatalf("default user = %#v", users[0])
	}
	if _, err := store.Authenticate("ted", "ted"); err != nil {
		t.Fatalf("default admin should authenticate: %v", err)
	}
}

func TestStoreRejectsMissingBootstrapCredentialsForNewDatabase(t *testing.T) {
	t.Setenv("LOCALBILIBILI_BOOTSTRAP_USERNAME", "")
	t.Setenv("LOCALBILIBILI_BOOTSTRAP_PASSWORD", "")
	_, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err == nil || !strings.Contains(err.Error(), "LOCALBILIBILI_BOOTSTRAP_USERNAME") {
		t.Fatalf("OpenStore error = %v, want missing bootstrap credentials error", err)
	}
}

func TestAuthLifecycleAndProtectedRoutes(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "video.mp4")
	if err := writeTestFile(mediaPath, []byte("video")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: mediaPath, FileName: "video.mp4"}},
	}}); err != nil {
		t.Fatal(err)
	}
	app := mustTestApp(t, store)
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)

	client := cookieClient()
	assertStatus(t, client, http.MethodGet, server.URL+"/api/feed", nil, http.StatusUnauthorized)
	assertStatus(t, client, http.MethodGet, server.URL+"/api/media/1", nil, http.StatusUnauthorized)
	assertStatus(t, client, http.MethodGet, server.URL+"/api/scan/events", nil, http.StatusUnauthorized)
	assertStatus(t, client, http.MethodGet, server.URL+"/api/events", nil, http.StatusUnauthorized)

	login(t, client, server.URL, "ted", "ted")
	me := getCurrentUser(t, client, server.URL)
	if me.Username != "ted" || !me.IsAdmin {
		t.Fatalf("current user = %#v", me)
	}
	assertStatus(t, client, http.MethodGet, server.URL+"/api/feed", nil, http.StatusOK)

	changePassword(t, client, server.URL, "ted", "new-ted")
	logout(t, client, server.URL)
	assertStatus(t, client, http.MethodGet, server.URL+"/api/feed", nil, http.StatusUnauthorized)
	login(t, client, server.URL, "ted", "new-ted")
}

func TestUnifiedEventsPermissions(t *testing.T) {
	app := mustTestApp(t, newTestStore(t))
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)

	app.hub.Publish(ScanEvent{Type: "scan-ready"})
	app.updateHub.PublishUpdate(UpdateEvent{Type: "update-ready"})
	app.subtitleHub.PublishAny(SubtitleEvent{Type: "subtitle-ready"})

	adminClient := cookieClient()
	login(t, adminClient, server.URL, "ted", "ted")
	adminEvents := readSSEEventNames(t, adminClient, server.URL+"/api/events", 3)
	if !containsString(adminEvents, "scan") || !containsString(adminEvents, "update") || !containsString(adminEvents, "subtitle") {
		t.Fatalf("admin events = %#v, want scan/update/subtitle", adminEvents)
	}

	if _, err := app.store.CreateUser("alice", "alice123"); err != nil {
		t.Fatal(err)
	}
	userClient := cookieClient()
	login(t, userClient, server.URL, "alice", "alice123")
	userEvents := readSSEEventNames(t, userClient, server.URL+"/api/events", 1)
	if len(userEvents) != 1 || userEvents[0] != "scan" {
		t.Fatalf("user events = %#v, want only scan", userEvents)
	}

	assertStatus(t, adminClient, http.MethodGet, server.URL+"/api/scan/events", nil, http.StatusOK)
}

func TestAdminUserManagement(t *testing.T) {
	app := mustTestApp(t, newTestStore(t))
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()

	login(t, client, server.URL, "ted", "ted")
	created := createUser(t, client, server.URL, "alice", "alice123")
	users := listUsers(t, client, server.URL)
	if len(users) != 2 {
		t.Fatalf("users len = %d, want 2", len(users))
	}
	if !containsUser(users, "alice") {
		t.Fatalf("alice not in users: %#v", users)
	}

	resetUserPassword(t, client, server.URL, created.ID, "alice456")
	logout(t, client, server.URL)
	login(t, client, server.URL, "alice", "alice456")

	assertForbidden(t, client, http.MethodGet, server.URL+"/api/users", nil)

	logout(t, client, server.URL)
	login(t, client, server.URL, "ted", "ted")
	assertStatus(t, client, http.MethodDelete, server.URL+"/api/users/"+strconvID(created.ID), nil, http.StatusOK)
}

func TestAdminCannotDeleteSelf(t *testing.T) {
	app := mustTestApp(t, newTestStore(t))
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")
	self := getCurrentUser(t, client, server.URL)
	assertStatus(t, client, http.MethodDelete, server.URL+"/api/users/"+strconvID(self.ID), nil, http.StatusBadRequest)
}

func mustTestApp(t *testing.T, store *Store) *App {
	t.Helper()
	hub := NewEventHub()
	updateHub := NewEventHub()
	subtitleHub := NewEventHub()
	app := &App{
		store:       store,
		scanner:     NewScanner(store, hub),
		hub:         hub,
		updater:     NewUpdateManager(store, updateHub, nil, DefaultDownloaderConcurrency),
		updateHub:   updateHub,
		subtitles:   NewSubtitleManager(store, subtitleHub, DefaultSubtitleRuntimeConfig()),
		subtitleHub: subtitleHub,
	}
	t.Cleanup(func() {
		if app.updater != nil {
			app.updater.Close()
		}
		if app.subtitles != nil {
			app.subtitles.Close()
		}
	})
	return app
}

func cookieClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func login(t *testing.T, client *http.Client, baseURL, username, password string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	assertStatus(t, client, http.MethodPost, baseURL+"/api/auth/login", bytes.NewReader(body), http.StatusOK)
}

func logout(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	assertStatus(t, client, http.MethodPost, baseURL+"/api/auth/logout", nil, http.StatusOK)
}

func changePassword(t *testing.T, client *http.Client, baseURL, oldPassword, newPassword string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"old_password": oldPassword, "new_password": newPassword})
	assertStatus(t, client, http.MethodPost, baseURL+"/api/auth/password", bytes.NewReader(body), http.StatusOK)
}

func createUser(t *testing.T, client *http.Client, baseURL, username, password string) User {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	var response struct {
		User User `json:"user"`
	}
	decodeJSONResponse(t, client, http.MethodPost, baseURL+"/api/users", bytes.NewReader(body), http.StatusCreated, &response)
	return response.User
}

func resetUserPassword(t *testing.T, client *http.Client, baseURL string, id int64, password string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	assertStatus(t, client, http.MethodPost, baseURL+"/api/users/"+strconvID(id)+"/reset-password", bytes.NewReader(body), http.StatusOK)
}

func listUsers(t *testing.T, client *http.Client, baseURL string) []User {
	t.Helper()
	var response struct {
		Users []User `json:"users"`
	}
	decodeJSONResponse(t, client, http.MethodGet, baseURL+"/api/users", nil, http.StatusOK, &response)
	return response.Users
}

func getCurrentUser(t *testing.T, client *http.Client, baseURL string) User {
	t.Helper()
	var response struct {
		User User `json:"user"`
	}
	decodeJSONResponse(t, client, http.MethodGet, baseURL+"/api/auth/me", nil, http.StatusOK, &response)
	return response.User
}

func decodeJSONResponse(t *testing.T, client *http.Client, method, url string, body io.Reader, wantStatus int, out any) {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, body=%s", method, url, resp.StatusCode, readBody(t, resp))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func assertStatus(t *testing.T, client *http.Client, method, url string, body io.Reader, wantStatus int) {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, body=%s", method, url, resp.StatusCode, readBody(t, resp))
	}
}

func assertForbidden(t *testing.T, client *http.Client, method, url string, body io.Reader) {
	t.Helper()
	assertStatus(t, client, method, url, body, http.StatusForbidden)
}

func containsUser(users []User, username string) bool {
	for _, user := range users {
		if user.Username == username {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func readSSEEventNames(t *testing.T, client *http.Client, url string, count int) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d, body=%s", resp.StatusCode, readBody(t, resp))
	}
	events := []string{}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			events = append(events, strings.TrimSpace(strings.TrimPrefix(line, "event: ")))
			if len(events) >= count {
				return events
			}
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	if len(events) < count {
		t.Fatalf("events = %#v, want %d events", events, count)
	}
	return events
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func writeTestFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
