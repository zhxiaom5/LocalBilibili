package server

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LearningCredentialMigration struct {
	Codex       string `json:"codex"`
	WiseMapping string `json:"wisemapping"`
	CompletedAt string `json:"completed_at"`
}

func learningMigrationMarkerPath() string {
	if value := strings.TrimSpace(os.Getenv("LOCALBILIBILI_LEARNING_MIGRATION_MARKER")); value != "" {
		return value
	}
	return filepath.Join(filepath.Dir(learningSecretPath()), "localdouyin-import-v1.json")
}

func learningCredentialMigrationStatus() *LearningCredentialMigration {
	data, err := os.ReadFile(learningMigrationMarkerPath())
	if err != nil {
		return nil
	}
	var status LearningCredentialMigration
	if json.Unmarshal(data, &status) != nil {
		return nil
	}
	return &status
}

// migrateLearningCredentialsOnce imports only authentication material. It does
// not retain a link to LocalDouyin, and the marker prevents later re-imports.
func migrateLearningCredentialsOnce(configPath string, cfg *AppConfig) error {
	if learningCredentialMigrationStatus() != nil {
		return nil
	}
	status := LearningCredentialMigration{
		Codex:       migrateCodexCredentials(),
		WiseMapping: migrateWiseMappingCredentials(cfg),
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if status.WiseMapping == "copied" && configPath != "" {
		if err := SaveConfig(configPath, *cfg); err != nil {
			return err
		}
	}
	return writeLearningMigrationMarker(status)
}

func migrateCodexCredentials() string {
	target := strings.TrimSpace(os.Getenv("LOCALBILIBILI_CODEX_HOME"))
	source := strings.TrimSpace(os.Getenv("LOCALBILIBILI_LEARNING_SOURCE_CODEX_HOME"))
	if target == "" {
		return "invalid"
	}
	if directoryHasEntries(target) {
		return "already_local"
	}
	if !directoryHasEntries(source) {
		return "source_missing"
	}
	if err := atomicCopyPrivateDirectory(source, target); err != nil {
		return "invalid"
	}
	return "copied"
}

func migrateWiseMappingCredentials(cfg *AppConfig) string {
	if strings.TrimSpace(cfg.Learning.WiseMappingURL) != "" ||
		strings.TrimSpace(cfg.Learning.WiseMappingUser) != "" ||
		learningPasswordConfigured() {
		return "already_local"
	}
	sourceConfig := strings.TrimSpace(os.Getenv("LOCALBILIBILI_LEARNING_SOURCE_CONFIG"))
	sourceSecret := strings.TrimSpace(os.Getenv("LOCALBILIBILI_LEARNING_SOURCE_WISEMAPPING_SECRET_FILE"))
	if sourceConfig == "" || sourceSecret == "" {
		return "source_missing"
	}
	data, err := os.ReadFile(sourceConfig)
	if err != nil {
		return "source_missing"
	}
	var source AppConfig
	if json.Unmarshal(data, &source) != nil {
		return "invalid"
	}
	password, err := os.ReadFile(sourceSecret)
	if err != nil {
		return "source_missing"
	}
	if strings.TrimSpace(source.Learning.WiseMappingURL) == "" ||
		strings.TrimSpace(source.Learning.WiseMappingUser) == "" ||
		strings.TrimSpace(string(password)) == "" {
		return "invalid"
	}
	if err := saveLearningPassword(string(password)); err != nil {
		return "invalid"
	}
	cfg.Learning.WiseMappingURL = source.Learning.WiseMappingURL
	cfg.Learning.WiseMappingUser = source.Learning.WiseMappingUser
	cfg.Learning = normalizeLearningRuntimeConfig(cfg.Learning)
	return "copied"
}

func writeLearningMigrationMarker(status LearningCredentialMigration) error {
	path := learningMigrationMarkerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".migration-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func directoryHasEntries(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}

func atomicCopyPrivateDirectory(source, target string) error {
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	if source == "." || target == "." || source == target {
		return errors.New("invalid credential directory")
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".codex-import-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := copyPrivateTree(source, tmp); err != nil {
		return err
	}
	if info, err := os.Stat(target); err == nil {
		if !info.IsDir() {
			return errors.New("credential target is not a directory")
		}
		if !directoryHasEntries(target) {
			if err := os.Remove(target); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		return err
	}
	return os.Chmod(target, 0o700)
}

func copyPrivateTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		destination := filepath.Join(target, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("credential directory contains symlink")
		}
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		if !info.Mode().IsRegular() {
			return errors.New("credential directory contains unsupported file")
		}
		return copyPrivateFile(path, destination)
	})
}

func copyPrivateFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
