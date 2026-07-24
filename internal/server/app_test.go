package server

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"
)

func TestAPIDirectoryScanAndImmediateFeed(t *testing.T) {
	root := t.TempDir()
	creator := filepath.Join(root, "creator-a")
	if err := os.Mkdir(creator, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(creator, "2026-05-16_作品_7640372060313385727_data.json"), []byte(`{"aweme_id":"7640372060313385727","desc":"作品"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(creator, "2026-05-16_作品_7640372060313385727.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(filepath.Join(t.TempDir(), "app.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")

	body, _ := json.Marshal(map[string]string{"path": root})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/directories", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var created struct {
		Directory Directory `json:"directory"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	scanReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/directories/"+strconvID(created.Directory.ID)+"/scan", nil)
	if _, err := client.Do(scanReq); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		feedReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/feed", nil)
		if err != nil {
			t.Fatal(err)
		}
		feedResp, err := client.Do(feedReq)
		if err != nil {
			t.Fatal(err)
		}
		var feed struct {
			Works []Work `json:"works"`
		}
		_ = json.NewDecoder(feedResp.Body).Decode(&feed)
		_ = feedResp.Body.Close()
		if len(feed.Works) == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("feed did not become visible after scan")
}

func TestMediaSupportsRange(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = store.UpsertCreatorWithWorks(dir.ID, "creator", []WorkInput{{
		AwemeID: "1",
		Title:   "clip",
		Media:   []MediaInput{{Type: "video", FilePath: mediaPath, FileName: "clip.mp4"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, scanner: NewScanner(store, NewEventHub()), hub: NewEventHub()}
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")
	works, err := store.Feed(0, 0, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/media/"+strconvID(works[0].Media[0].ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-3")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "video/mp4" {
		t.Fatalf("content type = %q", contentType)
	}
}

func TestMediaPreservesOpenEndedVideoRange(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("a"), 2*1024*1024+512)
	mediaPath := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(mediaPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = store.UpsertCreatorWithWorks(dir.ID, "creator", []WorkInput{{
		AwemeID: "1",
		Title:   "clip",
		Media:   []MediaInput{{Type: "video", FilePath: mediaPath, FileName: "clip.mp4"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, scanner: NewScanner(store, NewEventHub()), hub: NewEventHub()}
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")
	works, err := store.Feed(0, 0, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/media/"+strconvID(works[0].Media[0].ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got, want := resp.Header.Get("Content-Range"), "bytes 0-2097663/2097664"; got != want {
		t.Fatalf("content range = %q, want %q", got, want)
	}
	if got := resp.Header.Get("Content-Length"); got != "2097664" {
		t.Fatalf("content length = %q", got)
	}

	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/media/"+strconvID(works[0].Media[0].ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=2097152-2097663")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got, want := resp.Header.Get("Content-Range"), "bytes 2097152-2097663/2097664"; got != want {
		t.Fatalf("small explicit content range = %q, want %q", got, want)
	}
}

func TestMediaContentReadSeekerUsesSectionReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}

	reader := mediaContentReadSeeker(file, info)
	if _, ok := reader.(*os.File); ok {
		t.Fatal("media content reader must not expose *os.File to net/http sendfile fast path")
	}
	if _, ok := reader.(*io.SectionReader); !ok {
		t.Fatalf("media content reader = %T, want *io.SectionReader", reader)
	}
	buf, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf) != "0123456789" {
		t.Fatalf("content = %q", string(buf))
	}
}

func TestDeleteCreatorAPIRequiresAdminAndDeletesCreator(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "app.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(creatorDir, "2026-06-28_作品_1001.mp4")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := app.store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := app.store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: mediaPath, FileName: filepath.Base(mediaPath)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)

	adminClient := cookieClient()
	login(t, adminClient, server.URL, "ted", "ted")
	normal := createUser(t, adminClient, server.URL, "alice", "secret123")
	userClient := cookieClient()
	login(t, userClient, server.URL, normal.Username, "secret123")
	body, _ := json.Marshal(map[string]bool{"delete_media": false})
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/creators/"+strconvID(creator.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := userClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin status = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/creators/"+strconvID(creator.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = adminClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin status = %d", resp.StatusCode)
	}
	if _, err := os.Stat(mediaPath); err != nil {
		t.Fatalf("media should remain when delete_media=false: %v", err)
	}
	if _, err := app.store.Creator(creator.ID); err == nil {
		t.Fatal("creator should be deleted")
	}
}

func TestDeleteCreatorAPINotFound(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "app.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")

	body, _ := json.Marshal(map[string]bool{"delete_media": true})
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/creators/99999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, readBody(t, resp))
	}
}

func TestEmbeddedWebServesSPAAndKeepsAPIJSON(t *testing.T) {
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")

	for _, path := range []string{"/", "/creators", "/settings"} {
		req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		rec := httptest.NewRecorder()
		app.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
		if rec.Body.String() != "<html>embedded</html>" {
			t.Fatalf("%s body = %q", path, rec.Body.String())
		}
	}

	apiReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/creators", nil)
	apiResp, err := client.Do(apiReq)
	if err != nil {
		t.Fatal(err)
	}
	defer apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusOK {
		t.Fatalf("api status = %d, body=%s", apiResp.StatusCode, readBody(t, apiResp))
	}
	if contentType := apiResp.Header.Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("api content type = %q", contentType)
	}
}

func TestEmptyListAPIsReturnArrays(t *testing.T) {
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/api/creators", want: `{"creators":[]}` + "\n"},
		{path: "/api/directories", want: `{"directories":[]}` + "\n"},
		{path: "/api/feed", want: `{"works":[]}` + "\n"},
	} {
		req, _ := http.NewRequest(http.MethodGet, server.URL+tc.path, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", tc.path, resp.StatusCode, readBody(t, resp))
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != tc.want {
			t.Fatalf("%s body = %q, want %q", tc.path, string(body), tc.want)
		}
	}
}

func TestCreateDirectoryRejectsUnauthorizedPath(t *testing.T) {
	allowed := t.TempDir()
	blocked := t.TempDir()
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	app.browser = FileBrowser{allowedPaths: []string{allowed}}
	t.Cleanup(func() { _ = app.Close() })
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")

	body, _ := json.Marshal(map[string]string{"path": blocked})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/directories", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("blocked status = %d, body=%s", resp.StatusCode, readBody(t, resp))
	}

	body, _ = json.Marshal(map[string]string{"path": allowed})
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/directories", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("allowed status = %d, body=%s", resp.StatusCode, readBody(t, resp))
	}
}

func TestEmbeddedWebServesAssetsAndMissingAssets404(t *testing.T) {
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "console.log('embedded')" {
		t.Fatalf("asset body = %q", rec.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	missingRec := httptest.NewRecorder()
	app.Routes().ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, body=%s", missingRec.Code, missingRec.Body.String())
	}
}

func TestExternalWebDirOverridesEmbeddedAssets(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>external</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{Dir: webDir, FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "<html>external</html>" {
		t.Fatalf("body = %q", string(body))
	}
}

func testWebFS() fs.FS {
	return fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>embedded</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('embedded')")},
	}
}

func strconvID(id int64) string {
	return strconv.FormatInt(id, 10)
}
