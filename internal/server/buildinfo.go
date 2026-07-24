package server

import (
	"runtime"
	"strings"
)

var (
	buildVersion = "dev"
	buildCommit  = ""
	buildDate    = ""
	buildMode    = "single-go-service"
)

type AboutInfo struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Commit       string `json:"commit,omitempty"`
	BuildDate    string `json:"build_date,omitempty"`
	GoVersion    string `json:"go_version,omitempty"`
	Platform     string `json:"platform,omitempty"`
	BuildMode    string `json:"build_mode,omitempty"`
	Description  string `json:"description"`
	DatabasePath string `json:"database_path,omitempty"`
}

func About(databasePath string) AboutInfo {
	return AboutInfo{
		Name:         "LocalBilibili",
		Version:      strings.TrimSpace(buildVersion),
		Commit:       strings.TrimSpace(buildCommit),
		BuildDate:    strings.TrimSpace(buildDate),
		GoVersion:    runtime.Version(),
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
		BuildMode:    strings.TrimSpace(buildMode),
		Description:  "局域网版本地B站作品浏览与更新 Web 应用",
		DatabasePath: strings.TrimSpace(databasePath),
	}
}
