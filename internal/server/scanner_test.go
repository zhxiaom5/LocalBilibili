package server

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAwemeIDAndMediaType(t *testing.T) {
	name := "2026-05-16_会好_7640372060313385727_1.webp"
	if got := ParseAwemeID(name); got != "7640372060313385727" {
		t.Fatalf("ParseAwemeID() = %q", got)
	}
	if got := ParseTitle(name); got != "会好 7640372060313385727" {
		t.Fatalf("ParseTitle() = %q", got)
	}
	if got := MediaType(name); got != "image" {
		t.Fatalf("MediaType() = %q", got)
	}
	if got := MediaType("clip.mp4"); got != "video" {
		t.Fatalf("MediaType(video) = %q", got)
	}
}

func TestValidateDirectoryRejectsFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDirectory(dir); err != nil {
		t.Fatalf("directory should validate: %v", err)
	}
	if err := ValidateDirectory(file); err == nil {
		t.Fatal("file path should be rejected")
	}
}

func TestFileBrowserListsOnlyVisibleDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "creator"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := (FileBrowser{}).List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "creator" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}

func TestAccessiblePathsAllowLocalDevelopmentWhenUnset(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !PathAllowed("/any/path", nil) {
		t.Fatal("empty accessible paths should allow local development paths")
	}
	if err := ValidateAccessibleDirectory(dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAccessibleDirectory(file, nil); err == nil {
		t.Fatal("file path should still be rejected")
	}
}

func TestFileBrowserRestrictsRootsAndListToAccessiblePaths(t *testing.T) {
	root := t.TempDir()
	allowedA := filepath.Join(root, "allowed-a")
	allowedB := filepath.Join(root, "allowed-b")
	child := filepath.Join(allowedA, "creator")
	blocked := filepath.Join(root, "blocked")
	for _, dir := range []string{allowedA, allowedB, child, blocked} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	browser := FileBrowser{allowedPaths: ParseAccessiblePaths(allowedA + string(os.PathListSeparator) + allowedB)}
	roots := browser.Roots()
	if len(roots) != 2 {
		t.Fatalf("roots = %#v", roots)
	}
	entries, err := browser.List(allowedA)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != child {
		t.Fatalf("entries = %#v", entries)
	}
	if _, err := browser.List(blocked); !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("blocked err = %v, want ErrPathNotAllowed", err)
	}
}

func TestAccessibleDirectoryRejectsSiblingPrefix(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "media")
	sibling := filepath.Join(root, "media-other")
	if err := os.Mkdir(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if !PathAllowed(filepath.Join(allowed, "child"), []string{allowed}) {
		t.Fatal("child should be allowed")
	}
	if PathAllowed(sibling, []string{allowed}) {
		t.Fatal("sibling sharing path prefix should not be allowed")
	}
}

func TestScanCreatorParsesMetadataAndMedia(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "2026-05-16_会好_7640372060313385727_data.json")
	if err := os.WriteFile(jsonPath, []byte(`{"aweme_id":"7640372060313385727","desc":"会好#测试","create_time":1778912745,"music":{"title":"音乐"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-05-16_会好_7640372060313385727_1.webp"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	works, err := scanCreator(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 {
		t.Fatalf("works len = %d", len(works))
	}
	work := works[0]
	if work.Description != "会好#测试" || work.MusicTitle != "音乐" || len(work.Media) != 1 {
		t.Fatalf("unexpected work: %#v", work)
	}
	if _, err := time.Parse(time.RFC3339, work.PublishedAt); err != nil {
		t.Fatalf("published_at not RFC3339: %q", work.PublishedAt)
	}
}

func TestScanCreatorReadsLocalBilibiliMetadata(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "2026-07-01_B站长视频_BV1abc")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "media_001.mp4"), testMP4WithMoovAfterMdat(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "cover.jpg"), []byte("cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := `{
  "work_type": "video_long",
  "bvid": "BV1abc",
  "title": "B站长视频",
  "description": "一条长视频",
  "published_at": "2026-07-01T12:00:00Z",
  "source_url": "https://www.bilibili.com/video/BV1abc",
  "cover_file": "cover.jpg",
  "tags": ["测试"],
  "creator": {
    "mid": "12345",
    "name": "测试UP",
    "space_url": "https://space.bilibili.com/12345",
    "avatar_url": "https://example.com/avatar.jpg",
    "bio": "签名"
  },
  "media": [
    {"type": "video", "file": "media_001.mp4"}
  ]
}`
	if err := os.WriteFile(filepath.Join(workDir, "metadata.localbilibili.json"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}

	identity, works, warnings, err := scanCreatorWithIdentityAndWarnings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if identity.SecUID != "12345" || identity.ProfileURL != "https://space.bilibili.com/12345" || identity.Name != "测试UP" {
		t.Fatalf("identity = %#v", identity)
	}
	if len(works) != 1 {
		t.Fatalf("works len = %d: %#v", len(works), works)
	}
	work := works[0]
	if work.AwemeID != "BV1abc" || work.WorkType != "video_long" || work.Title != "B站长视频" || work.Description != "一条长视频" || work.SourceURL == "" {
		t.Fatalf("work = %#v", work)
	}
	if work.CoverFile != filepath.Join(workDir, "cover.jpg") {
		t.Fatalf("cover file = %q", work.CoverFile)
	}
	if len(work.Media) != 1 || work.Media[0].Type != "video" || work.Media[0].FilePath != filepath.Join(workDir, "media_001.mp4") {
		t.Fatalf("media = %#v", work.Media)
	}
}

func TestScanDirectoryUsesLocalBilibiliMetadataCreatorName(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	creatorDir := filepath.Join(root, "folder-name")
	workDir := filepath.Join(creatorDir, "2026-07-01_B站长视频_BV1abc")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "media_001.mp4"), testMP4WithMoovAfterMdat(), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := `{
  "work_type": "video_long",
  "bvid": "BV1abc",
  "title": "B站长视频",
  "creator": {"mid": "12345", "name": "真实UP", "space_url": "https://space.bilibili.com/12345"},
  "media": [{"type": "video", "file": "media_001.mp4"}]
}`
	if err := os.WriteFile(filepath.Join(workDir, "metadata.localbilibili.json"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	scanner := NewScanner(store, NewEventHub())
	scanner.Enqueue(dir.ID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !scanner.Status().Running && scanner.Status().State == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	creators, err := store.Creators("")
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 || creators[0].Name != "真实UP" {
		t.Fatalf("creators = %#v", creators)
	}
}

func TestScanCreatorReadsLocalBilibiliImageTextAndSkipsUnsupported(t *testing.T) {
	dir := t.TempDir()
	imageDir := filepath.Join(dir, "2026-07-01_图文_dyn123")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imageDir, "image_001.jpg"), []byte("img1"), 0o644); err != nil {
		t.Fatal(err)
	}
	imageMeta := `{
  "work_type": "image_text",
  "dynamic_id": "dyn123",
  "title": "图文作品",
  "description": "图文描述",
  "published_at": "2026-07-01T12:00:00Z",
  "media": [{"type": "image", "file": "image_001.jpg"}]
}`
	if err := os.WriteFile(filepath.Join(imageDir, "metadata.localbilibili.json"), []byte(imageMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	liveDir := filepath.Join(dir, "live")
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	liveMeta := `{"work_type":"live","bvid":"live-1","title":"直播","media":[{"type":"video","file":"media_001.mp4"}]}`
	if err := os.WriteFile(filepath.Join(liveDir, "metadata.localbilibili.json"), []byte(liveMeta), 0o644); err != nil {
		t.Fatal(err)
	}

	_, works, warnings, err := scanCreatorWithIdentityAndWarnings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 || works[0].AwemeID != "dyn123" || works[0].WorkType != "image_text" || len(works[0].Media) != 1 || works[0].Media[0].Type != "image" {
		t.Fatalf("works = %#v", works)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "跳过不支持的B站作品类型") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestScannerSkipsEmptyCreatorDirectory(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	creatorDir := filepath.Join(root, "valid")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(creatorDir, "2026-05-16_作品_100100000000.webp"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := store.SaveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	scanner := NewScanner(store, NewEventHub())
	scanner.Enqueue(dir.ID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !scanner.Status().Running && scanner.Status().State == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	creators, err := store.Creators()
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 1 || creators[0].Name != "valid" {
		t.Fatalf("empty creator should not be inserted: %#v", creators)
	}
}

func TestScanCreatorReadsNestedDownloaderOutput(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "UP主", "post", "2026-05-16_嵌套作品_7640372060313385727")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"aweme_id":"7640372060313385727","desc":"嵌套作品#测试","author":{"sec_uid":"MS4wLjABAAAA_NESTED"},"music":{"title":"音乐"}}`
	if err := os.WriteFile(filepath.Join(workDir, "2026-05-16_嵌套作品_7640372060313385727_data.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "2026-05-16_嵌套作品_7640372060313385727_1.jpg"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	identity, works, err := scanCreatorWithIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if identity.SecUID != "MS4wLjABAAAA_NESTED" {
		t.Fatalf("identity = %#v", identity)
	}
	if len(works) != 1 {
		t.Fatalf("works len = %d: %#v", len(works), works)
	}
	if works[0].AwemeID != "7640372060313385727" || works[0].Description != "嵌套作品#测试" || len(works[0].Media) != 1 {
		t.Fatalf("unexpected nested work: %#v", works[0])
	}
}

func TestScanCreatorUsesMetadataAwemeIDWhenFilenameHasShortSuffix(t *testing.T) {
	dir := t.TempDir()
	data := `{"aweme_id":"7640510208167103738","desc":"短后缀作品","author":{"sec_uid":"MS4wLjABAAAA_SHORT"}}`
	if err := os.WriteFile(filepath.Join(dir, "2026-05-16_短后缀作品_76_data.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-05-16_短后缀作品_76_1.webp"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	identity, works, err := scanCreatorWithIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if identity.SecUID != "MS4wLjABAAAA_SHORT" {
		t.Fatalf("identity = %#v", identity)
	}
	if len(works) != 1 || works[0].AwemeID != "7640510208167103738" || len(works[0].Media) != 1 {
		t.Fatalf("works = %#v", works)
	}
}

func TestScanCreatorUsesDirectoryMetadataWhenFilenamesOmitAwemeID(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "creator", "post", "2026-05-31_没有文件名作品ID")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"aweme_id":"7645923950719346090","desc":"目录元数据作品","author":{"sec_uid":"MS4wLjABAAAA_DIR_META"}}`
	if err := os.WriteFile(filepath.Join(workDir, "2026-05-31_没有文件名作品ID_data.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "2026-05-31_没有文件名作品ID_1.jpg"), []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	identity, works, err := scanCreatorWithIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if identity.SecUID != "MS4wLjABAAAA_DIR_META" {
		t.Fatalf("identity = %#v", identity)
	}
	if len(works) != 1 || works[0].AwemeID != "7645923950719346090" || len(works[0].Media) != 1 {
		t.Fatalf("works = %#v", works)
	}
}

func TestScanCreatorRepairsMP4Faststart(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "2026-05-16_测试_7645923950719346090.mp4")
	if err := os.WriteFile(videoPath, testMP4WithMoovAfterMdat(), 0o644); err != nil {
		t.Fatal(err)
	}
	works, warnings, err := scanCreatorWithWarnings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if len(works) != 1 || len(works[0].Media) != 1 {
		t.Fatalf("works = %#v", works)
	}
	repaired, err := os.ReadFile(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	moov := bytes.Index(repaired, []byte("moov"))
	mdat := bytes.Index(repaired, []byte("mdat"))
	if moov < 0 || mdat < 0 || moov > mdat {
		t.Fatalf("mp4 should be faststart after scan, moov=%d mdat=%d", moov, mdat)
	}
}

func TestScanCreatorWarnsWhenVideoRepairFailsButKeepsWork(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "2026-05-16_测试_7645923950719346090.mp4")
	if err := os.WriteFile(videoPath, []byte("not an mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	works, warnings, err := scanCreatorWithWarnings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(works) != 1 || len(works[0].Media) != 1 {
		t.Fatalf("works = %#v", works)
	}
	if len(warnings) != 1 || warnings[0] == "" {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func testMP4WithMoovAfterMdat() []byte {
	return bytes.Join([][]byte{
		mp4TestBox("ftyp", []byte("isom")),
		mp4TestBox("mdat", []byte("video-payload")),
		mp4TestBox("moov", testMoovPayload(25)),
	}, nil)
}

func testMoovPayload(chunkOffset uint32) []byte {
	stcoPayload := make([]byte, 12)
	binary.BigEndian.PutUint32(stcoPayload[4:8], 1)
	binary.BigEndian.PutUint32(stcoPayload[8:12], chunkOffset)
	return mp4TestBox("trak", mp4TestBox("mdia", mp4TestBox("minf", mp4TestBox("stbl", mp4TestBox("stco", stcoPayload)))))
}

func mp4TestBox(typ string, payload []byte) []byte {
	size := uint32(len(payload) + 8)
	box := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(box[:4], size)
	copy(box[4:8], []byte(typ))
	copy(box[8:], payload)
	return box
}
