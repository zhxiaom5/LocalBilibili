package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type config struct {
	WhisperCLI string    `json:"whisper_cli"`
	FFmpeg     string    `json:"ffmpeg"`
	Model      string    `json:"model"`
	Language   string    `json:"language"`
	GPU        bool      `json:"gpu"`
	NoGPU      bool      `json:"-"`
	LogLevel   string    `json:"log_level"`
	LogWriter  io.Writer `json:"-"`
}

type checkResult struct {
	OK         bool   `json:"ok"`
	WhisperCLI string `json:"whisper_cli"`
	FFmpeg     string `json:"ffmpeg"`
	Model      string `json:"model"`
	CUDA       string `json:"cuda"`
	Message    string `json:"message"`
	Platform   string `json:"platform"`
	CheckedAt  string `json:"checked_at"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "check":
		err = runCheck(os.Args[2:])
	case "transcribe":
		err = runTranscribe(os.Args[2:])
	case "scan":
		err = runScan(os.Args[2:])
	case "daemon":
		err = runDaemon(os.Args[2:])
	default:
		usage()
		err = fmt.Errorf("unknown command: %s", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `subtitle-cli commands:
  check [--whisper-cli PATH] [--ffmpeg PATH] [--model PATH]
  transcribe --video PATH [--whisper-cli PATH] [--ffmpeg PATH] [--model PATH]
  scan --root DIR [--workers N] [--log-level silent|error|info|debug] [--whisper-cli PATH] [--ffmpeg PATH] [--model PATH]
  daemon --addr :9099 --root DIR [--workers N] [--log-level silent|error|info|debug] [--whisper-cli PATH] [--ffmpeg PATH] [--model PATH]`)
}

func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	cfg := bindConfigFlags(fs)
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}
	cfg.finalize()
	if err := cfg.validate(); err != nil {
		return err
	}
	result := checkRuntime(*cfg)
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	if !result.OK {
		return errors.New(result.Message)
	}
	return nil
}

func runTranscribe(args []string) error {
	fs := flag.NewFlagSet("transcribe", flag.ContinueOnError)
	cfg := bindConfigFlags(fs)
	video := fs.String("video", "", "video path")
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}
	cfg.finalize()
	if err := cfg.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(*video) == "" {
		return errors.New("--video is required")
	}
	return transcribeVideo(context.Background(), *cfg, *video)
}

func runScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	cfg := bindConfigFlags(fs)
	root := fs.String("root", "", "root directory")
	workers := fs.Int("workers", 1, "workers")
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}
	cfg.finalize()
	if err := cfg.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return errors.New("--root is required")
	}
	videos, err := collectVideosWithLog(*root, *cfg)
	if err != nil {
		return err
	}
	return runVideoWorkers(context.Background(), *cfg, videos, normalizeWorkers(*workers))
}

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	cfg := bindConfigFlags(fs)
	addr := fs.String("addr", ":9099", "listen address")
	root := fs.String("root", "", "root directory")
	workers := fs.Int("workers", 1, "workers")
	if err := fs.Parse(normalizeFlagArgs(args)); err != nil {
		return err
	}
	cfg.finalize()
	if err := cfg.validate(); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/check", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, checkRuntime(*cfg))
	})
	mux.HandleFunc("POST /api/transcribe", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			VideoPath string `json:"video_path"`
			Language  string `json:"language"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		nextCfg := *cfg
		if strings.TrimSpace(body.Language) != "" {
			nextCfg.Language = body.Language
		}
		if err := transcribeVideo(r.Context(), nextCfg, body.VideoPath); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/scan", func(w http.ResponseWriter, r *http.Request) {
		scanRoot := *root
		if strings.TrimSpace(scanRoot) == "" {
			var body struct {
				Root string `json:"root"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			scanRoot = body.Root
		}
		videos, err := collectVideos(scanRoot)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := runVideoWorkers(r.Context(), *cfg, videos, normalizeWorkers(*workers)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "videos": len(videos)})
	})
	return http.ListenAndServe(*addr, mux)
}

func bindConfigFlags(fs *flag.FlagSet) *config {
	cfg := defaultConfig()
	fs.StringVar(&cfg.WhisperCLI, "whisper-cli", cfg.WhisperCLI, "whisper-cli path")
	fs.StringVar(&cfg.FFmpeg, "ffmpeg", cfg.FFmpeg, "ffmpeg path")
	fs.StringVar(&cfg.Model, "model", cfg.Model, "model path")
	fs.StringVar(&cfg.Language, "language", cfg.Language, "language")
	fs.BoolVar(&cfg.NoGPU, "no-gpu", false, "disable gpu")
	fs.BoolVar(&cfg.GPU, "gpu", cfg.GPU, "enable gpu")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: silent, error, info, debug")
	return &cfg
}

func normalizeFlagArgs(args []string) []string {
	normalized := make([]string, len(args))
	for i, arg := range args {
		if strings.HasPrefix(arg, "--") && len(arg) > 2 {
			normalized[i] = "-" + strings.TrimPrefix(arg, "--")
			continue
		}
		normalized[i] = arg
	}
	return normalized
}

func (cfg *config) finalize() {
	if cfg.NoGPU {
		cfg.GPU = false
	}
	if strings.TrimSpace(cfg.Language) == "" {
		cfg.Language = "zh"
	}
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	if cfg.LogLevel == "" {
		cfg.LogLevel = "silent"
	}
	if cfg.LogWriter == nil {
		cfg.LogWriter = os.Stderr
	}
}

func (cfg config) validate() error {
	switch cfg.LogLevel {
	case "silent", "error", "info", "debug":
		return nil
	default:
		return fmt.Errorf("invalid --log-level %q, want silent, error, info, or debug", cfg.LogLevel)
	}
}

func defaultConfig() config {
	return config{
		WhisperCLI: firstNonEmpty(os.Getenv("SUBTITLE_WHISPER_CLI"), "whisper-cli"),
		FFmpeg:     firstNonEmpty(os.Getenv("SUBTITLE_FFMPEG"), "ffmpeg"),
		Model:      os.Getenv("SUBTITLE_MODEL"),
		Language:   firstNonEmpty(os.Getenv("SUBTITLE_LANGUAGE"), "zh"),
		GPU:        os.Getenv("SUBTITLE_NO_GPU") == "",
		LogLevel:   firstNonEmpty(os.Getenv("SUBTITLE_LOG_LEVEL"), "silent"),
		LogWriter:  os.Stderr,
	}
}

func checkRuntime(cfg config) checkResult {
	result := checkResult{
		WhisperCLI: cfg.WhisperCLI,
		FFmpeg:     cfg.FFmpeg,
		Model:      cfg.Model,
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
		CheckedAt:  time.Now().UTC().Format(time.RFC3339),
		CUDA:       cudaStatus(),
	}
	var missing []string
	if _, err := exec.LookPath(cfg.FFmpeg); err != nil && !fileExecutable(cfg.FFmpeg) {
		missing = append(missing, "ffmpeg")
	}
	if _, err := exec.LookPath(cfg.WhisperCLI); err != nil && !fileExecutable(cfg.WhisperCLI) {
		missing = append(missing, "whisper-cli")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		missing = append(missing, "model")
	} else if _, err := os.Stat(cfg.Model); err != nil {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		result.Message = "missing: " + strings.Join(missing, ", ")
		return result
	}
	result.OK = true
	result.Message = "subtitle runtime ready"
	return result
}

func transcribeVideo(ctx context.Context, cfg config, videoPath string) error {
	videoPath = filepath.Clean(videoPath)
	if _, err := os.Stat(videoPath); err != nil {
		return err
	}
	if result := checkRuntime(cfg); !result.OK {
		return errors.New(result.Message)
	}
	textPath, srtPath, jsonPath := outputPaths(videoPath)
	if fileNonEmpty(textPath) && fileNonEmpty(srtPath) && fileNonEmpty(jsonPath) {
		cfg.logf("info", "skip existing: %s", videoPath)
		return nil
	}
	cfg.logf("info", "transcribing: %s", videoPath)
	tmpDir, err := os.MkdirTemp("", "localbilibili-subtitle-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	wavPath := filepath.Join(tmpDir, "audio.wav")
	cfg.logf("debug", "extract audio: %s", videoPath)
	if err := run(ctx, cfg.FFmpeg, "-y", "-i", videoPath, "-ar", "16000", "-ac", "1", wavPath); err != nil {
		if isNoAudioError(err) {
			cfg.logf("info", "skip no audio: %s", videoPath)
			return nil
		}
		return err
	}
	outBase := filepath.Join(tmpDir, "subtitle")
	args := []string{"-m", cfg.Model, "-f", wavPath, "-l", cfg.Language, "-otxt", "-osrt", "-oj", "-of", outBase}
	if !cfg.GPU {
		args = append(args, "-ng")
	}
	cfg.logf("debug", "run whisper: %s", videoPath)
	if err := run(ctx, cfg.WhisperCLI, args...); err != nil {
		return err
	}
	for _, pair := range [][2]string{
		{outBase + ".txt", textPath},
		{outBase + ".srt", srtPath},
		{outBase + ".json", jsonPath},
	} {
		if err := copyFile(pair[0], pair[1]); err != nil {
			return err
		}
	}
	cfg.logf("info", "done: %s", videoPath)
	return nil
}

func collectVideos(root string) ([]string, error) {
	return collectVideosWithLog(root, config{})
}

func collectVideosWithLog(root string, cfg config) ([]string, error) {
	root = filepath.Clean(root)
	cfg.logf("info", "collecting videos: root=%s", root)
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("root is not a directory")
	}
	var videos []string
	creators, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, creator := range creators {
		if !creator.IsDir() {
			continue
		}
		creatorDir := filepath.Join(root, creator.Name())
		before := len(videos)
		cfg.logf("info", "scan creator: %s", creator.Name())
		_ = filepath.WalkDir(creatorDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if path != creatorDir && d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if isVideoFile(path) {
				videos = append(videos, path)
				cfg.logf("debug", "found video: %s", path)
			}
			return nil
		})
		cfg.logf("info", "creator videos: %s count=%d", creator.Name(), len(videos)-before)
	}
	cfg.logf("info", "collect finished: videos=%d", len(videos))
	return videos, nil
}

func runVideoWorkers(ctx context.Context, cfg config, videos []string, workers int) error {
	jobs := make(chan string)
	errs := make(chan error, len(videos))
	var wg sync.WaitGroup
	cfg.logf("info", "scan started: videos=%d workers=%d", len(videos), workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				if err := transcribeVideo(ctx, cfg, path); err != nil {
					cfg.logf("error", "failed: %s: %v", path, err)
					errs <- fmt.Errorf("%s: %w", path, err)
				}
			}
		}()
	}
	for _, path := range videos {
		jobs <- path
	}
	close(jobs)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	cfg.logf("info", "scan finished: videos=%d", len(videos))
	return nil
}

func (cfg config) logf(level, format string, args ...any) {
	if !cfg.shouldLog(level) || cfg.LogWriter == nil {
		return
	}
	fmt.Fprintf(cfg.LogWriter, "[%s] %s\n", level, fmt.Sprintf(format, args...))
}

func (cfg config) shouldLog(level string) bool {
	current := logLevelValue(cfg.LogLevel)
	target := logLevelValue(level)
	return current > 0 && target > 0 && target <= current
}

func logLevelValue(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error":
		return 1
	case "info":
		return 2
	case "debug":
		return 3
	default:
		return 0
	}
}

func isNoAudioError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "Output file does not contain any stream") ||
		strings.Contains(message, "Stream map 'a' matches no streams") ||
		strings.Contains(message, "does not contain any audio")
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir := filepath.Dir(name); dir != "." && dir != "" {
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+prependPath(dir, os.Getenv("LD_LIBRARY_PATH")))
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return errors.New(message)
	}
	return nil
}

func prependPath(pathValue, existing string) string {
	if strings.TrimSpace(existing) == "" {
		return pathValue
	}
	return pathValue + ":" + existing
}

func outputPaths(videoPath string) (string, string, string) {
	ext := filepath.Ext(videoPath)
	stem := strings.TrimSuffix(videoPath, ext)
	return stem + ".txt", stem + ".srt", stem + ".json"
}

func isVideoFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mov", ".mkv", ".m4v", ".webm":
		return true
	default:
		return false
	}
}

func normalizeWorkers(value int) int {
	if value < 1 {
		return 1
	}
	if value > 8 {
		return 8
	}
	return value
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func fileExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func cudaStatus() string {
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		return "nvidia-smi available"
	}
	if _, err := os.Stat("/usr/local/cuda"); err == nil {
		return "cuda directory available"
	}
	return "not detected"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
