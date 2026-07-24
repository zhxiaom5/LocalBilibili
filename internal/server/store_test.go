package server

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("LOCALBILIBILI_BOOTSTRAP_USERNAME", "ted")
	t.Setenv("LOCALBILIBILI_BOOTSTRAP_PASSWORD", "ted")
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSaveDirectoryDeduplicatesAndDeleteKeepsFiles(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	realFile := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(realFile, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := store.SaveDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected dedupe, got %d and %d", first.ID, second.ID)
	}
	if err := store.DeleteDirectory(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(realFile); err != nil {
		t.Fatalf("delete directory should not delete real files: %v", err)
	}
}

func TestDeleteCreatorWithoutMediaKeepsCreatorDirectory(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(creatorDir, "2026-06-28_作品_1001.mp4")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: mediaPath, FileName: filepath.Base(mediaPath)}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteCreator(creator.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mediaPath); err != nil {
		t.Fatalf("media should remain on disk: %v", err)
	}
	if _, err := store.Creator(creator.ID); err == nil {
		t.Fatal("creator should be removed from database")
	}
}

func TestDeleteCreatorWithMediaRemovesWholeCreatorDirectory(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	creatorDir := filepath.Join(root, "阿言")
	nestedDir := filepath.Join(creatorDir, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(creatorDir, "2026-06-28_作品_1001.mp4")
	extraPath := filepath.Join(nestedDir, "extra.txt")
	if err := os.WriteFile(mediaPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extraPath, []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: mediaPath, FileName: filepath.Base(mediaPath)}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteCreator(creator.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(creatorDir); !os.IsNotExist(err) {
		t.Fatalf("creator directory should be removed, stat err=%v", err)
	}
	if _, err := store.Creator(creator.ID); err == nil {
		t.Fatal("creator should be removed from database")
	}
}

func TestDeleteCreatorWithMissingMediaDirectoryStillDeletesIndex(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "missing", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: filepath.Join(root, "missing", "clip.mp4"), FileName: "clip.mp4"}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteCreator(creator.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Creator(creator.ID); err == nil {
		t.Fatal("creator should be removed from database")
	}
}

func TestDeleteCreatorRejectsMediaDirectoryTraversalAndKeepsIndex(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "..", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: filepath.Join(root, "clip.mp4"), FileName: "clip.mp4"}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteCreator(creator.ID, true); err == nil {
		t.Fatal("expected traversal guard error")
	}
	if _, err := store.Creator(creator.ID); err != nil {
		t.Fatalf("creator index should remain after failed media delete: %v", err)
	}
}

func TestUpsertCreatorWithWorksDeduplicates(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "a.webp")
	input := []WorkInput{{
		AwemeID:     "1",
		Title:       "title",
		Description: "desc",
		Tags:        []string{"tag"},
		Media:       []MediaInput{{Type: "image", FilePath: mediaPath, FileName: "a.webp"}},
	}}
	if _, _, err := store.UpsertCreatorWithWorks(dir.ID, "creator", input); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertCreatorWithWorks(dir.ID, "creator", input); err != nil {
		t.Fatal(err)
	}
	creators, err := store.Creators()
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 {
		t.Fatalf("creators len = %d", len(creators))
	}
	works, err := store.Feed(0, 0, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 || len(works[0].Media) != 1 {
		t.Fatalf("unexpected works: %#v", works)
	}
}

func TestFeedSearchMatchesMetadataAndRespectsCreator(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	firstMedia := filepath.Join(t.TempDir(), "first.webp")
	secondMedia := filepath.Join(t.TempDir(), "second.webp")
	creatorA, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID:     "1001",
		Title:       "夏天训练",
		Description: "健身日常",
		MusicTitle:  "海边音乐",
		Tags:        []string{"夏日"},
		Media:       []MediaInput{{Type: "image", FilePath: firstMedia, FileName: "first.webp"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertCreatorWithWorks(dir.ID, "另一位", []WorkInput{{
		AwemeID:     "1002",
		Title:       "夏天旅行",
		Description: "旅游",
		MusicTitle:  "城市音乐",
		Tags:        []string{"旅行"},
		Media:       []MediaInput{{Type: "image", FilePath: secondMedia, FileName: "second.webp"}},
	}}); err != nil {
		t.Fatal(err)
	}
	cases := []string{"训练", "健身", "阿言", "夏日", "海边"}
	for _, keyword := range cases {
		works, err := store.Feed(0, 0, keyword, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(works) != 1 || works[0].AwemeID != "1001" {
			t.Fatalf("search %q returned %#v", keyword, works)
		}
	}
	works, err := store.Feed(creatorA.ID, 0, "旅行", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 0 {
		t.Fatalf("creator-scoped search leaked results: %#v", works)
	}
}

func TestFeedSearchMatchesPinyinAndInitials(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creatorA, _, err := store.UpsertCreatorWithWorks(dir.ID, "徐大川", []WorkInput{{
		AwemeID:     "1001",
		Title:       "夏天训练",
		Description: "海边健身日常",
		MusicTitle:  "城市音乐",
		Tags:        []string{"腹肌"},
		Media:       []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "first.webp"), FileName: "first.webp"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID:     "1002",
		Title:       "城市旅行",
		Description: "咖啡日记",
		MusicTitle:  "周末",
		Tags:        []string{"旅行"},
		Media:       []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "second.webp"), FileName: "second.webp"}},
	}}); err != nil {
		t.Fatal(err)
	}
	cases := []string{"xiatian", "XT", "hai bian", "fujI", "chengshiyinyue", "XuDaChuan", "xdc"}
	for _, keyword := range cases {
		works, err := store.Feed(0, 0, keyword, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(works) != 1 || works[0].AwemeID != "1001" {
			t.Fatalf("search %q returned %#v", keyword, works)
		}
	}
	works, err := store.Feed(creatorA.ID, 0, "lvxing", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 0 {
		t.Fatalf("creator-scoped pinyin search leaked results: %#v", works)
	}
}

func TestFeedOrderModes(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{
		{
			AwemeID:     "1001",
			Title:       "第一条",
			PublishedAt: "2026-03-10T00:00:00Z",
			Media:       []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "first.webp"), FileName: "first.webp"}},
		},
		{
			AwemeID:     "1002",
			Title:       "第二条",
			PublishedAt: "2026-03-11T00:00:00Z",
			Media:       []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "second.webp"), FileName: "second.webp"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	descWorks, err := store.FeedWithOptions(FeedOptions{Order: "desc", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(descWorks) != 2 || descWorks[0].AwemeID != "1002" || descWorks[1].AwemeID != "1001" {
		t.Fatalf("desc order = %#v", descWorks)
	}

	ascWorks, err := store.FeedWithOptions(FeedOptions{Order: "asc", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(ascWorks) != 2 || ascWorks[0].AwemeID != "1001" || ascWorks[1].AwemeID != "1002" {
		t.Fatalf("asc order = %#v", ascWorks)
	}

	fallbackWorks, err := store.FeedWithOptions(FeedOptions{Order: "weird", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(fallbackWorks) != 2 {
		t.Fatalf("fallback order returned %d works", len(fallbackWorks))
	}
}

func TestCreatorsSearchFiltersByName(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"阿言", "徐大川"} {
		if _, _, err := store.UpsertCreatorWithWorks(dir.ID, name, []WorkInput{{
			AwemeID: name,
			Title:   "title",
			Media:   []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), name+".webp"), FileName: name + ".webp"}},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	creators, err := store.Creators("徐")
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 || creators[0].Name != "徐大川" {
		t.Fatalf("unexpected creators: %#v", creators)
	}
}

func TestCreatorsSearchMatchesPinyinInitialsAndIdentity(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorksWithIdentity(dir.ID, "徐大川", CreatorIdentity{
		UID:        "uid-123",
		SecUID:     "MS4wLjABAAAA_SEARCH",
		ProfileURL: "https://www.bilibili.com/user/MS4wLjABAAAA_SEARCH",
	}, []WorkInput{{
		AwemeID: "1001",
		Title:   "title",
		Media:   []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "first.webp"), FileName: "first.webp"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1002",
		Title:   "title",
		Media:   []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "second.webp"), FileName: "second.webp"}},
	}}); err != nil {
		t.Fatal(err)
	}
	for _, keyword := range []string{"xudachuan", "XDC", "da chuan", "SEARCH"} {
		creators, err := store.Creators(keyword)
		if err != nil {
			t.Fatal(err)
		}
		if len(creators) != 1 || creators[0].ID != creator.ID {
			t.Fatalf("search %q returned %#v", keyword, creators)
		}
	}
}

func TestCreatorsWithOptionsAggregatesSortsFiltersAndLinks(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creatorA, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID:     "1001",
		Title:       "作品A",
		PublishedAt: "2026-01-02T00:00:00Z",
		Media:       []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "a.webp"), FileName: "a.webp"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	creatorB, _, err := store.UpsertCreatorWithWorks(dir.ID, "徐大川", []WorkInput{{
		AwemeID:     "1002",
		Title:       "作品B",
		PublishedAt: "2026-02-02T00:00:00Z",
		Media:       []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "b.webp"), FileName: "b.webp"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO creators(directory_id, name, work_count, search_text) VALUES(?, ?, 0, ?)`, dir.ID, "空UP主", buildSearchText("空UP主")); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCreatorLinks(creatorA.ID, []int64{creatorB.ID}); err != nil {
		t.Fatal(err)
	}
	admin, err := store.UserByUsername("ted")
	if err != nil {
		t.Fatal(err)
	}
	var workAID, workBID int64
	if err := store.db.QueryRow(`SELECT id FROM works WHERE aweme_id='1001'`).Scan(&workAID); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT id FROM works WHERE aweme_id='1002'`).Scan(&workBID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := store.RecordWorkView(admin.ID, workAID, "view", 0, 10, "image", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RecordWorkView(admin.ID, workBID, "view", 0, 10, "image", ""); err != nil {
		t.Fatal(err)
	}

	minWorks := int64(1)
	byViews, err := store.CreatorsWithOptions(CreatorListOptions{Sort: "views", Order: "desc", MinWorkCount: &minWorks})
	if err != nil {
		t.Fatal(err)
	}
	if len(byViews) != 2 {
		t.Fatalf("expected 2 creators after work count filter, got %#v", byViews)
	}
	if byViews[0].ID != creatorA.ID || byViews[0].ViewCount != 2 {
		t.Fatalf("view sort/aggregate mismatch: %#v", byViews)
	}
	if len(byViews[0].LinkedCreators) != 1 || byViews[0].LinkedCreators[0].ID != creatorB.ID {
		t.Fatalf("linked creators not loaded in list query: %#v", byViews[0].LinkedCreators)
	}
	byRecent, err := store.CreatorsWithOptions(CreatorListOptions{Sort: "updated_at", Order: "desc", MinWorkCount: &minWorks})
	if err != nil {
		t.Fatal(err)
	}
	if byRecent[0].ID != creatorB.ID || byRecent[0].LastWorkCreatedAt != "2026-02-02T00:00:00Z" {
		t.Fatalf("recent sort/aggregate mismatch: %#v", byRecent)
	}
}

func TestSearchIndexBackfillsExistingRows(t *testing.T) {
	t.Setenv("LOCALBILIBILI_BOOTSTRAP_USERNAME", "ted")
	t.Setenv("LOCALBILIBILI_BOOTSTRAP_PASSWORD", "ted")
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID:     "1001",
		Title:       "夏天训练",
		Description: "海边",
		MusicTitle:  "城市音乐",
		Tags:        []string{"腹肌"},
		Media:       []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "first.webp"), FileName: "first.webp"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE creators SET search_text=''`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE works SET search_text=''`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	creators, err := store.Creators("ayan")
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 || creators[0].Name != "阿言" {
		t.Fatalf("creator search after backfill = %#v", creators)
	}
	works, err := store.Feed(0, 0, "xiatian", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 || works[0].AwemeID != "1001" {
		t.Fatalf("feed search after backfill = %#v", works)
	}
}

func TestFavoriteFoldersAndFavorites(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "a.webp"), FileName: "a.webp"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := store.UserByUsername("ted")
	if err != nil {
		t.Fatal(err)
	}
	works, err := store.Feed(0, 0, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	workID := works[0].ID

	workFolders, err := store.FavoriteFolders(admin.ID, "work")
	if err != nil {
		t.Fatal(err)
	}
	creatorFolders, err := store.FavoriteFolders(admin.ID, "creator")
	if err != nil {
		t.Fatal(err)
	}
	if len(workFolders) != 1 || !workFolders[0].IsDefault || len(creatorFolders) != 1 || !creatorFolders[0].IsDefault {
		t.Fatalf("default folders missing: %#v %#v", workFolders, creatorFolders)
	}
	custom, err := store.CreateFavoriteFolder(admin.ID, "work", "健身")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateFavoriteFolder(admin.ID, custom.ID, "训练", boolPtr(true)); err != nil {
		t.Fatal(err)
	}
	workFolders, err = store.FavoriteFolders(admin.ID, "work")
	if err != nil {
		t.Fatal(err)
	}
	defaults := 0
	for _, folder := range workFolders {
		if folder.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("expected one default, got %#v", workFolders)
	}
	if err := store.AddWorkFavorite(admin.ID, workID, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.AddWorkFavorite(admin.ID, workID, 0); err != nil {
		t.Fatal(err)
	}
	status, err := store.FavoriteStatus(admin.ID, workID, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.WorkFolderIDs) != 1 {
		t.Fatalf("work favorite not deduped: %#v", status)
	}
	favoriteFeed, err := store.FeedWithOptions(FeedOptions{FavoriteFolderID: status.WorkFolderIDs[0], Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(favoriteFeed) != 1 || favoriteFeed[0].ID != workID {
		t.Fatalf("favorite feed = %#v", favoriteFeed)
	}
	if err := store.RemoveWorkFavorite(admin.ID, workID, status.WorkFolderIDs[0]); err != nil {
		t.Fatal(err)
	}
	status, err = store.FavoriteStatus(admin.ID, workID, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.WorkFolderIDs) != 0 {
		t.Fatalf("work favorite not removed: %#v", status)
	}

	if err := store.AddCreatorFavorite(admin.ID, creator.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.AddCreatorFavorite(admin.ID, creator.ID, 0); err != nil {
		t.Fatal(err)
	}
	status, err = store.FavoriteStatus(admin.ID, 0, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.CreatorFolderIDs) != 1 {
		t.Fatalf("creator favorite not deduped: %#v", status)
	}
	favoriteCreators, err := store.FavoriteCreators(admin.ID, status.CreatorFolderIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(favoriteCreators) != 1 || favoriteCreators[0].ID != creator.ID {
		t.Fatalf("favorite creators = %#v", favoriteCreators)
	}
	if err := store.RemoveCreatorFavorite(admin.ID, creator.ID, status.CreatorFolderIDs[0]); err != nil {
		t.Fatal(err)
	}
}

func TestCreatorLinksGroupFeedAndSummaries(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	creatorA, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品A",
		Media:   []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "a.webp"), FileName: "a.webp"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	creatorB, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言2", []WorkInput{{
		AwemeID: "1002",
		Title:   "作品B",
		Media:   []MediaInput{{Type: "image", FilePath: filepath.Join(t.TempDir(), "b.webp"), FileName: "b.webp"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetCreatorLinks(creatorA.ID, []int64{creatorB.ID}); err != nil {
		t.Fatal(err)
	}
	links, err := store.CreatorLinks(creatorA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links.Linked) != 1 || links.Linked[0].ID != creatorB.ID {
		t.Fatalf("linked = %#v", links.Linked)
	}
	feed, err := store.FeedWithOptions(FeedOptions{CreatorID: creatorA.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 2 {
		t.Fatalf("grouped feed = %#v", feed)
	}
	foundLinked := false
	for _, work := range feed {
		if len(work.LinkedCreators) == 1 && work.LinkedCreators[0].ID == creatorB.ID {
			foundLinked = true
		}
	}
	if !foundLinked {
		t.Fatalf("work linked creators missing: %#v", feed)
	}
}

func TestFeedWithMissingCreatorReturnsEmpty(t *testing.T) {
	store := newTestStore(t)
	feed, err := store.FeedWithOptions(FeedOptions{CreatorID: 1230, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 0 {
		t.Fatalf("missing creator feed = %#v", feed)
	}
}

func TestRecordWorkViewAggregatesDiagnostics(t *testing.T) {
	store := newTestStore(t)
	dir, err := store.SaveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	admin, err := store.UserByUsername("ted")
	if err != nil {
		t.Fatal(err)
	}
	creator, _, err := store.UpsertCreatorWithWorks(dir.ID, "阿言", []WorkInput{{
		AwemeID: "1001",
		Title:   "作品",
		Media:   []MediaInput{{Type: "video", FilePath: filepath.Join(t.TempDir(), "a.mp4"), FileName: "a.mp4"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	feed, err := store.Feed(0, 0, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	workID := feed[0].ID
	if err := store.RecordWorkView(admin.ID, workID, "view", 0, 10, "video", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordWorkView(admin.ID, workID, "seek", 4, 10, "video", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordWorkView(admin.ID, workID, "video_error", 5, 10, "video", "decode"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSystemEvent(SystemEventInput{
		EventType: "video_seekability_warning",
		Severity:  "warning",
		Message:   "修复失败，已保留原文件",
	}); err != nil {
		t.Fatal(err)
	}
	diagnostics, err := store.PlaybackDiagnostics(10)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics.TotalEvents != 3 || diagnostics.TotalViews != 1 || diagnostics.TotalErrors != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if len(diagnostics.TopCreators) != 1 || diagnostics.TopCreators[0].CreatorID != creator.ID {
		t.Fatalf("top creators = %#v", diagnostics.TopCreators)
	}
	if len(diagnostics.TopProblemWorks) != 1 || diagnostics.TopProblemWorks[0].WorkID != workID {
		t.Fatalf("top problem works = %#v", diagnostics.TopProblemWorks)
	}
	if len(diagnostics.RecentRepairWarnings) != 1 || diagnostics.RecentRepairWarnings[0].EventType != "video_seekability_warning" {
		t.Fatalf("recent warnings = %#v", diagnostics.RecentRepairWarnings)
	}
	creator, err = store.Creator(creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if creator.ViewCount != 1 {
		t.Fatalf("creator view count = %d", creator.ViewCount)
	}
}

func boolPtr(value bool) *bool {
	return &value
}
