package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const DatabaseFileName = "localbilibili.db"
const DefaultDownloaderConcurrency = 1
const MaxDownloaderConcurrency = 6
const DefaultSubtitleConcurrency = 1
const MaxSubtitleConcurrency = 6

type AppConfig struct {
	DatabaseDir           string                `json:"database_dir"`
	DownloaderConcurrency int                   `json:"downloader_concurrency"`
	Subtitle              SubtitleRuntimeConfig `json:"subtitle"`
	Learning              LearningRuntimeConfig `json:"learning"`
}

// LearningRuntimeConfig deliberately contains no WiseMapping password or Codex
// credentials.  Those remain in the fnOS private runtime directory.
type LearningRuntimeConfig struct {
	CodexPath       string `json:"codex_path"`
	WiseMappingURL  string `json:"wisemapping_url"`
	WiseMappingUser string `json:"wisemapping_user"`
	DefaultCron     string `json:"default_cron"`
	WorkPrompt      string `json:"work_prompt"`
	CreatorPrompt   string `json:"creator_prompt"`
}

type DatabaseConfig struct {
	Directory string `json:"directory"`
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Writable  bool   `json:"writable"`
}

type RuntimeConfig struct {
	DownloaderConcurrency int `json:"downloader_concurrency"`
}

type SubtitleRuntimeConfig struct {
	Mode        string `json:"mode"`
	Concurrency int    `json:"concurrency"`
	CLIPath     string `json:"cli_path"`
	APIURL      string `json:"api_url"`
	WhisperPath string `json:"whisper_path"`
	FFmpegPath  string `json:"ffmpeg_path"`
	ModelPath   string `json:"model_path"`
	Language    string `json:"language"`
	GPU         bool   `json:"gpu"`
}

func LoadConfig(path string, defaultDatabaseDir string) (AppConfig, error) {
	cfg := AppConfig{DatabaseDir: filepath.Clean(defaultDatabaseDir), DownloaderConcurrency: DefaultDownloaderConcurrency, Subtitle: DefaultSubtitleRuntimeConfig(), Learning: DefaultLearningRuntimeConfig()}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return AppConfig{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, err
	}
	if strings.TrimSpace(cfg.DatabaseDir) == "" {
		cfg.DatabaseDir = defaultDatabaseDir
	}
	cfg.DatabaseDir = filepath.Clean(cfg.DatabaseDir)
	cfg.DownloaderConcurrency = normalizeDownloaderConcurrency(cfg.DownloaderConcurrency)
	cfg.Subtitle = normalizeSubtitleRuntimeConfig(cfg.Subtitle)
	cfg.Learning = normalizeLearningRuntimeConfig(cfg.Learning)
	return cfg, nil
}

func DefaultLearningRuntimeConfig() LearningRuntimeConfig {
	return LearningRuntimeConfig{DefaultCron: "0 4 * * *", WorkPrompt: workMindMapInstruction, CreatorPrompt: creatorMindMapInstruction}
}

func normalizeLearningRuntimeConfig(cfg LearningRuntimeConfig) LearningRuntimeConfig {
	cfg.CodexPath = strings.TrimSpace(cfg.CodexPath)
	cfg.WiseMappingURL = strings.TrimRight(strings.TrimSpace(cfg.WiseMappingURL), "/")
	cfg.WiseMappingUser = strings.TrimSpace(cfg.WiseMappingUser)
	cfg.DefaultCron = strings.TrimSpace(cfg.DefaultCron)
	cfg.WorkPrompt = strings.TrimSpace(cfg.WorkPrompt)
	cfg.CreatorPrompt = strings.TrimSpace(cfg.CreatorPrompt)
	if !ValidCron5(cfg.DefaultCron) {
		cfg.DefaultCron = DefaultLearningRuntimeConfig().DefaultCron
	}
	if cfg.WorkPrompt == "" {
		cfg.WorkPrompt = DefaultLearningRuntimeConfig().WorkPrompt
	}
	if cfg.CreatorPrompt == "" {
		cfg.CreatorPrompt = DefaultLearningRuntimeConfig().CreatorPrompt
	}
	return cfg
}

func SaveConfig(path string, cfg AppConfig) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func normalizeDownloaderConcurrency(value int) int {
	if value < 1 {
		return DefaultDownloaderConcurrency
	}
	if value > MaxDownloaderConcurrency {
		return MaxDownloaderConcurrency
	}
	return value
}

func DefaultSubtitleRuntimeConfig() SubtitleRuntimeConfig {
	return normalizeSubtitleRuntimeConfig(SubtitleRuntimeConfig{
		Mode:        "local",
		Concurrency: DefaultSubtitleConcurrency,
		Language:    "zh",
		GPU:         true,
	})
}

func normalizeSubtitleRuntimeConfig(cfg SubtitleRuntimeConfig) SubtitleRuntimeConfig {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "local", "api", "scan":
		cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	default:
		cfg.Mode = "local"
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = DefaultSubtitleConcurrency
	}
	if cfg.Concurrency > MaxSubtitleConcurrency {
		cfg.Concurrency = MaxSubtitleConcurrency
	}
	if strings.TrimSpace(cfg.Language) == "" {
		cfg.Language = "zh"
	}
	cfg.CLIPath = strings.TrimSpace(cfg.CLIPath)
	cfg.APIURL = strings.TrimSpace(cfg.APIURL)
	cfg.WhisperPath = strings.TrimSpace(cfg.WhisperPath)
	cfg.FFmpegPath = strings.TrimSpace(cfg.FFmpegPath)
	cfg.ModelPath = strings.TrimSpace(cfg.ModelPath)
	return cfg
}

func (cfg AppConfig) Runtime() RuntimeConfig {
	return RuntimeConfig{DownloaderConcurrency: normalizeDownloaderConcurrency(cfg.DownloaderConcurrency)}
}

func DatabasePath(directory string) string {
	return filepath.Join(filepath.Clean(directory), DatabaseFileName)
}

func InspectDatabaseDirectory(directory string) (DatabaseConfig, error) {
	clean := filepath.Clean(directory)
	if err := ValidateDirectory(clean); err != nil {
		return DatabaseConfig{}, err
	}
	dbPath := DatabasePath(clean)
	_, statErr := os.Stat(dbPath)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return DatabaseConfig{}, statErr
	}
	return DatabaseConfig{
		Directory: clean,
		Path:      dbPath,
		Exists:    exists,
		Writable:  directoryWritable(clean),
	}, nil
}

func directoryWritable(directory string) bool {
	file, err := os.CreateTemp(directory, ".localbilibili-write-test-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}
