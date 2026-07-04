package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrPathNotAllowed = errors.New("path is not in authorized directories")

type FileBrowser struct {
	allowedPaths []string
}

type FSEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func ValidateDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("path is not a directory")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	_ = entries
	return nil
}

func NewFileBrowserFromEnv() FileBrowser {
	return FileBrowser{allowedPaths: ParseAccessiblePaths(os.Getenv("TRIM_DATA_ACCESSIBLE_PATHS"))}
}

func ParseAccessiblePaths(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, string(os.PathListSeparator))
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		clean := filepath.Clean(strings.TrimSpace(part))
		if clean == "" || clean == "." {
			continue
		}
		paths = append(paths, clean)
	}
	return paths
}

func PathAllowed(path string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	clean := filepath.Clean(path)
	for _, root := range allowed {
		root = filepath.Clean(root)
		if clean == root {
			return true
		}
		rel, err := filepath.Rel(root, clean)
		if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func ValidateAccessibleDirectory(path string, allowed []string) error {
	if err := ValidateDirectory(path); err != nil {
		return err
	}
	if !PathAllowed(path, allowed) {
		return ErrPathNotAllowed
	}
	return nil
}

func (browser FileBrowser) Roots() []FSEntry {
	if len(browser.allowedPaths) > 0 {
		roots := make([]FSEntry, 0, len(browser.allowedPaths))
		for _, path := range browser.allowedPaths {
			if err := ValidateDirectory(path); err != nil {
				continue
			}
			roots = append(roots, FSEntry{Path: path, Name: filepath.Base(path)})
		}
		return roots
	}
	roots := []FSEntry{}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, FSEntry{Path: home, Name: "Home"})
	}
	wd, err := os.Getwd()
	if err == nil {
		roots = append(roots, FSEntry{Path: wd, Name: "Project"})
	}
	roots = append(roots, FSEntry{Path: string(filepath.Separator), Name: "Root"})
	return roots
}

func (browser FileBrowser) List(path string) ([]FSEntry, error) {
	if err := ValidateAccessibleDirectory(path, browser.allowedPaths); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var dirs []FSEntry
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(path, name)
		if err := ValidateAccessibleDirectory(full, browser.allowedPaths); err != nil {
			continue
		}
		dirs = append(dirs, FSEntry{Path: full, Name: name})
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	return dirs, nil
}

func (browser FileBrowser) Validate(path string) error {
	return ValidateAccessibleDirectory(path, browser.allowedPaths)
}

type Scanner struct {
	store *Store
	hub   *EventHub

	mu      sync.Mutex
	queue   []int64
	running bool
	status  ScanStatus
}

func NewScanner(store *Store, hub *EventHub) *Scanner {
	return &Scanner{store: store, hub: hub, status: ScanStatus{State: "idle"}}
}

func (s *Scanner) Enqueue(directoryIDs ...int64) ScanStatus {
	s.mu.Lock()
	for _, id := range directoryIDs {
		if id > 0 {
			s.queue = append(s.queue, id)
		}
	}
	if !s.running {
		s.running = true
		go s.run()
	}
	status := s.snapshotLocked()
	s.mu.Unlock()
	s.hub.Publish(ScanEvent{Type: "queued", Status: status})
	return status
}

func (s *Scanner) Status() ScanStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Scanner) run() {
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.running = false
			s.status.Running = false
			if s.status.State != "completed" && s.status.State != "error" {
				s.status.State = "idle"
			}
			status := s.snapshotLocked()
			s.mu.Unlock()
			s.hub.Publish(ScanEvent{Type: "idle", Status: status})
			return
		}
		directoryID := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		s.scanDirectory(directoryID)
	}
}

func (s *Scanner) scanDirectory(directoryID int64) {
	dir, err := s.store.Directory(directoryID)
	if err != nil {
		s.recordError("load directory failed: " + err.Error())
		return
	}
	_ = s.store.MarkDirectoryStatus(directoryID, "scanning")
	creatorDirs, err := readCreatorDirs(dir.Path)
	if err != nil {
		s.recordError("read directory failed: " + err.Error())
		_ = s.store.MarkDirectoryStatus(directoryID, "error")
		return
	}
	s.mu.Lock()
	s.status = ScanStatus{
		Running:            true,
		QueueLength:        len(s.queue),
		CurrentTaskID:      time.Now().UTC().Format("20060102150405.000000000"),
		CurrentDirectoryID: dir.ID,
		CurrentDirectory:   dir.Path,
		TotalCreators:      len(creatorDirs),
		State:              "scanning",
	}
	status := s.snapshotLocked()
	s.mu.Unlock()
	s.hub.Publish(ScanEvent{Type: "started", Status: status})

	for _, creatorDir := range creatorDirs {
		s.mu.Lock()
		s.status.CurrentCreator = creatorDir.Name()
		status = s.snapshotLocked()
		s.mu.Unlock()
		s.hub.Publish(ScanEvent{Type: "creator_started", Status: status})

		identity, works, warnings, err := scanCreatorWithIdentityAndWarnings(filepath.Join(dir.Path, creatorDir.Name()))
		if err != nil {
			s.recordError(creatorDir.Name() + ": " + err.Error())
			continue
		}
		for _, warning := range warnings {
			s.recordError(creatorDir.Name() + ": " + warning)
		}
		if len(works) == 0 {
			s.mu.Lock()
			s.status.ScannedCreators++
			if s.status.TotalCreators > 0 {
				s.status.Progress = int(float64(s.status.ScannedCreators) / float64(s.status.TotalCreators) * 100)
			}
			status = s.snapshotLocked()
			s.mu.Unlock()
			s.hub.Publish(ScanEvent{Type: "creator_completed", Status: status})
			continue
		}
		creatorName := strings.TrimSpace(identity.Name)
		if creatorName == "" {
			creatorName = creatorDir.Name()
		}
		creator, count, err := s.store.UpsertCreatorWithWorksWithIdentity(dir.ID, creatorName, identity, works)
		if err != nil {
			s.recordError(creatorDir.Name() + ": " + err.Error())
			continue
		}
		s.mu.Lock()
		s.status.ScannedCreators++
		s.status.ScannedWorks += count
		if s.status.TotalCreators > 0 {
			s.status.Progress = int(float64(s.status.ScannedCreators) / float64(s.status.TotalCreators) * 100)
		}
		status = s.snapshotLocked()
		s.mu.Unlock()
		s.hub.Publish(ScanEvent{Type: "creator_completed", Status: status, Creator: &creator})
	}
	_ = s.store.MarkDirectoryScanned(dir.ID)
	s.mu.Lock()
	s.status.State = "completed"
	s.status.Progress = 100
	status = s.snapshotLocked()
	s.mu.Unlock()
	s.hub.Publish(ScanEvent{Type: "completed", Status: status})
}

func (s *Scanner) recordError(message string) {
	s.mu.Lock()
	s.status.Errors = append(s.status.Errors, message)
	s.status.State = "error"
	status := s.snapshotLocked()
	s.mu.Unlock()
	s.hub.Publish(ScanEvent{Type: "error", Status: status})
}

func (s *Scanner) snapshotLocked() ScanStatus {
	status := s.status
	status.Running = s.running
	status.QueueLength = len(s.queue)
	status.Errors = append([]string(nil), s.status.Errors...)
	return status
}

func readCreatorDirs(path string) ([]fs.DirEntry, error) {
	if err := ValidateDirectory(path); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var creators []fs.DirEntry
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			creators = append(creators, entry)
		}
	}
	sort.Slice(creators, func(i, j int) bool {
		return strings.ToLower(creators[i].Name()) < strings.ToLower(creators[j].Name())
	})
	return creators, nil
}

func scanCreator(path string) ([]WorkInput, error) {
	_, works, _, err := scanCreatorWithIdentityAndWarnings(path)
	return works, err
}

func scanCreatorWithIdentity(path string) (CreatorIdentity, []WorkInput, error) {
	identity, works, _, err := scanCreatorWithIdentityAndWarnings(path)
	return identity, works, err
}

func scanCreatorWithWarnings(path string) ([]WorkInput, []string, error) {
	_, works, warnings, err := scanCreatorWithIdentityAndWarnings(path)
	return works, warnings, err
}

func scanCreatorWithIdentityAndWarnings(path string) (CreatorIdentity, []WorkInput, []string, error) {
	if identity, works, warnings, found, err := scanLocalBilibiliCreator(path); found || err != nil {
		return identity, works, warnings, err
	}
	grouped := map[string][]string{}
	directoryGrouped := map[string][]string{}
	var warnings []string
	if err := filepath.WalkDir(path, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if filePath != path && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || name == "download_manifest.jsonl" {
			return nil
		}
		awemeID := ParseAwemeID(name)
		if awemeID == "" {
			if strings.EqualFold(filepath.Ext(name), ".json") || MediaType(name) != "" {
				dir := filepath.Dir(filePath)
				directoryGrouped[dir] = append(directoryGrouped[dir], filePath)
			}
			return nil
		}
		grouped[awemeID] = append(grouped[awemeID], filePath)
		return nil
	}); err != nil {
		return CreatorIdentity{}, nil, nil, err
	}
	for dir, files := range directoryGrouped {
		awemeID := ""
		for _, file := range files {
			if !strings.EqualFold(filepath.Ext(file), ".json") {
				continue
			}
			if meta := parseMetadata(file); meta.AwemeID != "" {
				awemeID = meta.AwemeID
				break
			}
		}
		if awemeID == "" {
			continue
		}
		grouped[awemeID] = append(grouped[awemeID], files...)
		delete(directoryGrouped, dir)
	}
	var identity CreatorIdentity
	var works []WorkInput
	for awemeID, files := range grouped {
		sort.Strings(files)
		work := WorkInput{AwemeID: awemeID, Title: awemeID, Description: "", Tags: []string{}}
		var media []MediaInput
		var coverCandidate string
		for _, file := range files {
			name := filepath.Base(file)
			if strings.EqualFold(filepath.Ext(name), ".json") {
				meta := parseMetadata(file)
				identity = mergeCreatorIdentity(identity, meta.CreatorIdentity)
				if meta.AwemeID != "" {
					work.AwemeID = meta.AwemeID
				}
				if meta.Title != "" {
					work.Title = meta.Title
				}
				work.Description = meta.Description
				work.MusicTitle = meta.MusicTitle
				work.SourceURL = meta.SourceURL
				work.CoverURL = meta.CoverURL
				if work.CoverFile == "" {
					work.CoverFile = meta.CoverFile
				}
				work.Tags = meta.Tags
				work.PublishedAt = meta.PublishedAt
				continue
			}
			mediaType := MediaType(name)
			if mediaType == "" {
				continue
			}
			if mediaType == "video" {
				if _, err := ensureVideoSeekable(file); err != nil {
					warnings = append(warnings, "视频兼容处理失败 "+name+": "+err.Error())
				}
			}
			if mediaType == "image" && coverCandidate == "" {
				coverCandidate = file
			}
			media = append(media, MediaInput{Type: mediaType, FilePath: file, FileName: name, Ordinal: len(media)})
			if work.Title == awemeID {
				work.Title = ParseTitle(name)
			}
		}
		if len(media) == 0 {
			continue
		}
		if work.CoverFile == "" && coverCandidate != "" {
			work.CoverFile = coverCandidate
		}
		work.Media = media
		works = append(works, work)
	}
	sort.Slice(works, func(i, j int) bool {
		return works[i].PublishedAt > works[j].PublishedAt
	})
	return identity, works, warnings, nil
}

type localBilibiliMetadata struct {
	WorkType    string   `json:"work_type"`
	BVID        string   `json:"bvid"`
	DynamicID   string   `json:"dynamic_id"`
	AID         string   `json:"aid"`
	CID         string   `json:"cid"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	PublishedAt string   `json:"published_at"`
	SourceURL   string   `json:"source_url"`
	CoverURL    string   `json:"cover_url"`
	CoverFile   string   `json:"cover_file"`
	Tags        []string `json:"tags"`
	Creator     struct {
		MID        string `json:"mid"`
		Name       string `json:"name"`
		SpaceURL   string `json:"space_url"`
		AvatarURL  string `json:"avatar_url"`
		AvatarFile string `json:"avatar_file"`
		Bio        string `json:"bio"`
	} `json:"creator"`
	Media []struct {
		Type string `json:"type"`
		File string `json:"file"`
	} `json:"media"`
}

func scanLocalBilibiliCreator(path string) (CreatorIdentity, []WorkInput, []string, bool, error) {
	var metadataFiles []string
	if err := filepath.WalkDir(path, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if filePath != path && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if name == "metadata.localbilibili.json" {
			metadataFiles = append(metadataFiles, filePath)
		}
		return nil
	}); err != nil {
		return CreatorIdentity{}, nil, nil, false, err
	}
	if len(metadataFiles) == 0 {
		return CreatorIdentity{}, nil, nil, false, nil
	}
	sort.Strings(metadataFiles)
	var identity CreatorIdentity
	var works []WorkInput
	var warnings []string
	for _, file := range metadataFiles {
		meta, err := parseLocalBilibiliMetadata(file)
		if err != nil {
			warnings = append(warnings, filepath.Base(filepath.Dir(file))+": "+err.Error())
			continue
		}
		work, nextIdentity, skipped, err := localBilibiliMetadataToWork(file, meta)
		if err != nil {
			warnings = append(warnings, filepath.Base(filepath.Dir(file))+": "+err.Error())
			continue
		}
		if skipped != "" {
			warnings = append(warnings, skipped)
			continue
		}
		identity = mergeCreatorIdentity(identity, nextIdentity)
		works = append(works, work)
	}
	sort.Slice(works, func(i, j int) bool {
		return works[i].PublishedAt > works[j].PublishedAt
	})
	return identity, works, warnings, true, nil
}

func parseLocalBilibiliMetadata(path string) (localBilibiliMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return localBilibiliMetadata{}, err
	}
	var meta localBilibiliMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return localBilibiliMetadata{}, err
	}
	return meta, nil
}

func localBilibiliMetadataToWork(metadataPath string, meta localBilibiliMetadata) (WorkInput, CreatorIdentity, string, error) {
	workType := strings.TrimSpace(meta.WorkType)
	if !isSupportedLocalBilibiliWorkType(workType) {
		return WorkInput{}, CreatorIdentity{}, "跳过不支持的B站作品类型: " + fallbackString(workType, "unknown"), nil
	}
	workID := firstNonEmptyLocalBilibili(meta.BVID, meta.DynamicID, meta.AID, meta.CID)
	if workID == "" {
		return WorkInput{}, CreatorIdentity{}, "", errors.New("B站作品缺少 bvid/dynamic_id/aid/cid")
	}
	baseDir := filepath.Dir(metadataPath)
	title := strings.TrimSpace(meta.Title)
	if title == "" {
		title = workID
	}
	work := WorkInput{
		AwemeID:     workID,
		WorkType:    workType,
		Title:       title,
		Description: strings.TrimSpace(meta.Description),
		SourceURL:   strings.TrimSpace(meta.SourceURL),
		CoverURL:    strings.TrimSpace(meta.CoverURL),
		Tags:        append([]string(nil), meta.Tags...),
		PublishedAt: normalizePublishedAt(meta.PublishedAt),
	}
	if strings.TrimSpace(meta.CoverFile) != "" {
		work.CoverFile = resolveLocalBilibiliPath(baseDir, meta.CoverFile)
	}
	for _, item := range meta.Media {
		mediaType := strings.TrimSpace(item.Type)
		if mediaType == "" {
			mediaType = MediaType(item.File)
		}
		if !localBilibiliMediaAllowed(workType, mediaType) {
			continue
		}
		filePath := resolveLocalBilibiliPath(baseDir, item.File)
		if strings.TrimSpace(item.File) == "" {
			continue
		}
		if _, err := os.Stat(filePath); err != nil {
			return WorkInput{}, CreatorIdentity{}, "", err
		}
		if mediaType == "video" {
			if _, err := ensureVideoSeekable(filePath); err != nil {
				return WorkInput{}, CreatorIdentity{}, "", errors.New("视频兼容处理失败 " + filepath.Base(filePath) + ": " + err.Error())
			}
		}
		work.Media = append(work.Media, MediaInput{Type: mediaType, FilePath: filePath, FileName: filepath.Base(filePath), Ordinal: len(work.Media)})
		if work.CoverFile == "" && mediaType == "image" {
			work.CoverFile = filePath
		}
	}
	if len(work.Media) == 0 {
		return WorkInput{}, CreatorIdentity{}, "", errors.New("B站作品没有符合类型的本地媒体")
	}
	identity := CreatorIdentity{
		Name:       strings.TrimSpace(meta.Creator.Name),
		UID:        strings.TrimSpace(meta.Creator.MID),
		SecUID:     strings.TrimSpace(meta.Creator.MID),
		ProfileURL: strings.TrimSpace(meta.Creator.SpaceURL),
		AvatarURL:  strings.TrimSpace(meta.Creator.AvatarURL),
		AvatarFile: resolveOptionalLocalBilibiliPath(baseDir, meta.Creator.AvatarFile),
		Bio:        strings.TrimSpace(meta.Creator.Bio),
	}
	if identity.ProfileURL == "" && identity.SecUID != "" {
		identity.ProfileURL = "https://space.bilibili.com/" + identity.SecUID
	}
	return work, identity, "", nil
}

func isSupportedLocalBilibiliWorkType(workType string) bool {
	switch workType {
	case "video_long", "video_short", "image_text":
		return true
	default:
		return false
	}
}

func localBilibiliMediaAllowed(workType, mediaType string) bool {
	switch workType {
	case "video_long", "video_short":
		return mediaType == "video"
	case "image_text":
		return mediaType == "image"
	default:
		return false
	}
}

func resolveLocalBilibiliPath(baseDir, value string) string {
	value = strings.TrimSpace(value)
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(baseDir, value)
}

func resolveOptionalLocalBilibiliPath(baseDir, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return resolveLocalBilibiliPath(baseDir, value)
}

func normalizePublishedAt(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return value
}

func firstNonEmptyLocalBilibili(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func mergeCreatorIdentity(current, next CreatorIdentity) CreatorIdentity {
	if strings.TrimSpace(current.Name) == "" {
		current.Name = next.Name
	}
	if strings.TrimSpace(current.UID) == "" {
		current.UID = next.UID
	}
	if strings.TrimSpace(current.SecUID) == "" {
		current.SecUID = next.SecUID
	}
	if strings.TrimSpace(current.ProfileURL) == "" {
		current.ProfileURL = next.ProfileURL
	}
	if strings.TrimSpace(current.AvatarURL) == "" {
		current.AvatarURL = next.AvatarURL
	}
	if strings.TrimSpace(current.AvatarFile) == "" {
		current.AvatarFile = next.AvatarFile
	}
	if strings.TrimSpace(current.Bio) == "" {
		current.Bio = next.Bio
	}
	return current
}

func ParseAwemeID(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(base, "_")
	if len(parts) < 2 {
		return ""
	}
	last := parts[len(parts)-1]
	if last == "data" && len(parts) >= 2 && isDigits(parts[len(parts)-2]) {
		return parts[len(parts)-2]
	}
	if isDigits(last) {
		if len(parts) >= 3 && parts[len(parts)-2] == "live" && isDigits(parts[len(parts)-3]) {
			return parts[len(parts)-3]
		}
		if len(parts) >= 2 && isDigits(parts[len(parts)-2]) {
			return parts[len(parts)-2]
		}
		if len(last) >= 4 {
			return last
		}
	}
	return ""
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func ParseTitle(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(base, "_")
	if len(parts) < 3 {
		return base
	}
	return strings.Join(parts[1:len(parts)-1], " ")
}

func MediaType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".mov", ".m4v", ".webm":
		return "video"
	case ".webp", ".jpg", ".jpeg", ".png", ".gif":
		return "image"
	case ".mp3", ".m4a", ".aac", ".wav", ".flac":
		return "audio"
	default:
		return ""
	}
}

type metadata struct {
	AwemeID         string
	Title           string
	Description     string
	MusicTitle      string
	SourceURL       string
	CoverURL        string
	CoverFile       string
	Tags            []string
	PublishedAt     string
	CreatorIdentity CreatorIdentity
}

func parseMetadata(path string) metadata {
	var raw struct {
		AwemeID    string `json:"aweme_id"`
		Desc       string `json:"desc"`
		CreateTime int64  `json:"create_time"`
		ShareURL   string `json:"share_url"`
		Music      struct {
			Title string `json:"title"`
		} `json:"music"`
		Author struct {
			UID       string `json:"uid"`
			SecUID    string `json:"sec_uid"`
			Nickname  string `json:"nickname"`
			Signature string `json:"signature"`
			Avatar300 struct {
				URLList []string `json:"url_list"`
			} `json:"avatar_300x300"`
			AvatarThumb struct {
				URLList []string `json:"url_list"`
			} `json:"avatar_thumb"`
			ShareInfo struct {
				ShareURL string `json:"share_url"`
			} `json:"share_info"`
		} `json:"author"`
		TextExtra []struct {
			HashtagName string `json:"hashtag_name"`
		} `json:"text_extra"`
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &raw) != nil {
		return metadata{}
	}
	meta := metadata{
		AwemeID:     raw.AwemeID,
		Title:       raw.Desc,
		Description: raw.Desc,
		MusicTitle:  raw.Music.Title,
		SourceURL:   raw.ShareURL,
		CreatorIdentity: CreatorIdentity{
			Name:       raw.Author.Nickname,
			UID:        raw.Author.UID,
			SecUID:     raw.Author.SecUID,
			ProfileURL: raw.Author.ShareInfo.ShareURL,
			Bio:        raw.Author.Signature,
		},
	}
	if len(raw.Author.Avatar300.URLList) > 0 {
		meta.CreatorIdentity.AvatarURL = raw.Author.Avatar300.URLList[0]
	} else if len(raw.Author.AvatarThumb.URLList) > 0 {
		meta.CreatorIdentity.AvatarURL = raw.Author.AvatarThumb.URLList[0]
	}
	if meta.CreatorIdentity.ProfileURL == "" && meta.CreatorIdentity.SecUID != "" {
		meta.CreatorIdentity.ProfileURL = "https://www.bilibili.com/user/" + meta.CreatorIdentity.SecUID
	}
	if meta.SourceURL == "" {
		meta.SourceURL = meta.CreatorIdentity.ProfileURL
	}
	if raw.CreateTime > 0 {
		meta.PublishedAt = time.Unix(raw.CreateTime, 0).Format(time.RFC3339)
	}
	for _, item := range raw.TextExtra {
		if item.HashtagName != "" {
			meta.Tags = append(meta.Tags, item.HashtagName)
		}
	}
	if len(meta.Tags) == 0 {
		meta.Tags = hashtags(raw.Desc)
	}
	return meta
}

func hashtags(desc string) []string {
	parts := strings.Split(desc, "#")
	if len(parts) < 2 {
		return nil
	}
	var tags []string
	for _, part := range parts[1:] {
		tag := strings.Fields(part)
		if len(tag) > 0 {
			tags = append(tags, tag[0])
		}
	}
	return tags
}
