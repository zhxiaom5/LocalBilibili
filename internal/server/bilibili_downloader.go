package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const bilibiliRequestAttempts = 3

type BilibiliDownloader struct {
	BaseURL         string
	VideoAPIBaseURL string
	SpaceAPIBaseURL string
	HTTPClient      *http.Client
}

type permanentMediaError struct {
	Title  string
	Status int
	File   string
}

func (e permanentMediaError) Error() string {
	file := strings.TrimSpace(e.File)
	if file == "" {
		file = strings.TrimSpace(e.Title)
	}
	if file == "" {
		file = "媒体"
	}
	if e.Status > 0 {
		return fmt.Sprintf("媒体下载失败 %s: HTTP %d", file, e.Status)
	}
	return fmt.Sprintf("媒体下载失败 %s", file)
}

type permanentYTDLPError struct {
	Message string
}

func (e permanentYTDLPError) Error() string {
	return "yt-dlp 执行失败: " + sanitizeYTDLPError(e.Message)
}

type bilibiliUser struct {
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
}

type bilibiliUserResponse struct {
	User       bilibiliUser `json:"user"`
	StatusMsg  string       `json:"status_msg"`
	StatusCode int          `json:"status_code"`
}

type bilibiliCardResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Card struct {
			MID  string `json:"mid"`
			Name string `json:"name"`
			Face string `json:"face"`
			Sign string `json:"sign"`
		} `json:"card"`
	} `json:"data"`
}

type bilibiliVideoViewResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		BVID    string `json:"bvid"`
		CID     int64  `json:"cid"`
		Title   string `json:"title"`
		Desc    string `json:"desc"`
		Pubdate int64  `json:"pubdate"`
	} `json:"data"`
}

type bilibiliVideoPlayURLResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Quality int `json:"quality"`
		DURL    []struct {
			URL    string   `json:"url"`
			Backup []string `json:"backup_url"`
			Size   int64    `json:"size"`
			Length int64    `json:"length"`
		} `json:"durl"`
	} `json:"data"`
}

type bilibiliSpaceArchiveResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		List struct {
			Videos []bilibiliSpaceArchiveVideo `json:"vlist"`
		} `json:"list"`
		Page struct {
			Count int `json:"count"`
			PN    int `json:"pn"`
			PS    int `json:"ps"`
		} `json:"page"`
	} `json:"data"`
}

type bilibiliSpaceArchiveVideo struct {
	AID            int64  `json:"aid"`
	BVID           string `json:"bvid"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Pic            string `json:"pic"`
	Author         string `json:"author"`
	MID            int64  `json:"mid"`
	Created        int64  `json:"created"`
	Pubdate        int64  `json:"pubdate"`
	CTime          int64  `json:"ctime"`
	Length         string `json:"length"`
	Duration       int64  `json:"duration"`
	IsPay          int    `json:"is_pay"`
	IsUnionVideo   int    `json:"is_union_video"`
	IsSteinsGate   int    `json:"is_steins_gate"`
	IsLivePlayback int    `json:"is_live_playback"`
	IsLessonVideo  int    `json:"is_lesson_video"`
	Interactive    bool   `json:"interactive_video"`
	SeasonID       int64  `json:"season_id"`
}

type bilibiliSpaceSeasonListResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Items struct {
			Seasons []bilibiliSpaceSeason `json:"seasons_list"`
		} `json:"items_lists"`
	} `json:"data"`
}

type bilibiliSpaceSeason struct {
	Meta     bilibiliSpaceSeasonMeta     `json:"meta"`
	Archives []bilibiliSpaceArchiveVideo `json:"archives"`
}

type bilibiliSpaceSeasonMeta struct {
	MID      int64  `json:"mid"`
	SeasonID int64  `json:"season_id"`
	Title    string `json:"title"`
	Name     string `json:"name"`
	Total    int    `json:"total"`
}

type bilibiliSpaceSeasonArchiveResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Archives []bilibiliSpaceArchiveVideo `json:"archives"`
		Meta     bilibiliSpaceSeasonMeta     `json:"meta"`
		Page     struct {
			Total    int `json:"total"`
			PageNum  int `json:"page_num"`
			PageSize int `json:"page_size"`
		} `json:"page"`
	} `json:"data"`
}

type bilibiliDynamicFeedResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Items   []json.RawMessage `json:"items"`
		HasMore flexibleBool      `json:"has_more"`
		Offset  string            `json:"offset"`
	} `json:"data"`
}

type bilibiliPostResponse struct {
	AwemeList []json.RawMessage `json:"aweme_list"`
	HasMore   flexibleBool      `json:"has_more"`
	MaxCursor int64             `json:"max_cursor"`
	StatusMsg string            `json:"status_msg"`
	Status    int               `json:"status_code"`
}

type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	switch strings.TrimSpace(string(data)) {
	case "true", "1":
		*b = true
		return nil
	case "false", "0", "", "null":
		*b = false
		return nil
	default:
		return fmt.Errorf("invalid bool value %s", string(data))
	}
}

type bilibiliAweme struct {
	AwemeID    string            `json:"aweme_id"`
	Desc       string            `json:"desc"`
	CreateTime int64             `json:"create_time"`
	Author     bilibiliAuthor    `json:"author"`
	Music      bilibiliMusic     `json:"music"`
	Video      bilibiliVideo     `json:"video"`
	Images     []bilibiliImage   `json:"images"`
	ImagePost  bilibiliImagePost `json:"image_post_info"`
	raw        json.RawMessage
}

type bilibiliAuthor struct {
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
}

type bilibiliMusic struct {
	Title string `json:"title"`
}

type bilibiliVideo struct {
	PlayAddr     bilibiliURLList `json:"play_addr"`
	DownloadAddr bilibiliURLList `json:"download_addr"`
	BitRate      []struct {
		PlayAddr bilibiliURLList `json:"play_addr"`
	} `json:"bit_rate"`
}

type bilibiliImagePost struct {
	Images []bilibiliImage `json:"images"`
}

type bilibiliImage struct {
	URLList      []string        `json:"url_list"`
	DisplayImage bilibiliURLList `json:"display_image"`
	OriginImage  bilibiliURLList `json:"origin_image"`
	DownloadURL  bilibiliURLList `json:"download_url"`
}

type bilibiliURLList struct {
	URLList []string `json:"url_list"`
	URI     string   `json:"uri"`
	URL     string   `json:"url"`
}

type ytdlpPlaylist struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Uploader    string       `json:"uploader"`
	UploaderID  string       `json:"uploader_id"`
	Channel     string       `json:"channel"`
	ChannelID   string       `json:"channel_id"`
	WebpageURL  string       `json:"webpage_url"`
	Description string       `json:"description"`
	Entries     []ytdlpEntry `json:"entries"`
}

type ytdlpEntry struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	WebpageURL  string  `json:"webpage_url"`
	URL         string  `json:"url"`
	Duration    float64 `json:"duration"`
	Timestamp   int64   `json:"timestamp"`
	UploadDate  string  `json:"upload_date"`
	Thumbnail   string  `json:"thumbnail"`
	Uploader    string  `json:"uploader"`
	UploaderID  string  `json:"uploader_id"`
}

type bilibiliVideoDetail struct {
	BVID        string
	CID         int64
	Title       string
	Description string
	PublishedAt string
	SourceURL   string
}

type localBilibiliDownloadMetadata struct {
	WorkType    string                           `json:"work_type"`
	BVID        string                           `json:"bvid,omitempty"`
	DynamicID   string                           `json:"dynamic_id,omitempty"`
	Title       string                           `json:"title"`
	Description string                           `json:"description,omitempty"`
	PublishedAt string                           `json:"published_at,omitempty"`
	SourceURL   string                           `json:"source_url,omitempty"`
	CoverFile   string                           `json:"cover_file,omitempty"`
	Creator     localBilibiliDownloadCreatorMeta `json:"creator"`
	Media       []localBilibiliDownloadMediaMeta `json:"media"`
}

type localBilibiliDownloadCreatorMeta struct {
	MID       string `json:"mid,omitempty"`
	Name      string `json:"name,omitempty"`
	SpaceURL  string `json:"space_url,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Bio       string `json:"bio,omitempty"`
}

type localBilibiliDownloadMediaMeta struct {
	Type string `json:"type"`
	File string `json:"file"`
}

type bilibiliOpus struct {
	ID          string
	Title       string
	Description string
	PublishedAt string
	SourceURL   string
	Creator     localBilibiliDownloadCreatorMeta
	Images      []string
	RawHTMLSize int
}

func (d BilibiliDownloader) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(d.BaseURL) != "" || d.HTTPClient != nil {
		return nil
	}
	path := strings.TrimSpace(os.Getenv("LOCALBILIBILI_YTDLP"))
	if path == "" {
		path = "yt-dlp"
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("yt-dlp 不可用，请安装 yt-dlp 或设置 LOCALBILIBILI_YTDLP: %s", message)
	}
	return nil
}

func (d BilibiliDownloader) ValidateCookie(ctx context.Context, cookie string) error {
	return ctx.Err()
}

func (d BilibiliDownloader) ResolveCreatorProfile(ctx context.Context, profileURL, cookie string) (CreatorProfile, error) {
	if isBilibiliOpusURL(profileURL) {
		opus, err := d.fetchOpus(ctx, profileURL, cookie)
		if err != nil {
			return CreatorProfile{}, err
		}
		return CreatorProfile{Name: opus.Creator.Name, UID: opus.Creator.MID, SecUID: opus.Creator.MID, ProfileURL: opus.Creator.SpaceURL, Bio: opus.Description}, nil
	}
	if !d.usesLegacyHTTPAPI() {
		if profile, err := d.fetchSpaceCardProfile(ctx, profileURL, cookie); err == nil && strings.TrimSpace(profile.Name) != "" {
			return profile, nil
		}
		return d.resolveCreatorProfileWithYTDLP(ctx, profileURL, cookie)
	}
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return CreatorProfile{}, ErrCookieRequired
	}
	secUID := extractBilibiliSecUID(profileURL)
	if secUID == "" {
		return CreatorProfile{}, errors.New("无法从B站主页解析 sec_uid")
	}
	user, err := d.fetchUser(ctx, secUID, cookie)
	if err != nil {
		return CreatorProfile{}, err
	}
	userSecUID := strings.TrimSpace(user.SecUID)
	if userSecUID == "" {
		userSecUID = secUID
	}
	return CreatorProfile{
		Name:       sanitizeCreatorDisplayName(user.Nickname),
		UID:        strings.TrimSpace(user.UID),
		SecUID:     userSecUID,
		ProfileURL: "https://www.bilibili.com/user/" + userSecUID,
		AvatarURL:  firstString(user.Avatar300.URLList, user.AvatarThumb.URLList),
		Bio:        strings.TrimSpace(user.Signature),
	}, nil
}

func (d BilibiliDownloader) UpdateCreator(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	if isBilibiliOpusURL(task.ProfileURL) {
		return d.updateOpusWithHTML(ctx, task)
	}
	if !d.usesLegacyHTTPAPI() {
		return d.updateCreatorWithYTDLP(ctx, task)
	}
	return d.updateCreatorLegacy(ctx, task)
}

func (d BilibiliDownloader) usesLegacyHTTPAPI() bool {
	return strings.TrimSpace(d.BaseURL) != "" || d.HTTPClient != nil
}

func (d BilibiliDownloader) updateCreatorLegacy(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	cookie := strings.TrimSpace(task.Cookie)
	if cookie == "" {
		return DownloadResult{}, errors.New("Cookie 不能为空，请先在更新管理中保存B站 Cookie")
	}
	secUID := extractBilibiliSecUID(task.ProfileURL)
	if secUID == "" {
		return DownloadResult{}, errors.New("无法从B站主页解析 sec_uid")
	}
	if strings.TrimSpace(task.OutputDir) == "" {
		return DownloadResult{}, errors.New("下载输出目录不能为空")
	}
	if err := ensureWritableDirectory(task.OutputDir); err != nil {
		return DownloadResult{}, err
	}

	user, err := d.fetchUser(ctx, secUID, cookie)
	if err != nil {
		return DownloadResult{}, err
	}
	creatorName := sanitizeCreatorDisplayName(user.Nickname)
	reportDownloadProgress(task.Progress, DownloadProgress{Message: "已获取UP主资料", Directory: task.OutputDir, Notice: user.Signature})

	existing, err := existingAwemeIDs(task.OutputDir)
	if err != nil {
		return DownloadResult{}, err
	}
	awemes, err := d.fetchPosts(ctx, secUID, cookie)
	if err != nil {
		return DownloadResult{CreatorName: creatorName}, err
	}
	reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(awemes), Message: "已获取作品列表", Directory: task.OutputDir, Notice: user.Signature})
	added := 0
	processed := 0
	skipped := 0
	permanentSkipped := 0
	attemptedNewWorks := 0
	downloadedMedia := 0
	mediaFailures := make([]string, 0)
	for _, aweme := range awemes {
		if aweme.AwemeID == "" || existing[aweme.AwemeID] {
			processed++
			skipped++
			reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(awemes), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: "跳过已下载作品", Directory: task.OutputDir, Notice: downloadNotice(user.Signature, mediaFailures)})
			continue
		}
		attemptedNewWorks++
		if aweme.Author.SecUID == "" {
			aweme.Author.SecUID = user.SecUID
		}
		if aweme.Author.UID == "" {
			aweme.Author.UID = user.UID
		}
		if aweme.Author.Nickname == "" {
			aweme.Author.Nickname = user.Nickname
		}
		if aweme.Author.Signature == "" {
			aweme.Author.Signature = user.Signature
		}
		if len(aweme.Author.Avatar300.URLList) == 0 && len(user.Avatar300.URLList) > 0 {
			aweme.Author.Avatar300.URLList = user.Avatar300.URLList
		}
		if len(aweme.Author.AvatarThumb.URLList) == 0 && len(user.AvatarThumb.URLList) > 0 {
			aweme.Author.AvatarThumb.URLList = user.AvatarThumb.URLList
		}
		if aweme.Author.ShareInfo.ShareURL == "" && aweme.Author.SecUID != "" {
			aweme.Author.ShareInfo.ShareURL = "https://www.bilibili.com/user/" + aweme.Author.SecUID
		}
		mediaCount, err := d.writeAweme(ctx, task.OutputDir, aweme, cookie, func(speed int64) {
			reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(awemes), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, SpeedBps: speed, Message: "正在下载作品", Directory: task.OutputDir, Notice: downloadNotice(user.Signature, mediaFailures)})
		}, func(message string) {
			reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(awemes), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: message, Directory: task.OutputDir, Notice: downloadNotice(user.Signature, mediaFailures)})
		})
		var permanent permanentMediaError
		if errors.As(err, &permanent) {
			processed++
			skipped++
			permanentSkipped++
			mediaFailures = appendLimitedFailure(mediaFailures, fmt.Sprintf("%s（HTTP %d）", awemeTitle(aweme), permanent.Status))
			reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(awemes), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: "跳过不可下载作品", Directory: task.OutputDir, Notice: downloadNotice(user.Signature, mediaFailures)})
			continue
		}
		if err != nil {
			return DownloadResult{AddedWorks: added, CreatorName: creatorName}, err
		}
		existing[aweme.AwemeID] = true
		added++
		processed++
		downloadedMedia += mediaCount
		reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(awemes), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: "作品下载完成", Directory: task.OutputDir, Notice: downloadNotice(user.Signature, mediaFailures)})
	}
	if attemptedNewWorks > 0 && permanentSkipped == attemptedNewWorks && added == 0 {
		return DownloadResult{AddedWorks: added, CreatorName: creatorName}, errors.New(downloadNotice("全部新增作品不可下载", mediaFailures))
	}
	return DownloadResult{AddedWorks: added, CreatorName: creatorName}, nil
}

func (d BilibiliDownloader) resolveCreatorProfileWithYTDLP(ctx context.Context, profileURL, cookie string) (CreatorProfile, error) {
	if strings.TrimSpace(profileURL) == "" {
		return CreatorProfile{}, errors.New("缺少B站主页链接")
	}
	playlist, err := d.fetchYTDLPPlaylist(ctx, profileURL, cookie, 1)
	if err != nil {
		return CreatorProfile{}, err
	}
	mid := firstNonEmpty(playlist.UploaderID, playlist.ChannelID, playlist.ID, extractBilibiliMID(profileURL))
	name := preferredCreatorName(firstNonEmpty(playlist.Uploader, playlist.Channel, playlist.Title, mid))
	if name == "" {
		return CreatorProfile{}, errors.New("yt-dlp 未返回 UP主昵称")
	}
	if mid == "" {
		mid = extractBilibiliMID(profileURL)
	}
	spaceURL := normalizeBilibiliSpaceURL(firstNonEmpty(playlist.WebpageURL, profileURL), mid)
	if profile, err := d.fetchSpaceCardProfile(ctx, spaceURL, cookie); err == nil && strings.TrimSpace(profile.Name) != "" && !isNumericCreatorName(profile.Name, mid) {
		return profile, nil
	}
	return CreatorProfile{
		Name:       name,
		UID:        mid,
		SecUID:     mid,
		ProfileURL: spaceURL,
		Bio:        strings.TrimSpace(playlist.Description),
	}, nil
}

func (d BilibiliDownloader) fetchSpaceCardProfile(ctx context.Context, profileURL, cookie string) (CreatorProfile, error) {
	if strings.TrimSpace(cookie) == "" {
		return CreatorProfile{}, ErrCookieRequired
	}
	mid := extractBilibiliMID(profileURL)
	if mid == "" {
		return CreatorProfile{}, errors.New("无法从B站主页解析 mid")
	}
	endpoint := "https://api.bilibili.com/x/web-interface/card"
	if strings.TrimSpace(d.BaseURL) != "" {
		endpoint = strings.TrimRight(d.BaseURL, "/") + "/x/web-interface/card"
	}
	values := url.Values{}
	values.Set("mid", mid)
	req, err := d.newRequest(ctx, endpoint+"?"+values.Encode(), cookie)
	if err != nil {
		return CreatorProfile{}, err
	}
	req.Header.Set("Referer", "https://space.bilibili.com/"+mid)
	var body bilibiliCardResponse
	if err := d.doJSON(req, &body); err != nil {
		return CreatorProfile{}, err
	}
	if body.Code != 0 {
		message := strings.TrimSpace(body.Message)
		if message == "" {
			message = "B站资料接口返回失败"
		}
		return CreatorProfile{}, errors.New(message)
	}
	card := body.Data.Card
	name := preferredCreatorName(card.Name)
	if name == "" {
		return CreatorProfile{}, errors.New("B站未返回UP主昵称")
	}
	cardMID := strings.TrimSpace(card.MID)
	if cardMID == "" {
		cardMID = mid
	}
	return CreatorProfile{
		Name:       name,
		UID:        cardMID,
		SecUID:     cardMID,
		ProfileURL: "https://space.bilibili.com/" + cardMID,
		AvatarURL:  strings.TrimSpace(card.Face),
		Bio:        strings.TrimSpace(card.Sign),
	}, nil
}

func (d BilibiliDownloader) fetchBilibiliVideoDetail(ctx context.Context, bvid, cookie string) (bilibiliVideoDetail, error) {
	bvid = strings.TrimSpace(bvid)
	if bvid == "" {
		return bilibiliVideoDetail{}, errors.New("缺少 bvid")
	}
	endpoint := "https://api.bilibili.com/x/web-interface/view"
	if strings.TrimSpace(d.VideoAPIBaseURL) != "" {
		endpoint = strings.TrimRight(d.VideoAPIBaseURL, "/") + "/x/web-interface/view"
	} else if strings.TrimSpace(d.BaseURL) != "" {
		endpoint = strings.TrimRight(d.BaseURL, "/") + "/x/web-interface/view"
	}
	values := url.Values{}
	values.Set("bvid", bvid)
	sourceURL := "https://www.bilibili.com/video/" + bvid
	req, err := d.newRequest(ctx, endpoint+"?"+values.Encode(), cookie)
	if err != nil {
		return bilibiliVideoDetail{}, err
	}
	req.Header.Set("Referer", sourceURL)
	var body bilibiliVideoViewResponse
	if err := d.doJSON(req, &body); err != nil {
		return bilibiliVideoDetail{}, err
	}
	if body.Code != 0 {
		message := strings.TrimSpace(body.Message)
		if message == "" {
			message = "B站视频详情接口返回失败"
		}
		return bilibiliVideoDetail{}, errors.New(message)
	}
	detail := bilibiliVideoDetail{
		BVID:        firstNonEmpty(body.Data.BVID, bvid),
		CID:         body.Data.CID,
		Title:       strings.TrimSpace(body.Data.Title),
		Description: strings.TrimSpace(body.Data.Desc),
		SourceURL:   sourceURL,
	}
	if body.Data.Pubdate > 0 {
		detail.PublishedAt = time.Unix(body.Data.Pubdate, 0).UTC().Format(time.RFC3339)
	}
	return detail, nil
}

func (d BilibiliDownloader) fetchBilibiliProgressiveURL(ctx context.Context, detail bilibiliVideoDetail, cookie string) (string, error) {
	bvid := strings.TrimSpace(detail.BVID)
	if bvid == "" || detail.CID <= 0 {
		return "", errors.New("缺少 B站视频 bvid 或 cid")
	}
	endpoint := "https://api.bilibili.com/x/player/playurl"
	if strings.TrimSpace(d.VideoAPIBaseURL) != "" {
		endpoint = strings.TrimRight(d.VideoAPIBaseURL, "/") + "/x/player/playurl"
	} else if strings.TrimSpace(d.BaseURL) != "" {
		endpoint = strings.TrimRight(d.BaseURL, "/") + "/x/player/playurl"
	}
	values := url.Values{}
	values.Set("bvid", bvid)
	values.Set("cid", strconv.FormatInt(detail.CID, 10))
	values.Set("qn", "80")
	values.Set("fnval", "0")
	values.Set("fourk", "1")
	sourceURL := firstNonEmpty(detail.SourceURL, "https://www.bilibili.com/video/"+bvid)
	req, err := d.newRequest(ctx, endpoint+"?"+values.Encode(), cookie)
	if err != nil {
		return "", err
	}
	req.Header.Set("Referer", sourceURL)
	var body bilibiliVideoPlayURLResponse
	if err := d.doJSON(req, &body); err != nil {
		return "", err
	}
	if body.Code != 0 {
		message := strings.TrimSpace(body.Message)
		if message == "" {
			message = "B站视频播放地址接口返回失败"
		}
		return "", errors.New(message)
	}
	for _, item := range body.Data.DURL {
		if strings.TrimSpace(item.URL) != "" {
			return strings.TrimSpace(item.URL), nil
		}
		for _, backup := range item.Backup {
			if strings.TrimSpace(backup) != "" {
				return strings.TrimSpace(backup), nil
			}
		}
	}
	return "", errors.New("B站视频播放地址接口未返回 MP4 地址")
}

func isNumericCreatorName(name, mid string) bool {
	name = strings.TrimSpace(name)
	mid = strings.TrimSpace(mid)
	return name != "" && mid != "" && name == mid
}

func (d BilibiliDownloader) updateCreatorWithYTDLP(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	profileURL := strings.TrimSpace(task.ProfileURL)
	if profileURL == "" {
		return DownloadResult{}, errors.New("缺少B站主页链接")
	}
	if strings.TrimSpace(task.OutputDir) == "" {
		return DownloadResult{}, errors.New("下载输出目录不能为空")
	}
	if err := ensureWritableDirectory(task.OutputDir); err != nil {
		return DownloadResult{}, err
	}
	if err := d.Ready(ctx); err != nil {
		return DownloadResult{}, err
	}
	maxWorks := ytdlpMaxWorks()
	playlist, err := d.fetchYTDLPPlaylist(ctx, profileURL, task.Cookie, maxWorks)
	if err != nil {
		fallback, fallbackErr := d.fetchBilibiliSpaceArchivePlaylist(ctx, profileURL, task.Cookie, maxWorks)
		if fallbackErr != nil {
			seasonFallback, seasonErr := d.fetchBilibiliSpaceSeasonPlaylist(ctx, profileURL, task.Cookie, maxWorks)
			if seasonErr != nil {
				dynamicFallback, dynamicErr := d.fetchBilibiliDynamicFeedPlaylist(ctx, profileURL, task.Cookie, maxWorks)
				if dynamicErr != nil {
					combinedErr := fmt.Errorf("yt-dlp列表获取失败: %s；B站空间分页列表失败: %s；B站合集分页列表失败: %s；B站动态流列表失败: %s", sanitizeYTDLPError(err.Error()), sanitizeYTDLPError(fallbackErr.Error()), sanitizeYTDLPError(seasonErr.Error()), sanitizeYTDLPError(dynamicErr.Error()))
					return d.repairExistingMetadataAfterPlaylistFailure(ctx, task, combinedErr)
				}
				playlist = dynamicFallback
				reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), Message: "yt-dlp列表获取失败，已改用B站动态流列表", Directory: task.OutputDir, Notice: sanitizeYTDLPError(err.Error())})
			} else {
				playlist = seasonFallback
				reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), Message: "yt-dlp列表获取失败，已改用B站合集分页列表", Directory: task.OutputDir, Notice: sanitizeYTDLPError(err.Error())})
			}
		} else {
			playlist = fallback
			reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), Message: "yt-dlp列表获取失败，已改用B站空间分页列表", Directory: task.OutputDir, Notice: sanitizeYTDLPError(err.Error())})
		}
	}
	mid := firstNonEmpty(playlist.UploaderID, playlist.ChannelID, playlist.ID, extractBilibiliMID(profileURL))
	creatorName := preferredCreatorName(firstNonEmpty(playlist.Uploader, playlist.Channel, playlist.Title, task.Creator.Name, mid))
	spaceURL := normalizeBilibiliSpaceURL(firstNonEmpty(playlist.WebpageURL, profileURL), mid)
	if creatorName == "" {
		creatorName = "B站UP主"
	}
	creatorAvatarURL := strings.TrimSpace(task.Creator.AvatarURL)
	creatorBio := strings.TrimSpace(playlist.Description)
	if profile, err := d.fetchSpaceCardProfile(ctx, spaceURL, task.Cookie); err == nil && strings.TrimSpace(profile.Name) != "" {
		creatorName = profile.Name
		mid = firstNonEmpty(profile.UID, profile.SecUID, mid)
		spaceURL = firstNonEmpty(profile.ProfileURL, spaceURL)
		creatorAvatarURL = strings.TrimSpace(profile.AvatarURL)
		creatorBio = strings.TrimSpace(profile.Bio)
	}
	reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), Message: "已获取B站作品列表", Directory: task.OutputDir, Notice: creatorBio})

	existing, err := existingLocalBilibiliMetadataPaths(task.OutputDir)
	if err != nil {
		return DownloadResult{}, err
	}
	existingFlat, err := existingAwemeIDs(task.OutputDir)
	if err != nil {
		return DownloadResult{}, err
	}
	added := 0
	processed := 0
	skipped := 0
	downloadedMedia := 0
	for _, entry := range playlist.Entries {
		workID := ytdlpEntryID(entry)
		if workID == "" {
			processed++
			skipped++
			reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: "跳过缺少作品 ID 的条目", Directory: task.OutputDir})
			continue
		}
		if metadataPath := existing[workID]; metadataPath != "" {
			if repaired, err := d.repairLocalBilibiliMetadata(ctx, metadataPath, task.Cookie); err == nil && repaired {
				reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: "已修复作品标题元数据", Directory: task.OutputDir})
			} else if err != nil {
				reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: "作品标题元数据修复失败，保留现有信息", Directory: task.OutputDir, Notice: sanitizeYTDLPError(err.Error())})
			}
			processed++
			skipped++
			reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: "跳过已下载作品", Directory: task.OutputDir})
			continue
		}
		if existingFlat[workID] {
			processed++
			skipped++
			reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: "跳过已下载作品", Directory: task.OutputDir})
			continue
		}
		mediaCount, err := d.downloadYTDLPEntry(ctx, task.OutputDir, entry, localBilibiliDownloadCreatorMeta{MID: mid, Name: creatorName, SpaceURL: spaceURL, AvatarURL: creatorAvatarURL, Bio: creatorBio}, task.Cookie)
		if err != nil {
			if isPermanentYTDLPError(err) {
				processed++
				skipped++
				reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: "跳过不可下载作品", Directory: task.OutputDir, Notice: err.Error()})
				continue
			}
			return DownloadResult{AddedWorks: added, CreatorName: creatorName}, err
		}
		existing[workID] = "downloaded"
		existingFlat[workID] = true
		added++
		processed++
		downloadedMedia += mediaCount
		reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(playlist.Entries), ProcessedWorks: processed, DownloadedMedia: downloadedMedia, SkippedWorks: skipped, Message: "作品下载完成", Directory: task.OutputDir})
	}
	return DownloadResult{AddedWorks: added, CreatorName: creatorName}, nil
}

func (d BilibiliDownloader) repairExistingMetadataAfterPlaylistFailure(ctx context.Context, task DownloadTask, playlistErr error) (DownloadResult, error) {
	existing, err := existingLocalBilibiliMetadataPaths(task.OutputDir)
	if err != nil || len(existing) == 0 {
		return DownloadResult{}, playlistErr
	}
	reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(existing), Message: "B站列表获取失败，尝试修复本地作品元数据", Directory: task.OutputDir, Notice: sanitizeYTDLPError(playlistErr.Error())})
	processed := 0
	skipped := 0
	repaired := 0
	failed := 0
	for _, metadataPath := range existing {
		changed, repairErr := d.repairLocalBilibiliMetadata(ctx, metadataPath, task.Cookie)
		processed++
		if repairErr != nil {
			failed++
			reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(existing), ProcessedWorks: processed, SkippedWorks: skipped, Message: "作品标题元数据修复失败，保留现有信息", Directory: task.OutputDir, Notice: sanitizeYTDLPError(repairErr.Error())})
			continue
		}
		if changed {
			repaired++
			reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(existing), ProcessedWorks: processed, SkippedWorks: skipped, Message: "已修复作品标题元数据", Directory: task.OutputDir})
			continue
		}
		skipped++
		reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(existing), ProcessedWorks: processed, SkippedWorks: skipped, Message: "本地作品元数据无需修复", Directory: task.OutputDir})
	}
	notice := ""
	if failed > 0 {
		notice = fmt.Sprintf("列表获取失败后已尝试修复本地元数据：成功 %d，失败 %d", repaired, failed)
	}
	reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: len(existing), ProcessedWorks: processed, SkippedWorks: skipped, Message: "本地作品元数据修复完成", Directory: task.OutputDir, Notice: notice})
	if repaired == 0 {
		return DownloadResult{}, playlistErr
	}
	return DownloadResult{AddedWorks: 0, CreatorName: task.Creator.Name}, nil
}

func (d BilibiliDownloader) updateOpusWithHTML(ctx context.Context, task DownloadTask) (DownloadResult, error) {
	if strings.TrimSpace(task.OutputDir) == "" {
		return DownloadResult{}, errors.New("下载输出目录不能为空")
	}
	if err := ensureWritableDirectory(task.OutputDir); err != nil {
		return DownloadResult{}, err
	}
	opus, err := d.fetchOpus(ctx, task.ProfileURL, task.Cookie)
	if err != nil {
		return DownloadResult{}, err
	}
	existing, err := existingLocalBilibiliWorkIDs(task.OutputDir)
	if err != nil {
		return DownloadResult{}, err
	}
	if existing[opus.ID] {
		reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: 1, ProcessedWorks: 1, SkippedWorks: 1, Message: "跳过已下载图文", Directory: task.OutputDir})
		return DownloadResult{CreatorName: opus.Creator.Name}, nil
	}
	reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: 1, Message: "已获取B站图文", Directory: task.OutputDir, Notice: opus.Description})
	if err := d.writeOpus(ctx, task.OutputDir, opus, task.Cookie); err != nil {
		return DownloadResult{}, err
	}
	reportDownloadProgress(task.Progress, DownloadProgress{TotalWorks: 1, ProcessedWorks: 1, DownloadedMedia: len(opus.Images), Message: "图文下载完成", Directory: task.OutputDir})
	return DownloadResult{AddedWorks: 1, CreatorName: opus.Creator.Name}, nil
}

func (d BilibiliDownloader) fetchOpus(ctx context.Context, opusURL, cookie string) (bilibiliOpus, error) {
	id := extractBilibiliOpusID(opusURL)
	if id == "" {
		return bilibiliOpus{}, errors.New("缺少B站图文 opus id")
	}
	req, err := d.newRequest(ctx, opusURL, cookie)
	if err != nil {
		return bilibiliOpus{}, err
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return bilibiliOpus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return bilibiliOpus{}, fmt.Errorf("B站图文页面请求失败: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return bilibiliOpus{}, err
	}
	page := string(data)
	title := parseOpusTitle(page)
	if title == "" {
		title = id
	}
	description := parseOpusDescription(page)
	mid := parseOpusMID(page)
	name := preferredCreatorName(parseOpusAuthorName(page))
	if name == "" {
		name = preferredCreatorName(taskSafeCreatorNameFromURL(opusURL, mid))
	}
	images := parseOpusImageURLs(page)
	if len(images) == 0 {
		return bilibiliOpus{}, errors.New("B站图文页面没有可保存图片")
	}
	return bilibiliOpus{
		ID:          id,
		Title:       title,
		Description: description,
		PublishedAt: parseOpusPublishedAt(page),
		SourceURL:   opusURL,
		Creator: localBilibiliDownloadCreatorMeta{
			MID:      mid,
			Name:     name,
			SpaceURL: normalizeBilibiliSpaceURL("", mid),
		},
		Images:      images,
		RawHTMLSize: len(data),
	}, nil
}

func (d BilibiliDownloader) writeOpus(ctx context.Context, outputDir string, opus bilibiliOpus, cookie string) error {
	date := "1970-01-01"
	if opus.PublishedAt != "" {
		if t, err := time.Parse(time.RFC3339, opus.PublishedAt); err == nil {
			date = t.Format("2006-01-02")
		}
	}
	title := sanitizeFlatFilename(opus.Title, maxTitleBytesForAweme(opus.ID))
	if title == "" {
		title = opus.ID
	}
	workDir := filepath.Join(outputDir, fmt.Sprintf("%s_%s_%s", date, title, opus.ID))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	for i, imageURL := range opus.Images {
		ext := imageExt(imageURL)
		target := filepath.Join(workDir, fmt.Sprintf("image_%03d%s", i+1, ext))
		if err := d.downloadFile(ctx, imageURL, target, cookie, nil, nil); err != nil {
			_ = os.RemoveAll(workDir)
			return err
		}
	}
	firstImage := filepath.Join(workDir, "image_001"+imageExt(opus.Images[0]))
	cover := "cover" + strings.ToLower(filepath.Ext(firstImage))
	if err := copyFile(firstImage, filepath.Join(workDir, cover)); err != nil {
		return err
	}
	source := map[string]any{
		"source_url":    opus.SourceURL,
		"source_type":   "opus_html",
		"raw_html_size": opus.RawHTMLSize,
		"note":          "Public image-text metadata extracted from opus HTML. No cookies or private response data are stored.",
	}
	sourceData, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, "source.bilibili.json"), sourceData, 0o644); err != nil {
		return err
	}
	media := make([]localBilibiliDownloadMediaMeta, 0, len(opus.Images))
	for i, imageURL := range opus.Images {
		media = append(media, localBilibiliDownloadMediaMeta{Type: "image", File: fmt.Sprintf("image_%03d%s", i+1, imageExt(imageURL))})
	}
	meta := localBilibiliDownloadMetadata{
		WorkType:    "image_text",
		DynamicID:   opus.ID,
		Title:       opus.Title,
		Description: opus.Description,
		PublishedAt: opus.PublishedAt,
		SourceURL:   opus.SourceURL,
		CoverFile:   cover,
		Creator:     opus.Creator,
		Media:       media,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workDir, "metadata.localbilibili.json"), data, 0o644)
}

func (d BilibiliDownloader) fetchYTDLPPlaylist(ctx context.Context, profileURL, cookie string, limit int) (ytdlpPlaylist, error) {
	args := []string{"--dump-single-json", "--flat-playlist", "--no-warnings"}
	if limit > 0 {
		args = append(args, "--playlist-end", fmt.Sprintf("%d", limit))
	}
	args = appendYTDLPCookieHeader(args, cookie)
	args = append(args, profileURL)
	output, err := d.runYTDLP(ctx, args...)
	if err != nil {
		return ytdlpPlaylist{}, err
	}
	var playlist ytdlpPlaylist
	if err := unmarshalYTDLPJSON(output, &playlist); err != nil {
		return ytdlpPlaylist{}, fmt.Errorf("解析 yt-dlp 作品列表失败: %w", err)
	}
	if len(playlist.Entries) == 0 {
		var entry ytdlpEntry
		if err := unmarshalYTDLPJSON(output, &entry); err == nil && ytdlpEntryID(entry) != "" {
			playlist.Entries = []ytdlpEntry{entry}
		}
	}
	if len(playlist.Entries) == 0 {
		return ytdlpPlaylist{}, errors.New("yt-dlp 未返回可下载作品，请检查 B站主页链接或 Cookie")
	}
	return playlist, nil
}

func (d BilibiliDownloader) fetchBilibiliSpaceArchivePlaylist(ctx context.Context, profileURL, cookie string, limit int) (ytdlpPlaylist, error) {
	mid := extractBilibiliMID(profileURL)
	if mid == "" {
		return ytdlpPlaylist{}, errors.New("无法从B站主页解析 mid")
	}
	endpoint := d.bilibiliSpaceEndpoint("/x/space/arc/search")
	const pageSize = 50
	playlist := ytdlpPlaylist{
		ID:         mid,
		UploaderID: mid,
		ChannelID:  mid,
		WebpageURL: normalizeBilibiliSpaceURL(profileURL, mid),
	}
	seen := map[string]bool{}
	for page := 1; page <= 200; page++ {
		values := url.Values{}
		values.Set("mid", mid)
		values.Set("pn", strconv.Itoa(page))
		values.Set("ps", strconv.Itoa(pageSize))
		values.Set("order", "pubdate")
		req, err := d.newRequest(ctx, endpoint+"?"+values.Encode(), cookie)
		if err != nil {
			return ytdlpPlaylist{}, err
		}
		req.Header.Set("Referer", "https://space.bilibili.com/"+mid+"/video")
		var body bilibiliSpaceArchiveResponse
		if err := d.doJSON(req, &body); err != nil {
			return ytdlpPlaylist{}, err
		}
		if body.Code != 0 {
			message := strings.TrimSpace(body.Message)
			if message == "" {
				message = "B站空间作品列表接口返回失败"
			}
			return ytdlpPlaylist{}, errors.New(message)
		}
		videos := body.Data.List.Videos
		if len(videos) == 0 {
			break
		}
		for _, video := range videos {
			entry := ytdlpEntryFromSpaceArchive(video)
			id := ytdlpEntryID(entry)
			if id == "" || seen[id] || !isSupportedSpaceArchiveVideo(video) {
				continue
			}
			seen[id] = true
			if playlist.Uploader == "" {
				playlist.Uploader = strings.TrimSpace(video.Author)
			}
			playlist.Entries = append(playlist.Entries, entry)
			if limit > 0 && len(playlist.Entries) >= limit {
				return playlist, nil
			}
		}
		if body.Data.Page.Count > 0 && page*pageSize >= body.Data.Page.Count {
			break
		}
	}
	if len(playlist.Entries) == 0 {
		return ytdlpPlaylist{}, errors.New("B站空间作品列表未返回可下载视频")
	}
	if playlist.Title == "" {
		playlist.Title = firstNonEmpty(playlist.Uploader, mid)
	}
	return playlist, nil
}

func (d BilibiliDownloader) fetchBilibiliSpaceSeasonPlaylist(ctx context.Context, profileURL, cookie string, limit int) (ytdlpPlaylist, error) {
	mid := extractBilibiliMID(profileURL)
	if mid == "" {
		return ytdlpPlaylist{}, errors.New("无法从B站主页解析 mid")
	}
	values := url.Values{}
	values.Set("mid", mid)
	values.Set("page_num", "1")
	values.Set("page_size", "20")
	listURL := d.bilibiliSpaceEndpoint("/x/polymer/web-space/seasons_series_list") + "?" + values.Encode()
	req, err := d.newRequest(ctx, listURL, cookie)
	if err != nil {
		return ytdlpPlaylist{}, err
	}
	req.Header.Set("Referer", "https://space.bilibili.com/"+mid+"/video")
	var seasonsBody bilibiliSpaceSeasonListResponse
	if err := d.doJSON(req, &seasonsBody); err != nil {
		return ytdlpPlaylist{}, err
	}
	if seasonsBody.Code != 0 {
		message := strings.TrimSpace(seasonsBody.Message)
		if message == "" {
			message = "B站合集列表接口返回失败"
		}
		return ytdlpPlaylist{}, errors.New(message)
	}
	seasons := seasonsBody.Data.Items.Seasons
	if len(seasons) == 0 {
		return ytdlpPlaylist{}, errors.New("B站合集列表未返回合集")
	}
	const pageSize = 30
	playlist := ytdlpPlaylist{
		ID:         mid,
		UploaderID: mid,
		ChannelID:  mid,
		WebpageURL: normalizeBilibiliSpaceURL(profileURL, mid),
	}
	seen := map[string]bool{}
	for _, season := range seasons {
		seasonID := season.Meta.SeasonID
		if seasonID <= 0 {
			continue
		}
		total := season.Meta.Total
		for page := 1; page <= 200; page++ {
			values := url.Values{}
			values.Set("mid", mid)
			values.Set("season_id", strconv.FormatInt(seasonID, 10))
			values.Set("page_num", strconv.Itoa(page))
			values.Set("page_size", strconv.Itoa(pageSize))
			req, err := d.newRequest(ctx, d.bilibiliSpaceEndpoint("/x/polymer/web-space/seasons_archives_list")+"?"+values.Encode(), cookie)
			if err != nil {
				return ytdlpPlaylist{}, err
			}
			req.Header.Set("Referer", "https://space.bilibili.com/"+mid+"/channel/collectiondetail?sid="+strconv.FormatInt(seasonID, 10))
			var body bilibiliSpaceSeasonArchiveResponse
			if err := d.doJSON(req, &body); err != nil {
				return ytdlpPlaylist{}, err
			}
			if body.Code != 0 {
				message := strings.TrimSpace(body.Message)
				if message == "" {
					message = "B站合集作品列表接口返回失败"
				}
				return ytdlpPlaylist{}, errors.New(message)
			}
			videos := body.Data.Archives
			if len(videos) == 0 {
				break
			}
			if body.Data.Page.Total > 0 {
				total = body.Data.Page.Total
			}
			for _, video := range videos {
				if video.MID == 0 {
					video.MID = parseInt64(mid)
				}
				if video.SeasonID == 0 {
					video.SeasonID = seasonID
				}
				entry := ytdlpEntryFromSpaceArchive(video)
				id := ytdlpEntryID(entry)
				if id == "" || seen[id] || !isSupportedSpaceArchiveVideo(video) {
					continue
				}
				seen[id] = true
				if playlist.Uploader == "" {
					playlist.Uploader = strings.TrimSpace(video.Author)
				}
				playlist.Entries = append(playlist.Entries, entry)
				if limit > 0 && len(playlist.Entries) >= limit {
					playlist.Title = firstNonEmpty(playlist.Title, playlist.Uploader, mid)
					return playlist, nil
				}
			}
			if total > 0 && page*pageSize >= total {
				break
			}
			if len(videos) < pageSize {
				break
			}
		}
	}
	if len(playlist.Entries) == 0 {
		return ytdlpPlaylist{}, errors.New("B站合集作品列表未返回可下载视频")
	}
	if playlist.Title == "" {
		playlist.Title = firstNonEmpty(playlist.Uploader, mid)
	}
	return playlist, nil
}

func (d BilibiliDownloader) fetchBilibiliDynamicFeedPlaylist(ctx context.Context, profileURL, cookie string, limit int) (ytdlpPlaylist, error) {
	mid := extractBilibiliMID(profileURL)
	if mid == "" {
		return ytdlpPlaylist{}, errors.New("无法从B站主页解析 mid")
	}
	playlist := ytdlpPlaylist{
		ID:         mid,
		UploaderID: mid,
		ChannelID:  mid,
		WebpageURL: normalizeBilibiliSpaceURL(profileURL, mid),
	}
	seen := map[string]bool{}
	offset := ""
	for page := 1; page <= 200; page++ {
		values := url.Values{}
		values.Set("host_mid", mid)
		values.Set("timezone_offset", "-480")
		values.Set("features", "itemOpusStyle,listOnlyfans,opusBigCover,onlyfansVote,decorationCard,forwardListHidden,ugcDelete,onlyfansQaCard")
		if offset != "" {
			values.Set("offset", offset)
		}
		req, err := d.newRequest(ctx, d.bilibiliSpaceEndpoint("/x/polymer/web-dynamic/desktop/v1/feed/space")+"?"+values.Encode(), cookie)
		if err != nil {
			return ytdlpPlaylist{}, err
		}
		req.Header.Set("Referer", "https://space.bilibili.com/"+mid+"/dynamic")
		var body bilibiliDynamicFeedResponse
		if err := d.doJSON(req, &body); err != nil {
			return ytdlpPlaylist{}, err
		}
		if body.Code != 0 {
			message := strings.TrimSpace(body.Message)
			if message == "" {
				message = "B站动态流接口返回失败"
			}
			return ytdlpPlaylist{}, errors.New(message)
		}
		if len(body.Data.Items) == 0 {
			break
		}
		for _, item := range body.Data.Items {
			for _, bvid := range extractBVIDsFromJSON(item) {
				if seen[bvid] {
					continue
				}
				seen[bvid] = true
				playlist.Entries = append(playlist.Entries, ytdlpEntry{
					ID:         bvid,
					Title:      bvid,
					WebpageURL: "https://www.bilibili.com/video/" + bvid,
					URL:        "https://www.bilibili.com/video/" + bvid,
					UploaderID: mid,
				})
				if limit > 0 && len(playlist.Entries) >= limit {
					return playlist, nil
				}
			}
		}
		nextOffset := strings.TrimSpace(body.Data.Offset)
		if !bool(body.Data.HasMore) || nextOffset == "" || nextOffset == offset {
			break
		}
		offset = nextOffset
	}
	if len(playlist.Entries) == 0 {
		return ytdlpPlaylist{}, errors.New("B站动态流列表未返回可下载视频")
	}
	return playlist, nil
}

func (d BilibiliDownloader) bilibiliSpaceEndpoint(path string) string {
	base := "https://api.bilibili.com"
	if strings.TrimSpace(d.SpaceAPIBaseURL) != "" {
		base = strings.TrimRight(d.SpaceAPIBaseURL, "/")
	} else if strings.TrimSpace(d.VideoAPIBaseURL) != "" {
		base = strings.TrimRight(d.VideoAPIBaseURL, "/")
	}
	return base + path
}

func ytdlpEntryFromSpaceArchive(video bilibiliSpaceArchiveVideo) ytdlpEntry {
	bvid := strings.TrimSpace(video.BVID)
	return ytdlpEntry{
		ID:          bvid,
		Title:       strings.TrimSpace(video.Title),
		Description: strings.TrimSpace(video.Description),
		WebpageURL:  "https://www.bilibili.com/video/" + bvid,
		URL:         "https://www.bilibili.com/video/" + bvid,
		Duration:    bilibiliArchiveDurationSeconds(video),
		Timestamp:   firstPositiveInt64(video.Created, video.Pubdate, video.CTime),
		Thumbnail:   normalizeBilibiliImageURL(video.Pic),
		Uploader:    strings.TrimSpace(video.Author),
		UploaderID:  strconv.FormatInt(video.MID, 10),
	}
}

func isSupportedSpaceArchiveVideo(video bilibiliSpaceArchiveVideo) bool {
	if strings.TrimSpace(video.BVID) == "" {
		return false
	}
	if video.IsPay != 0 || video.IsSteinsGate != 0 || video.IsLivePlayback != 0 || video.IsLessonVideo != 0 || video.Interactive {
		return false
	}
	return true
}

func bilibiliArchiveDurationSeconds(video bilibiliSpaceArchiveVideo) float64 {
	if strings.TrimSpace(video.Length) != "" {
		return parseBilibiliDurationSeconds(video.Length)
	}
	if video.Duration > 0 {
		return float64(video.Duration)
	}
	return 0
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func parseInt64(value string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return n
}

func parseBilibiliDurationSeconds(value string) float64 {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) == 0 {
		return 0
	}
	total := 0
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return float64(total)
}

func normalizeBilibiliImageURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	if strings.HasPrefix(value, "http://") {
		return "https://" + strings.TrimPrefix(value, "http://")
	}
	return value
}

var bilibiliBVIDInJSONPattern = regexp.MustCompile(`BV[0-9A-Za-z]{6,14}`)

func extractBVIDsFromJSON(data []byte) []string {
	matches := bilibiliBVIDInJSONPattern.FindAll(data, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	bvids := []string{}
	for _, match := range matches {
		bvid := string(match)
		if seen[bvid] {
			continue
		}
		seen[bvid] = true
		bvids = append(bvids, bvid)
	}
	return bvids
}

func unmarshalYTDLPJSON(output []byte, target any) error {
	if err := json.Unmarshal(output, target); err == nil {
		return nil
	}
	payload, err := extractFirstJSONObject(output)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}

func extractFirstJSONObject(output []byte) ([]byte, error) {
	start := -1
	depth := 0
	inString := false
	escaped := false
	for index, b := range output {
		if start < 0 {
			if b == '{' {
				start = index
				depth = 1
			}
			continue
		}
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if b == '\\' {
				escaped = true
				continue
			}
			if b == '"' {
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return output[start : index+1], nil
			}
		}
	}
	if start >= 0 {
		return nil, errors.New("yt-dlp 输出中的 JSON 不完整")
	}
	return nil, errors.New("yt-dlp 输出中未找到 JSON")
}

func (d BilibiliDownloader) downloadYTDLPEntry(ctx context.Context, outputDir string, entry ytdlpEntry, creator localBilibiliDownloadCreatorMeta, cookie string) (int, error) {
	workID := ytdlpEntryID(entry)
	if workID == "" {
		return 0, errors.New("作品缺少 bvid")
	}
	targetURL := firstNonEmpty(entry.WebpageURL, entry.URL)
	if targetURL == "" {
		targetURL = "https://www.bilibili.com/video/" + workID
	}
	detail, _ := d.fetchBilibiliVideoDetail(ctx, workID, cookie)
	title := sanitizeFlatFilename(preferredVideoTitle(workID, detail.Title, "", entry.Title), maxTitleBytesForAweme(workID))
	if title == "" {
		title = workID
	}
	date := ytdlpEntryDateWithDetail(entry, detail)
	prefix := fmt.Sprintf("%s_%s_%s", date, title, workID)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return 0, err
	}
	cleanupYTDLPTempFiles(outputDir)
	args := []string{
		"--no-playlist",
		"--no-warnings",
		"--write-info-json",
		"-f", ytdlpFormat(),
		"--merge-output-format", "mp4",
		"-o", filepath.Join(outputDir, "media_001.%(ext)s"),
	}
	if section := strings.TrimSpace(os.Getenv("LOCALBILIBILI_YTDLP_DOWNLOAD_SECTIONS")); section != "" {
		args = append(args, "--download-sections", section)
	}
	args = appendYTDLPCookieHeader(args, cookie)
	args = append(args, targetURL)
	if _, err := d.runYTDLP(ctx, args...); err != nil {
		cleanupYTDLPTempFiles(outputDir)
		if fallbackErr := d.downloadBilibiliProgressiveEntry(ctx, outputDir, prefix, workID, detail, entry, creator, targetURL, cookie); fallbackErr == nil {
			return 1, nil
		}
		return 0, err
	}
	mediaFile, err := findDownloadedMedia(outputDir)
	if err != nil {
		cleanupYTDLPTempFiles(outputDir)
		return 0, err
	}
	var infoEntry ytdlpEntry
	if info := findFirstBySuffix(outputDir, ".info.json"); info != "" {
		infoEntry = readYTDLPInfoEntry(info)
		_ = os.Remove(info)
	}
	mediaExt := strings.ToLower(filepath.Ext(mediaFile))
	if mediaExt == "" {
		mediaExt = ".mp4"
	}
	targetMedia := filepath.Join(outputDir, prefix+mediaExt)
	if err := os.Rename(mediaFile, targetMedia); err != nil {
		cleanupYTDLPTempFiles(outputDir)
		return 0, err
	}
	if err := writeFlatYTDLPMetadata(outputDir, prefix, workID, detail, infoEntry, entry, creator, targetURL); err != nil {
		_ = os.Remove(targetMedia)
		return 0, err
	}
	cleanupYTDLPTempFiles(outputDir)
	return 1, nil
}

func (d BilibiliDownloader) downloadBilibiliProgressiveEntry(ctx context.Context, outputDir, prefix, workID string, detail bilibiliVideoDetail, entry ytdlpEntry, creator localBilibiliDownloadCreatorMeta, targetURL, cookie string) error {
	if strings.TrimSpace(detail.BVID) == "" || detail.CID <= 0 {
		nextDetail, err := d.fetchBilibiliVideoDetail(ctx, workID, cookie)
		if err != nil {
			return err
		}
		detail = nextDetail
	}
	mediaURL, err := d.fetchBilibiliProgressiveURL(ctx, detail, cookie)
	if err != nil {
		return err
	}
	targetMedia := filepath.Join(outputDir, prefix+".mp4")
	if err := d.downloadFileWithReferer(ctx, mediaURL, targetMedia, cookie, firstNonEmpty(detail.SourceURL, targetURL)); err != nil {
		_ = os.Remove(targetMedia)
		return err
	}
	if err := writeFlatYTDLPMetadata(outputDir, prefix, workID, detail, ytdlpEntry{}, entry, creator, targetURL); err != nil {
		_ = os.Remove(targetMedia)
		return err
	}
	cleanupYTDLPTempFiles(outputDir)
	return nil
}

func writeFlatYTDLPMetadata(outputDir, prefix, workID string, detail bilibiliVideoDetail, infoEntry, entry ytdlpEntry, creator localBilibiliDownloadCreatorMeta, targetURL string) error {
	publishedAt := firstNonEmpty(detail.PublishedAt, ytdlpEntryPublishedAt(infoEntry), ytdlpEntryPublishedAt(entry))
	createTime := int64(0)
	if publishedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, publishedAt); err == nil {
			createTime = parsed.Unix()
		}
	}
	sourceURL := firstNonEmpty(detail.SourceURL, infoEntry.WebpageURL, targetURL)
	title := preferredVideoTitle(workID, detail.Title, infoEntry.Title, entry.Title)
	data := map[string]any{
		"aweme_id":    workID,
		"title":       title,
		"desc":        firstNonEmpty(detail.Description, infoEntry.Description, entry.Description, title),
		"create_time": createTime,
		"share_url":   sourceURL,
		"author": map[string]any{
			"uid":       creator.MID,
			"sec_uid":   creator.MID,
			"nickname":  creator.Name,
			"signature": creator.Bio,
			"share_info": map[string]any{
				"share_url": creator.SpaceURL,
			},
			"avatar_300x300": map[string]any{
				"url_list": []string{creator.AvatarURL},
			},
			"avatar_thumb": map[string]any{
				"url_list": []string{creator.AvatarURL},
			},
		},
		"music": map[string]any{
			"title": "",
		},
		"source_type": "yt-dlp",
		"bvid":        workID,
		"work_type":   classifyYTDLPWorkType(entry),
	}
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, prefix+"_data.json"), payload, 0o644)
}

func cleanupYTDLPTempFiles(outputDir string) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "media_001.") {
			_ = os.Remove(filepath.Join(outputDir, name))
		}
	}
}

func (d BilibiliDownloader) runYTDLP(ctx context.Context, args ...string) ([]byte, error) {
	path := strings.TrimSpace(os.Getenv("LOCALBILIBILI_YTDLP"))
	if path == "" {
		path = "yt-dlp"
	}
	runCtx := ctx
	cancel := func() {}
	if timeout := ytdlpCommandTimeout(); timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(runCtx, path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("yt-dlp 执行失败: %s", sanitizeYTDLPError(err.Error()))
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			message := strings.TrimSpace(output.String())
			if message == "" {
				message = err.Error()
			}
			if isPermanentYTDLPMessage(message) {
				return nil, permanentYTDLPError{Message: message}
			}
			return nil, fmt.Errorf("yt-dlp 执行失败: %s", sanitizeYTDLPError(message))
		}
		return output.Bytes(), nil
	case <-runCtx.Done():
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Process.Kill()
		}
		<-done
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("yt-dlp 下载超时: 已超过 %s", ytdlpCommandTimeout())
	}
}

func reportDownloadProgress(fn func(DownloadProgress), progress DownloadProgress) {
	if fn != nil {
		fn(progress)
	}
}

func (d BilibiliDownloader) client() *http.Client {
	if d.HTTPClient != nil {
		return d.HTTPClient
	}
	return &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}}
}

func (d BilibiliDownloader) baseURL() string {
	if strings.TrimSpace(d.BaseURL) != "" {
		return strings.TrimRight(d.BaseURL, "/")
	}
	return "https://www.bilibili.com"
}

func (d BilibiliDownloader) fetchUser(ctx context.Context, secUID, cookie string) (bilibiliUser, error) {
	endpoint := d.baseURL() + "/aweme/v1/web/user/profile/other/"
	values := url.Values{}
	values.Set("sec_user_id", secUID)
	values.Set("device_platform", "webapp")
	values.Set("aid", "6383")
	values.Set("channel", "channel_pc_web")
	req, err := d.newRequest(ctx, endpoint+"?"+values.Encode(), cookie)
	if err != nil {
		return bilibiliUser{}, err
	}
	var body bilibiliUserResponse
	if err := d.doJSON(req, &body); err != nil {
		return bilibiliUser{}, err
	}
	if body.User.SecUID == "" {
		return bilibiliUser{}, errors.New("获取用户信息失败，请检查 Cookie 是否有效或重新登录B站")
	}
	return body.User, nil
}

func (d BilibiliDownloader) fetchPosts(ctx context.Context, secUID, cookie string) ([]bilibiliAweme, error) {
	var awemes []bilibiliAweme
	var cursor int64
	for page := 0; page < 200; page++ {
		endpoint := d.baseURL() + "/aweme/v1/web/aweme/post/"
		values := url.Values{}
		values.Set("sec_user_id", secUID)
		values.Set("max_cursor", fmt.Sprintf("%d", cursor))
		values.Set("count", "20")
		values.Set("device_platform", "webapp")
		values.Set("aid", "6383")
		values.Set("channel", "channel_pc_web")
		req, err := d.newRequest(ctx, endpoint+"?"+values.Encode(), cookie)
		if err != nil {
			return nil, err
		}
		var body bilibiliPostResponse
		if err := d.doJSON(req, &body); err != nil {
			return nil, err
		}
		if len(body.AwemeList) == 0 && len(awemes) == 0 && body.Status == 0 {
			return nil, errors.New("B站接口未返回作品列表，请刷新 Cookie 后重试")
		}
		for _, raw := range body.AwemeList {
			var aweme bilibiliAweme
			if err := json.Unmarshal(raw, &aweme); err != nil {
				continue
			}
			aweme.raw = raw
			if aweme.AwemeID != "" {
				awemes = append(awemes, aweme)
			}
		}
		if !bool(body.HasMore) || body.MaxCursor == cursor {
			break
		}
		cursor = body.MaxCursor
	}
	return awemes, nil
}

func (d BilibiliDownloader) newRequest(ctx context.Context, target, cookie string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en-US;q=0.8")
	req.Header.Set("Cookie", cookie)
	return req, nil
}

func (d BilibiliDownloader) doJSON(req *http.Request, target any) error {
	var lastErr error
	for attempt := 1; attempt <= bilibiliRequestAttempts; attempt++ {
		resp, err := d.client().Do(req)
		if err != nil {
			lastErr = err
			if shouldRetryRequest(err, 0, attempt) {
				sleepBeforeRetry(req.Context(), attempt)
				continue
			}
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		closeErr := resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("B站请求失败: HTTP %d", resp.StatusCode)
			if shouldRetryRequest(lastErr, resp.StatusCode, attempt) {
				sleepBeforeRetry(req.Context(), attempt)
				continue
			}
			return lastErr
		}
		if readErr != nil {
			lastErr = readErr
			if shouldRetryRequest(readErr, 0, attempt) {
				sleepBeforeRetry(req.Context(), attempt)
				continue
			}
			return readErr
		}
		if closeErr != nil {
			lastErr = closeErr
			if shouldRetryRequest(closeErr, 0, attempt) {
				sleepBeforeRetry(req.Context(), attempt)
				continue
			}
			return closeErr
		}
		if strings.TrimSpace(string(data)) == "" {
			return errors.New("B站接口返回空响应，请刷新 Cookie 后重试")
		}
		if err := json.Unmarshal(data, target); err != nil {
			return fmt.Errorf("解析B站响应失败: %w", err)
		}
		return nil
	}
	return lastErr
}

func (d BilibiliDownloader) writeAweme(ctx context.Context, outputDir string, aweme bilibiliAweme, cookie string, progress func(int64), retryNotice func(string)) (int, error) {
	prefix := flatAwemePrefix(aweme)
	if prefix == "" {
		return 0, errors.New("作品缺少可用文件名信息")
	}
	mediaURLs := awemeMediaURLs(aweme)
	if len(mediaURLs) == 0 {
		return 0, fmt.Errorf("作品 %s 没有可下载媒体", aweme.AwemeID)
	}
	downloaded := 0
	var firstPermanent error
	for index, mediaURL := range mediaURLs {
		ext := mediaURL.ext
		name := prefix + ext
		if len(mediaURLs) > 1 {
			name = fmt.Sprintf("%s_%d%s", prefix, index+1, ext)
		}
		if err := d.downloadFile(ctx, mediaURL.url, filepath.Join(outputDir, name), cookie, progress, retryNotice); err != nil {
			var permanent permanentMediaError
			if errors.As(err, &permanent) {
				if firstPermanent == nil {
					firstPermanent = permanentMediaError{Title: awemeTitle(aweme), Status: permanent.Status, File: name}
				}
				continue
			}
			return downloaded, err
		}
		downloaded++
	}
	if downloaded == 0 && firstPermanent != nil {
		return 0, firstPermanent
	}
	raw := aweme.raw
	if len(raw) == 0 {
		data, _ := json.Marshal(aweme)
		raw = data
	}
	jsonPath := filepath.Join(outputDir, prefix+"_data.json")
	return downloaded, os.WriteFile(jsonPath, raw, 0o644)
}

func (d BilibiliDownloader) downloadFile(ctx context.Context, target, path, cookie string, progress func(int64), retryNotice func(string)) error {
	return d.downloadFileWithOptions(ctx, target, path, cookie, "", progress, retryNotice)
}

func (d BilibiliDownloader) downloadFileWithReferer(ctx context.Context, target, path, cookie, referer string) error {
	return d.downloadFileWithOptions(ctx, target, path, cookie, referer, nil, nil)
}

func (d BilibiliDownloader) downloadFileWithOptions(ctx context.Context, target, path, cookie, referer string, progress func(int64), retryNotice func(string)) error {
	var lastErr error
	for attempt := 1; attempt <= bilibiliRequestAttempts; attempt++ {
		req, err := d.newRequest(ctx, target, cookie)
		if err != nil {
			return err
		}
		if strings.TrimSpace(referer) != "" {
			req.Header.Set("Referer", strings.TrimSpace(referer))
		}
		resp, err := d.client().Do(req)
		if err != nil {
			lastErr = err
			if shouldRetryRequest(err, 0, attempt) {
				reportRetryNotice(ctx, retryNotice, attempt)
				sleepBeforeRetry(ctx, attempt)
				continue
			}
			return err
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			return permanentMediaError{Status: resp.StatusCode, File: filepath.Base(path)}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("媒体下载失败 %s: HTTP %d", filepath.Base(path), resp.StatusCode)
			if shouldRetryRequest(lastErr, resp.StatusCode, attempt) {
				reportRetryNotice(ctx, retryNotice, attempt)
				sleepBeforeRetry(ctx, attempt)
				continue
			}
			return lastErr
		}
		finalPath := path
		if ct := resp.Header.Get("Content-Type"); filepath.Ext(finalPath) == ".webp" && ct != "" {
			if exts, _ := mime.ExtensionsByType(strings.Split(ct, ";")[0]); len(exts) > 0 {
				switch exts[0] {
				case ".jpg", ".jpeg", ".png", ".gif", ".webp":
					finalPath = strings.TrimSuffix(finalPath, filepath.Ext(finalPath)) + exts[0]
				}
			}
		}
		tmp := finalPath + ".tmp"
		_ = os.Remove(tmp)
		file, err := os.Create(tmp)
		if err != nil {
			_ = resp.Body.Close()
			return err
		}
		reader := &speedReader{reader: resp.Body, progress: progress, started: time.Now()}
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		bodyErr := resp.Body.Close()
		if copyErr != nil {
			_ = os.Remove(tmp)
			lastErr = copyErr
			if shouldRetryRequest(copyErr, 0, attempt) {
				reportRetryNotice(ctx, retryNotice, attempt)
				sleepBeforeRetry(ctx, attempt)
				continue
			}
			return copyErr
		}
		if closeErr != nil {
			_ = os.Remove(tmp)
			return closeErr
		}
		if bodyErr != nil {
			_ = os.Remove(tmp)
			lastErr = bodyErr
			if shouldRetryRequest(bodyErr, 0, attempt) {
				reportRetryNotice(ctx, retryNotice, attempt)
				sleepBeforeRetry(ctx, attempt)
				continue
			}
			return bodyErr
		}
		if err := os.Rename(tmp, finalPath); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return nil
	}
	return lastErr
}

func shouldRetryRequest(err error, statusCode, attempt int) bool {
	if attempt >= bilibiliRequestAttempts {
		return false
	}
	if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
		return true
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection reset") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "unexpected eof") ||
		strings.Contains(message, "timeout") ||
		strings.Contains(message, "temporarily unavailable") ||
		strings.Contains(message, "stream error")
}

func sleepBeforeRetry(ctx context.Context, attempt int) {
	delay := time.Duration(200*(1<<(attempt-1))) * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func reportRetryNotice(ctx context.Context, retryNotice func(string), attempt int) {
	if retryNotice == nil || ctx.Err() != nil {
		return
	}
	retryNotice(fmt.Sprintf("网络中断，正在重试 %d/%d", attempt+1, bilibiliRequestAttempts))
}

func appendLimitedFailure(failures []string, message string) []string {
	message = strings.TrimSpace(message)
	if message == "" || len(failures) >= 3 {
		return failures
	}
	return append(failures, trimRunes(message, 80))
}

func downloadNotice(base string, failures []string) string {
	base = strings.TrimSpace(base)
	if len(failures) == 0 {
		return base
	}
	summary := "不可下载作品：" + strings.Join(failures, "；")
	if len(failures) >= 3 {
		summary += "；其余略"
	}
	if base == "" {
		return trimRunes(summary, 240)
	}
	return trimRunes(base+"；"+summary, 240)
}

func awemeTitle(aweme bilibiliAweme) string {
	title := strings.TrimSpace(aweme.Desc)
	if title == "" {
		title = strings.TrimSpace(aweme.AwemeID)
	}
	if title == "" {
		title = "未知作品"
	}
	return trimRunes(title, 40)
}

func trimRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

type speedReader struct {
	reader     io.Reader
	progress   func(int64)
	started    time.Time
	lastReport time.Time
	bytes      int64
}

func (r *speedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.bytes += int64(n)
		now := time.Now()
		if r.progress != nil && (r.lastReport.IsZero() || now.Sub(r.lastReport) >= 500*time.Millisecond) {
			elapsed := now.Sub(r.started).Seconds()
			if elapsed > 0 {
				r.progress(int64(float64(r.bytes) / elapsed))
			}
			r.lastReport = now
		}
	}
	if err == io.EOF && r.progress != nil {
		elapsed := time.Since(r.started).Seconds()
		if elapsed > 0 {
			r.progress(int64(float64(r.bytes) / elapsed))
		}
	}
	return n, err
}

type downloadableMedia struct {
	url string
	ext string
}

func awemeMediaURLs(aweme bilibiliAweme) []downloadableMedia {
	if u := firstURL(aweme.Video.PlayAddr, aweme.Video.DownloadAddr); u != "" {
		if !isAudioURL(u) {
			return []downloadableMedia{{url: u, ext: ".mp4"}}
		}
	}
	for _, item := range aweme.Video.BitRate {
		if u := firstURL(item.PlayAddr); u != "" {
			if !isAudioURL(u) {
				return []downloadableMedia{{url: u, ext: ".mp4"}}
			}
		}
	}
	images := aweme.Images
	if len(images) == 0 {
		images = aweme.ImagePost.Images
	}
	var media []downloadableMedia
	for _, image := range images {
		if u := firstURL(image.OriginImage, image.DisplayImage, image.DownloadURL, bilibiliURLList{URLList: image.URLList}); u != "" {
			if isAudioURL(u) {
				continue
			}
			media = append(media, downloadableMedia{url: u, ext: imageExt(u)})
		}
	}
	return media
}

func isAudioURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(filepath.Ext(parsed.Path)) {
	case ".mp3", ".m4a", ".aac", ".wav", ".flac", ".ogg", ".opus":
		return true
	default:
		return false
	}
}

func firstURL(lists ...bilibiliURLList) string {
	for _, list := range lists {
		for _, candidate := range list.URLList {
			if strings.TrimSpace(candidate) != "" {
				return candidate
			}
		}
		if strings.TrimSpace(list.URL) != "" {
			return list.URL
		}
	}
	return ""
}

func firstString(lists ...[]string) string {
	for _, list := range lists {
		for _, candidate := range list {
			if strings.TrimSpace(candidate) != "" {
				return strings.TrimSpace(candidate)
			}
		}
	}
	return ""
}

func imageExt(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil {
		switch strings.ToLower(filepath.Ext(parsed.Path)) {
		case ".jpg", ".jpeg", ".png", ".gif", ".webp":
			return strings.ToLower(filepath.Ext(parsed.Path))
		}
	}
	return ".webp"
}

func flatAwemePrefix(aweme bilibiliAweme) string {
	if aweme.AwemeID == "" {
		return ""
	}
	date := "1970-01-01"
	if aweme.CreateTime > 0 {
		date = time.Unix(aweme.CreateTime, 0).Format("2006-01-02")
	}
	title := sanitizeFlatFilename(aweme.Desc, maxTitleBytesForAweme(aweme.AwemeID))
	if title == "" {
		title = aweme.AwemeID
	}
	return fmt.Sprintf("%s_%s_%s", date, title, aweme.AwemeID)
}

func sanitizeFlatFilename(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer("/", " ", "\\", " ", ":", " ", "*", " ", "?", " ", "\"", " ", "<", " ", ">", " ", "|", " ", "\n", " ", "\r", " ", "\t", " ")
	value = replacer.Replace(value)
	value = strings.Join(strings.Fields(value), " ")
	if maxBytes > 0 {
		value = trimUTF8Bytes(value, maxBytes)
	}
	return strings.TrimSpace(value)
}

func maxTitleBytesForAweme(awemeID string) int {
	const maxComponentBytes = 180
	overhead := len("2006-01-02__") + len(awemeID)
	available := maxComponentBytes - overhead
	if available < 24 {
		return 24
	}
	return available
}

func trimUTF8Bytes(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	var builder strings.Builder
	bytesUsed := 0
	for _, r := range value {
		size := len(string(r))
		if bytesUsed+size > maxBytes {
			break
		}
		builder.WriteRune(r)
		bytesUsed += size
	}
	return strings.TrimSpace(builder.String())
}

func extractBilibiliSecUID(profileURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(profileURL))
	if err == nil {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		for i, part := range parts {
			if part == "user" && i+1 < len(parts) {
				return parts[i+1]
			}
			if part == "space" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
		if parsed.Hostname() == "space.bilibili.com" && len(parts) > 0 {
			return parts[0]
		}
	}
	re := regexp.MustCompile(`/user/([A-Za-z0-9_-]+)`)
	if match := re.FindStringSubmatch(profileURL); len(match) == 2 {
		return match[1]
	}
	re = regexp.MustCompile(`space\.bilibili\.com/([A-Za-z0-9_-]+)`)
	if match := re.FindStringSubmatch(profileURL); len(match) == 2 {
		return match[1]
	}
	return ""
}

func existingAwemeIDs(dir string) (map[string]bool, error) {
	ids := map[string]bool{}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return ids, nil
	}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if id := ParseAwemeID(filepath.Base(path)); id != "" {
			ids[id] = true
		}
		return nil
	})
	return ids, err
}

func ytdlpMaxWorks() int {
	value := strings.TrimSpace(os.Getenv("LOCALBILIBILI_YTDLP_MAX_WORKS"))
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func ytdlpCommandTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("LOCALBILIBILI_YTDLP_TIMEOUT_SECONDS"))
	if value == "" {
		return 5 * time.Minute
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(seconds) * time.Second
}

func ytdlpFormat() string {
	if value := strings.TrimSpace(os.Getenv("LOCALBILIBILI_YTDLP_FORMAT")); value != "" {
		return value
	}
	return "bv*+ba/bestvideo+bestaudio/best"
}

func appendYTDLPCookieHeader(args []string, cookie string) []string {
	args = append(args,
		"--add-header", "Origin: https://www.bilibili.com",
		"--add-header", "Referer: https://www.bilibili.com/",
	)
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return args
	}
	return append(args, "--add-header", "Cookie: "+cookie)
}

func ytdlpEntryID(entry ytdlpEntry) string {
	id := strings.TrimSpace(entry.ID)
	if strings.HasPrefix(id, "BV") {
		return id
	}
	for _, raw := range []string{entry.WebpageURL, entry.URL} {
		if id := extractBVID(raw); id != "" {
			return id
		}
	}
	return id
}

func readYTDLPInfoEntry(path string) ytdlpEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return ytdlpEntry{}
	}
	var entry ytdlpEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return ytdlpEntry{}
	}
	return entry
}

func preferredVideoTitle(bvid string, values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !isBVIDFallbackTitle(value, bvid) {
			return value
		}
	}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return strings.TrimSpace(bvid)
}

func isBVIDFallbackTitle(title, bvid string) bool {
	title = strings.TrimSpace(title)
	bvid = strings.TrimSpace(bvid)
	if title == "" {
		return true
	}
	if bvid != "" && title == bvid {
		return true
	}
	return extractBVID(title) == title
}

func extractBVID(raw string) string {
	re := regexp.MustCompile(`BV[0-9A-Za-z]+`)
	if match := re.FindString(raw); match != "" {
		return match
	}
	return ""
}

func extractBilibiliMID(profileURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(profileURL))
	if err == nil {
		host := strings.ToLower(parsed.Hostname())
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if host == "space.bilibili.com" && len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
		for i, part := range parts {
			if (part == "space" || part == "user") && i+1 < len(parts) {
				return strings.TrimSpace(parts[i+1])
			}
		}
	}
	re := regexp.MustCompile(`space\.bilibili\.com/([0-9]+)`)
	if match := re.FindStringSubmatch(profileURL); len(match) == 2 {
		return match[1]
	}
	return extractBilibiliSecUID(profileURL)
}

func normalizeBilibiliSpaceURL(raw, mid string) string {
	if mid = strings.TrimSpace(mid); mid != "" {
		return "https://space.bilibili.com/" + mid
	}
	if strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return ""
}

func ytdlpEntryDateWithDetail(entry ytdlpEntry, detail bilibiliVideoDetail) string {
	if detail.PublishedAt != "" {
		if t, err := time.Parse(time.RFC3339, detail.PublishedAt); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ytdlpEntryDate(entry)
}

func ytdlpEntryDate(entry ytdlpEntry) string {
	if entry.Timestamp > 0 {
		return time.Unix(entry.Timestamp, 0).Format("2006-01-02")
	}
	if len(entry.UploadDate) == 8 {
		return entry.UploadDate[:4] + "-" + entry.UploadDate[4:6] + "-" + entry.UploadDate[6:8]
	}
	return time.Now().Format("2006-01-02")
}

func ytdlpEntryPublishedAt(entry ytdlpEntry) string {
	if entry.Timestamp > 0 {
		return time.Unix(entry.Timestamp, 0).UTC().Format(time.RFC3339)
	}
	if len(entry.UploadDate) == 8 {
		t, err := time.Parse("20060102", entry.UploadDate)
		if err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func classifyYTDLPWorkType(entry ytdlpEntry) string {
	if entry.Duration > 0 && entry.Duration <= 180 {
		return "video_short"
	}
	return "video_long"
}

func findDownloadedMedia(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "media_001.") || strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".part") {
			continue
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".mp4", ".m4v", ".webm", ".mkv", ".mov":
			return filepath.Join(dir, name), nil
		}
	}
	return "", errors.New("yt-dlp 未生成可播放视频文件")
}

func findDownloadedThumbnail(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "media_001.") {
			continue
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".jpg", ".jpeg", ".png", ".webp":
			return filepath.Join(dir, name)
		}
	}
	return ""
}

func findFirstBySuffix(dir, suffix string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			return filepath.Join(dir, entry.Name())
		}
	}
	return ""
}

func (d BilibiliDownloader) repairLocalBilibiliMetadata(ctx context.Context, metadataPath, cookie string) (bool, error) {
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return false, err
	}
	var meta localBilibiliDownloadMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return false, err
	}
	bvid := strings.TrimSpace(meta.BVID)
	if bvid == "" || !isBVIDFallbackTitle(meta.Title, bvid) {
		return false, nil
	}
	detail, err := d.fetchBilibiliVideoDetail(ctx, bvid, cookie)
	if err != nil {
		return false, err
	}
	changed := applyVideoDetailToMetadata(&meta, detail)
	if !changed {
		return false, nil
	}
	next, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(metadataPath, next, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func applyVideoDetailToMetadata(meta *localBilibiliDownloadMetadata, detail bilibiliVideoDetail) bool {
	changed := false
	if strings.TrimSpace(detail.Title) != "" && meta.Title != detail.Title {
		meta.Title = detail.Title
		changed = true
	}
	if strings.TrimSpace(detail.Description) != "" && meta.Description != detail.Description {
		meta.Description = detail.Description
		changed = true
	}
	if strings.TrimSpace(detail.PublishedAt) != "" && meta.PublishedAt != detail.PublishedAt {
		meta.PublishedAt = detail.PublishedAt
		changed = true
	}
	if strings.TrimSpace(detail.SourceURL) != "" && meta.SourceURL != detail.SourceURL {
		meta.SourceURL = detail.SourceURL
		changed = true
	}
	return changed
}

func existingLocalBilibiliMetadataPaths(dir string) (map[string]string, error) {
	paths := map[string]string{}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return paths, nil
	}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "metadata.localbilibili.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var meta localBilibiliDownloadMetadata
		if err := json.Unmarshal(data, &meta); err == nil {
			if id := firstNonEmpty(meta.BVID, meta.DynamicID); id != "" {
				paths[id] = path
			}
		}
		return nil
	})
	return paths, err
}

func existingLocalBilibiliWorkIDs(dir string) (map[string]bool, error) {
	paths, err := existingLocalBilibiliMetadataPaths(dir)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]bool, len(paths))
	for id := range paths {
		ids[id] = true
	}
	return ids, nil
}

func isBilibiliOpusURL(raw string) bool {
	return extractBilibiliOpusID(raw) != ""
}

func extractBilibiliOpusID(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err == nil {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		for i, part := range parts {
			if part == "opus" && i+1 < len(parts) {
				if id := strings.TrimSpace(parts[i+1]); id != "" {
					return id
				}
			}
		}
	}
	re := regexp.MustCompile(`/opus/([0-9]+)`)
	if match := re.FindStringSubmatch(raw); len(match) == 2 {
		return match[1]
	}
	return ""
}

func parseOpusTitle(page string) string {
	for _, pattern := range []string{
		`(?is)<[^>]+class=["'][^"']*opus-module-title__text[^"']*["'][^>]*>(.*?)</[^>]+>`,
		`"module_title"\s*:\s*\{[^{}]*"text"\s*:\s*"([^"]+)"`,
	} {
		if value := firstJSONLikeString(page, pattern); value != "" {
			return trimRunes(stripHTML(value), 160)
		}
	}
	re := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	if match := re.FindStringSubmatch(page); len(match) == 2 {
		title := html.UnescapeString(stripHTML(match[1]))
		title = strings.TrimSpace(strings.TrimSuffix(title, "- 哔哩哔哩"))
		title = strings.TrimSpace(strings.TrimSuffix(title, " - 哔哩哔哩"))
		title = strings.TrimSpace(strings.TrimSuffix(title, "的动态"))
		if title != "" {
			return title
		}
	}
	if value := firstJSONLikeString(page, `"title"\s*:\s*"([^"]+)"`); value != "" {
		return value
	}
	return ""
}

func parseOpusDescription(page string) string {
	for _, pattern := range []string{
		`"words"\s*:\s*"([^"]*)"`,
		`"text"\s*:\s*"([^"]*)"`,
		`"desc"\s*:\s*"([^"]*)"`,
		`"content"\s*:\s*"([^"]*)"`,
	} {
		if value := firstJSONLikeString(page, pattern); value != "" {
			return trimRunes(value, 500)
		}
	}
	return ""
}

func parseOpusMID(page string) string {
	for _, pattern := range []string{
		`"mid"\s*:\s*"?([0-9]+)"?`,
		`"uid"\s*:\s*"?([0-9]+)"?`,
	} {
		re := regexp.MustCompile(pattern)
		if match := re.FindStringSubmatch(page); len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func parseOpusAuthorName(page string) string {
	for _, pattern := range []string{
		`"name"\s*:\s*"([^"]+)"`,
		`"uname"\s*:\s*"([^"]+)"`,
		`"author_name"\s*:\s*"([^"]+)"`,
	} {
		if value := firstJSONLikeString(page, pattern); value != "" {
			return value
		}
	}
	return ""
}

func parseOpusPublishedAt(page string) string {
	for _, pattern := range []string{
		`"pub_ts"\s*:\s*"?([0-9]+)"?`,
		`"timestamp"\s*:\s*"?([0-9]+)"?`,
	} {
		re := regexp.MustCompile(pattern)
		if match := re.FindStringSubmatch(page); len(match) == 2 {
			if ts, err := strconv.ParseInt(match[1], 10, 64); err == nil && ts > 0 {
				return time.Unix(ts, 0).UTC().Format(time.RFC3339)
			}
		}
	}
	for _, pattern := range []string{
		`"pub_time"\s*:\s*"([^"]+)"`,
		`(?is)<[^>]+class=["'][^"']*opus-module-author__pub__text[^"']*["'][^>]*>(.*?)</[^>]+>`,
	} {
		if value := firstJSONLikeString(page, pattern); value != "" {
			value = strings.TrimSpace(stripHTML(value))
			loc, err := time.LoadLocation("Asia/Shanghai")
			if err != nil {
				loc = time.FixedZone("CST", 8*3600)
			}
			if t, err := time.ParseInLocation("2006年01月02日 15:04", value, loc); err == nil {
				return t.UTC().Format(time.RFC3339)
			}
		}
	}
	return ""
}

func parseOpusImageURLs(page string) []string {
	re := regexp.MustCompile(`(?i)(?:https?:)?//[^"' <>\n\r]+/bfs/(?:new_dyn|album|article|archive)/[^"' <>\n\r]+?\.(?:jpg|jpeg|png|webp)(?:@[^"' <>\n\r]+)?`)
	matches := re.FindAllString(page, -1)
	seen := map[string]bool{}
	var urls []string
	for _, raw := range matches {
		value := html.UnescapeString(strings.TrimSpace(raw))
		if strings.HasPrefix(value, "//") {
			value = "https:" + value
		}
		if at := strings.Index(value, "@"); at >= 0 {
			value = value[:at]
		}
		if strings.Contains(value, "/face/") || strings.Contains(value, "/static/") || strings.Contains(value, "/seed/") {
			continue
		}
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		urls = append(urls, value)
	}
	return urls
}

func firstJSONLikeString(page, pattern string) string {
	re := regexp.MustCompile(pattern)
	if match := re.FindStringSubmatch(page); len(match) == 2 {
		value := strings.ReplaceAll(match[1], `\"`, `"`)
		value = strings.ReplaceAll(value, `\n`, "\n")
		value = strings.ReplaceAll(value, `\u002F`, "/")
		return strings.TrimSpace(html.UnescapeString(value))
	}
	return ""
}

func stripHTML(value string) string {
	re := regexp.MustCompile(`(?is)<[^>]+>`)
	return re.ReplaceAllString(value, "")
}

func taskSafeCreatorNameFromURL(raw, mid string) string {
	if mid != "" {
		return mid
	}
	if host := extractBilibiliMID(raw); host != "" {
		return host
	}
	return "B站UP主"
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func isPermanentYTDLPError(err error) bool {
	if err == nil {
		return false
	}
	var permanent permanentYTDLPError
	if errors.As(err, &permanent) {
		return true
	}
	message := strings.ToLower(err.Error())
	return isPermanentYTDLPMessage(message)
}

func isPermanentYTDLPMessage(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "http error 403") ||
		strings.Contains(message, "http error 404") ||
		strings.Contains(message, "http error 412") ||
		strings.Contains(message, "precondition failed") ||
		strings.Contains(message, "unable to extract initial state") ||
		(strings.Contains(message, "[bilibili]") && strings.Contains(message, "extracti")) ||
		strings.Contains(message, "下载超时") ||
		strings.Contains(message, "private") ||
		strings.Contains(message, "会员") ||
		strings.Contains(message, "付费") ||
		strings.Contains(message, "copyright")
}

func sanitizeYTDLPError(message string) string {
	message = strings.TrimSpace(message)
	cookieRe := regexp.MustCompile(`(?i)(cookie:\s*)[^\n]+`)
	message = cookieRe.ReplaceAllString(message, "${1}[hidden]")
	if len(message) > 1000 {
		message = message[:1000] + "..."
	}
	return message
}
