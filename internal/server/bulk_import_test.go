package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewBulkImportExtractsDirectAndShortLinks(t *testing.T) {
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.bilibili.com/user/MS4wLjABAAAA_SHORT?from=share", http.StatusFound)
	}))
	t.Cleanup(redirectServer.Close)

	items := PreviewBulkImportCreators(t.Context(), nil, 0, strings.Join([]string{
		"https://space.bilibili.com/54887875?spm_id_from=333.337.0.0",
		"https://www.bilibili.com/user/MS4wLjABAAAA_DIRECT?enter_from=author_card",
		"长按复制此条消息，打开B站搜索，查看TA的更多作品。 " + redirectServer.URL + "/abc/",
		"4- 长按复制此条消息，打开B站搜索，查看TA的更多作品。 " + redirectServer.URL + "/abc/ 1@7.com :9pm",
		"不是B站链接 https://example.com/nope",
	}, "\n"), redirectResolvingClient())

	if len(items) != 4 {
		t.Fatalf("items len = %d: %#v", len(items), items)
	}
	if items[0].ProfileURL != "https://space.bilibili.com/54887875" || items[0].SecUID != "54887875" || items[0].Status != "ready" {
		t.Fatalf("space item = %#v", items[0])
	}
	if items[1].ProfileURL != "https://www.bilibili.com/user/MS4wLjABAAAA_DIRECT" || items[1].SecUID != "MS4wLjABAAAA_DIRECT" || items[1].Status != "ready" {
		t.Fatalf("direct item = %#v", items[1])
	}
	if items[2].ProfileURL != "https://www.bilibili.com/user/MS4wLjABAAAA_SHORT" || items[2].Status != "ready" {
		t.Fatalf("short item = %#v", items[2])
	}
	if items[3].Status != "error" || !strings.Contains(items[3].Error, "unsupported") {
		t.Fatalf("invalid item = %#v", items[3])
	}
}

func TestBulkImportCreatorsCreatesPlaceholdersAndReusesExisting(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	existing, err := store.ImportCreatorPlaceholder(dir.ID, "https://www.bilibili.com/user/MS4wLjABAAAA_EXISTING")
	if err != nil {
		t.Fatal(err)
	}
	creators, err := store.ImportCreatorPlaceholders(dir.ID, []string{
		"https://www.bilibili.com/user/MS4wLjABAAAA_EXISTING",
		"https://www.bilibili.com/user/MS4wLjABAAAA_NEWONE",
		"https://www.bilibili.com/user/MS4wLjABAAAA_NEWONE?ignored=1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 2 {
		t.Fatalf("creators len = %d: %#v", len(creators), creators)
	}
	if creators[0].ID != existing.ID || creators[0].WorkCount != 0 {
		t.Fatalf("existing not reused: %#v existing=%#v", creators[0], existing)
	}
	if creators[1].BilibiliSecUID != "MS4wLjABAAAA_NEWONE" || creators[1].BilibiliProfileURL != "https://www.bilibili.com/user/MS4wLjABAAAA_NEWONE" || creators[1].OnlineIdentityStatus != "ready" {
		t.Fatalf("new placeholder = %#v", creators[1])
	}
	all, err := store.Creators()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("duplicate placeholders created: %#v", all)
	}
}

func TestBulkImportPreviewAndImportUseResolvedProfile(t *testing.T) {
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	app.updater = NewUpdateManager(app.store, app.updateHub, writingDownloader{creatorName: "全哥"})
	dir, err := app.store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")

	previewBody, _ := json.Marshal(map[string]any{"directory_id": dir.ID, "text": "https://www.bilibili.com/user/MS4wLjABAAAA_wxKfOR6J"})
	resp, err := client.Post(server.URL+"/api/creators/bulk-import/preview", "application/json", bytes.NewReader(previewBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var preview struct {
		Items []BulkImportItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 1 || preview.Items[0].DisplayName != "全哥" || preview.Items[0].ResolveStatus != "resolved" {
		t.Fatalf("preview items = %#v", preview.Items)
	}

	importBody, _ := json.Marshal(map[string]any{"directory_id": dir.ID, "profile_urls": []string{preview.Items[0].ProfileURL}})
	resp, err = client.Post(server.URL+"/api/creators/bulk-import", "application/json", bytes.NewReader(importBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var imported struct {
		Creators []Creator        `json:"creators"`
		Items    []BulkImportItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&imported); err != nil {
		t.Fatal(err)
	}
	if len(imported.Creators) != 1 || imported.Creators[0].Name != "全哥" || imported.Creators[0].AvatarURL == "" || imported.Creators[0].Bio != "全哥签名" {
		t.Fatalf("imported creators = %#v", imported.Creators)
	}
}

func TestBulkImportRequiresNameAndOnlineIdentityToReuse(t *testing.T) {
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	app.updater = NewUpdateManager(app.store, app.updateHub, writingDownloader{creatorName: "全哥"})
	dir, err := app.store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	placeholder, err := app.store.ImportCreatorPlaceholder(dir.ID, "https://www.bilibili.com/user/MS4wLjABAAAA_wxKfOR6J")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")

	previewBody, _ := json.Marshal(map[string]any{"directory_id": dir.ID, "text": "https://www.bilibili.com/user/MS4wLjABAAAA_wxKfOR6J"})
	resp, err := client.Post(server.URL+"/api/creators/bulk-import/preview", "application/json", bytes.NewReader(previewBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var preview struct {
		Items []BulkImportItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 1 || !preview.Items[0].Existing || preview.Items[0].ExistingCreatorID != placeholder.ID || preview.Items[0].DisplayName != "全哥" {
		t.Fatalf("preview should reuse matching placeholder identity: %#v placeholder=%#v", preview.Items, placeholder)
	}

	importBody, _ := json.Marshal(map[string]any{"directory_id": dir.ID, "profile_urls": []string{preview.Items[0].ProfileURL}})
	resp, err = client.Post(server.URL+"/api/creators/bulk-import", "application/json", bytes.NewReader(importBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var imported struct {
		Creators []Creator `json:"creators"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&imported); err != nil {
		t.Fatal(err)
	}
	if len(imported.Creators) != 1 || imported.Creators[0].ID != placeholder.ID || imported.Creators[0].Name != "全哥" {
		t.Fatalf("import should promote matching placeholder: placeholder=%#v imported=%#v", placeholder, imported.Creators)
	}
	all, err := app.store.Creators()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != placeholder.ID || all[0].Name != "全哥" {
		t.Fatalf("expected promoted placeholder only: %#v", all)
	}
}

func TestBulkImportReusesOnlyWhenNameAndOnlineIdentityMatch(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	existing, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "全哥", CreatorIdentity{SecUID: "sec-one", ProfileURL: "https://www.bilibili.com/user/sec-one"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	creators, err := store.ImportResolvedCreatorProfiles(dir.ID, []CreatorProfile{{Name: "全哥", SecUID: "sec-one", ProfileURL: "https://www.bilibili.com/user/sec-one"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 || creators[0].ID != existing.ID {
		t.Fatalf("same name and identity should reuse: existing=%#v creators=%#v", existing, creators)
	}

	creators, err = store.ImportResolvedCreatorProfiles(dir.ID, []CreatorProfile{{Name: "全哥", SecUID: "sec-two", ProfileURL: "https://www.bilibili.com/user/sec-two"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 || creators[0].ID == existing.ID || creators[0].Name == existing.Name {
		t.Fatalf("same name but different identity should create unique creator: existing=%#v creators=%#v", existing, creators)
	}
}

func TestBulkImportWithoutOnlineIdentityMatchesByName(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	existing, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "无身份UP主", CreatorIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	creators, err := store.ImportResolvedCreatorProfiles(dir.ID, []CreatorProfile{{Name: "无身份UP主"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 || creators[0].ID != existing.ID {
		t.Fatalf("missing online identity should reuse by name: existing=%#v creators=%#v", existing, creators)
	}

	creators, err = store.ImportResolvedCreatorProfiles(dir.ID, []CreatorProfile{{Name: "另一个无身份UP主"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 || creators[0].ID == existing.ID || creators[0].Name != "另一个无身份UP主" {
		t.Fatalf("different name without identity should create: existing=%#v creators=%#v", existing, creators)
	}
}

func TestResolveCreatorPlaceholdersPromotesHistoricalShortNames(t *testing.T) {
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	app.updater = NewUpdateManager(app.store, app.updateHub, writingDownloader{creatorName: "毅本万利"})
	dir, err := app.store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, err := app.store.ImportCreatorPlaceholder(dir.ID, "https://www.bilibili.com/user/MS4wLjABAAAA_wxKfOR6J")
	if err != nil {
		t.Fatal(err)
	}
	if creator.Name != "wxKfOR6J" {
		t.Fatalf("expected short placeholder, got %#v", creator)
	}
	if err := app.store.SaveUpdateCookie("msToken=one; ttwid=two"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	client := cookieClient()
	login(t, client, server.URL, "ted", "ted")

	resp, err := client.Post(server.URL+"/api/creators/resolve-placeholders", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	updated, err := app.store.Creator(creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "毅本万利" || updated.AvatarURL == "" || updated.Bio != "毅本万利签名" {
		t.Fatalf("placeholder not resolved: %#v", updated)
	}
}

func TestBulkImportAPIsRequireAdmin(t *testing.T) {
	app, err := NewAppWithAssets(filepath.Join(t.TempDir(), "app.db"), WebAssets{FS: testWebFS()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	server := httptest.NewServer(app.Routes())
	t.Cleanup(server.Close)
	adminClient := cookieClient()
	login(t, adminClient, server.URL, "ted", "ted")
	createUser(t, adminClient, server.URL, "alice", "secret")
	userClient := cookieClient()
	login(t, userClient, server.URL, "alice", "secret")

	body, _ := json.Marshal(map[string]any{"directory_id": 1, "text": "https://www.bilibili.com/user/MS4wLjABAAAA_TEST"})
	assertForbidden(t, userClient, http.MethodPost, server.URL+"/api/creators/bulk-import/preview", bytes.NewReader(body))
	body, _ = json.Marshal(map[string]any{"directory_id": 1, "profile_urls": []string{"https://www.bilibili.com/user/MS4wLjABAAAA_TEST"}})
	assertForbidden(t, userClient, http.MethodPost, server.URL+"/api/creators/bulk-import", bytes.NewReader(body))
}
