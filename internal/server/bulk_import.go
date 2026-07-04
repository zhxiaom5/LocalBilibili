package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type BulkImportItem struct {
	Input             string `json:"input"`
	ProfileURL        string `json:"bilibili_space_url,omitempty"`
	SecUID            string `json:"bilibili_mid,omitempty"`
	UID               string `json:"bilibili_uid,omitempty"`
	DisplayName       string `json:"display_name,omitempty"`
	AvatarURL         string `json:"avatar_url,omitempty"`
	Bio               string `json:"bio,omitempty"`
	ResolveStatus     string `json:"resolve_status,omitempty"`
	Status            string `json:"status"`
	Existing          bool   `json:"existing"`
	ExistingCreatorID int64  `json:"existing_creator_id,omitempty"`
	Error             string `json:"error,omitempty"`
}

var bulkImportURLPattern = regexp.MustCompile(`https?://[^\s"'<>()，。]+`)

type BulkImportPreviewConfig struct {
	Resolver creatorProfileResolver
	Cookie   string
}

func redirectResolvingClient() *http.Client {
	return &http.Client{Timeout: 12 * time.Second}
}

func PreviewBulkImportCreators(ctx context.Context, store *Store, directoryID int64, text string, client *http.Client, configs ...BulkImportPreviewConfig) []BulkImportItem {
	if client == nil {
		client = redirectResolvingClient()
	}
	config := BulkImportPreviewConfig{}
	if len(configs) > 0 {
		config = configs[0]
	}
	seen := map[string]bool{}
	items := []BulkImportItem{}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}
		matches := bulkImportURLPattern.FindAllString(input, -1)
		if len(matches) == 0 {
			items = append(items, BulkImportItem{Input: input, Status: "error", Error: "no bilibili url found"})
			continue
		}
		for _, raw := range matches {
			raw = trimURLNoise(raw)
			profileURL, secUID, err := normalizeBilibiliProfileURL(ctx, raw, client)
			if err != nil {
				items = append(items, BulkImportItem{Input: input, Status: "error", Error: err.Error()})
				continue
			}
			if seen[profileURL] {
				continue
			}
			seen[profileURL] = true
			item := BulkImportItem{Input: input, ProfileURL: profileURL, SecUID: secUID, Status: "ready", ResolveStatus: "not_required"}
			if config.Resolver != nil {
				profile, err := resolveBulkImportProfile(ctx, config.Resolver, profileURL, config.Cookie)
				if err != nil {
					items = append(items, BulkImportItem{Input: input, ProfileURL: profileURL, SecUID: secUID, Status: "error", ResolveStatus: "failed", Error: err.Error()})
					continue
				}
				item.UID = profile.UID
				item.SecUID = firstNonEmpty(profile.SecUID, secUID)
				item.ProfileURL = firstNonEmpty(profile.ProfileURL, profileURL)
				item.DisplayName = profile.Name
				item.AvatarURL = profile.AvatarURL
				item.Bio = profile.Bio
				item.ResolveStatus = "resolved"
			}
			if store != nil {
				if creator, ok, err := store.CreatorByBulkImportIdentity(directoryID, item.DisplayName, item.SecUID, item.ProfileURL); err == nil && ok {
					item.Existing = true
					item.ExistingCreatorID = creator.ID
				}
			}
			items = append(items, item)
		}
	}
	return items
}

func resolveBulkImportProfile(ctx context.Context, resolver creatorProfileResolver, profileURL, cookie string) (CreatorProfile, error) {
	if strings.TrimSpace(cookie) == "" {
		return CreatorProfile{}, errors.New("Cookie 不能为空，请先在更新管理中保存B站 Cookie")
	}
	profileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	profile, err := resolver.ResolveCreatorProfile(profileCtx, profileURL, cookie)
	if err != nil {
		return CreatorProfile{}, errors.New("未能解析B站UP主资料，请检查 Cookie 或主页链接: " + err.Error())
	}
	profile.Name = preferredCreatorName(profile.Name)
	if profile.Name == "" {
		return CreatorProfile{}, errors.New("未能解析B站UP主资料，请检查 Cookie 或主页链接: B站未返回UP主昵称")
	}
	if profile.ProfileURL == "" {
		profile.ProfileURL = profileURL
	}
	if profile.SecUID == "" {
		profile.SecUID = extractBilibiliSecUID(profile.ProfileURL)
	}
	return profile, nil
}

func normalizeBilibiliProfileURL(ctx context.Context, raw string, client *http.Client) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", errors.New("invalid url")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "space.bilibili.com" {
		parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
		if len(parts) < 1 || strings.TrimSpace(parts[0]) == "" {
			return "", "", errors.New("missing bilibili mid")
		}
		mid, err := url.PathUnescape(parts[0])
		if err != nil || strings.TrimSpace(mid) == "" {
			return "", "", errors.New("invalid bilibili mid")
		}
		return "https://space.bilibili.com/" + mid, mid, nil
	}
	if strings.HasSuffix(host, "bilibili.com") && strings.HasPrefix(parsed.EscapedPath(), "/user/") {
		parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return "", "", errors.New("missing bilibili mid")
		}
		secUID, err := url.PathUnescape(parts[1])
		if err != nil || strings.TrimSpace(secUID) == "" {
			return "", "", errors.New("invalid bilibili mid")
		}
		return "https://www.bilibili.com/user/" + secUID, secUID, nil
	}
	if host == "v.bilibili.com" || host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "127.") {
		finalURL, err := resolveRedirectURL(ctx, raw, client)
		if err != nil {
			return "", "", err
		}
		if finalURL == raw {
			return "", "", errors.New("short link did not redirect to bilibili profile")
		}
		return normalizeBilibiliProfileURL(ctx, finalURL, client)
	}
	return "", "", errors.New("unsupported bilibili url")
}

func resolveRedirectURL(ctx context.Context, raw string, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "LocalBilibili/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String(), nil
	}
	return raw, nil
}

func trimURLNoise(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), ".,，。!?！？)）]")
}
