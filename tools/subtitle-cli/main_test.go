package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubtitleCLIDefaultScanIsSilent(t *testing.T) {
	root, cfg := setupExistingSubtitleScan(t)
	var logs bytes.Buffer
	cfg.LogLevel = "silent"
	cfg.LogWriter = &logs
	cfg.finalize()

	videos, err := collectVideos(root)
	if err != nil {
		t.Fatalf("collect videos: %v", err)
	}
	if err := runVideoWorkers(t.Context(), cfg, videos, 1); err != nil {
		t.Fatalf("run workers: %v", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("default silent scan wrote logs: %q", logs.String())
	}
}

func TestSubtitleCLIInfoLogLevelReportsScanAndSkips(t *testing.T) {
	root, cfg := setupExistingSubtitleScan(t)
	var logs bytes.Buffer
	cfg.LogLevel = "info"
	cfg.LogWriter = &logs
	cfg.finalize()

	videos, err := collectVideosWithLog(root, cfg)
	if err != nil {
		t.Fatalf("collect videos: %v", err)
	}
	if err := runVideoWorkers(t.Context(), cfg, videos, 1); err != nil {
		t.Fatalf("run workers: %v", err)
	}
	output := logs.String()
	for _, want := range []string{
		"[info] collecting videos:",
		"[info] scan creator: creator",
		"[info] creator videos: creator count=1",
		"[info] collect finished: videos=1",
		"[info] scan started: videos=1 workers=1",
		"[info] skip existing:",
		"[info] scan finished: videos=1",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing log %q in %q", want, output)
		}
	}
}

func TestSubtitleCLIInvalidLogLevel(t *testing.T) {
	cfg := defaultConfig()
	cfg.LogLevel = "loud"
	cfg.finalize()
	if err := cfg.validate(); err == nil {
		t.Fatal("expected invalid log level error")
	}
}

func TestSubtitleCLISkipsVideoWithoutAudio(t *testing.T) {
	root := t.TempDir()
	video := filepath.Join(root, "work.mp4")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatalf("write video: %v", err)
	}
	ffmpeg := filepath.Join(root, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\necho 'Output file does not contain any stream' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write ffmpeg: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	model := filepath.Join(root, "model.bin")
	if err := os.WriteFile(model, []byte("model"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	var logs bytes.Buffer
	cfg := config{
		WhisperCLI: exe,
		FFmpeg:     ffmpeg,
		Model:      model,
		Language:   "zh",
		GPU:        true,
		LogLevel:   "info",
		LogWriter:  &logs,
	}
	cfg.finalize()

	if err := transcribeVideo(t.Context(), cfg, video); err != nil {
		t.Fatalf("transcribe no-audio video: %v", err)
	}
	if !strings.Contains(logs.String(), "[info] skip no audio:") {
		t.Fatalf("missing no-audio skip log in %q", logs.String())
	}
}

func setupExistingSubtitleScan(t *testing.T) (string, config) {
	t.Helper()
	root := t.TempDir()
	creatorDir := filepath.Join(root, "creator")
	if err := os.Mkdir(creatorDir, 0o755); err != nil {
		t.Fatalf("mkdir creator: %v", err)
	}
	video := filepath.Join(creatorDir, "work.mp4")
	for _, path := range []string{
		video,
		filepath.Join(creatorDir, "work.txt"),
		filepath.Join(creatorDir, "work.srt"),
		filepath.Join(creatorDir, "work.json"),
	} {
		if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	model := filepath.Join(root, "model.bin")
	if err := os.WriteFile(model, []byte("model"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	cfg := config{
		WhisperCLI: exe,
		FFmpeg:     exe,
		Model:      model,
		Language:   "zh",
		GPU:        true,
		LogLevel:   "silent",
	}
	return root, cfg
}
