package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLearningCredentialsCopyOnceThenStayIndependent(t *testing.T) {
	root := t.TempDir()
	sourceCodex := filepath.Join(root, "localdouyin-codex")
	targetCodex := filepath.Join(root, "localbilibili-codex")
	sourceSecret := filepath.Join(root, "localdouyin-learning", "wisemapping.secret")
	targetSecretDir := filepath.Join(root, "localbilibili-learning")
	sourceConfig := filepath.Join(root, "localdouyin.config.json")
	targetConfig := filepath.Join(root, "localbilibili.config.json")
	if err := os.MkdirAll(sourceCodex, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceCodex, "auth.json"), []byte(`{"token":"source"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(sourceSecret), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceSecret, []byte("wise-source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := AppConfig{Learning: LearningRuntimeConfig{WiseMappingURL: "http://wise", WiseMappingUser: "user@example.com"}}
	data, _ := json.Marshal(source)
	if err := os.WriteFile(sourceConfig, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALBILIBILI_CODEX_HOME", targetCodex)
	t.Setenv("LOCALBILIBILI_LEARNING_SECRET_DIR", targetSecretDir)
	t.Setenv("LOCALBILIBILI_LEARNING_MIGRATION_MARKER", filepath.Join(targetSecretDir, "migration.json"))
	t.Setenv("LOCALBILIBILI_LEARNING_SOURCE_CODEX_HOME", sourceCodex)
	t.Setenv("LOCALBILIBILI_LEARNING_SOURCE_CONFIG", sourceConfig)
	t.Setenv("LOCALBILIBILI_LEARNING_SOURCE_WISEMAPPING_SECRET_FILE", sourceSecret)

	cfg := AppConfig{DatabaseDir: root, Learning: DefaultLearningRuntimeConfig()}
	if err := migrateLearningCredentialsOnce(targetConfig, &cfg); err != nil {
		t.Fatal(err)
	}
	status := learningCredentialMigrationStatus()
	if status == nil || status.Codex != "copied" || status.WiseMapping != "copied" {
		t.Fatalf("migration status = %#v", status)
	}
	if cfg.Learning.WiseMappingURL != "http://wise" || cfg.Learning.WiseMappingUser != "user@example.com" {
		t.Fatalf("learning config = %#v", cfg.Learning)
	}
	if password, err := os.ReadFile(filepath.Join(targetSecretDir, "wisemapping.secret")); err != nil || string(password) != "wise-source\n" {
		t.Fatalf("copied password = %q, %v", password, err)
	}
	if err := os.WriteFile(filepath.Join(targetCodex, "auth.json"), []byte(`{"token":"local"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceCodex, "auth.json"), []byte(`{"token":"changed-source"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateLearningCredentialsOnce(targetConfig, &cfg); err != nil {
		t.Fatal(err)
	}
	auth, err := os.ReadFile(filepath.Join(targetCodex, "auth.json"))
	if err != nil || string(auth) != `{"token":"local"}` {
		t.Fatalf("second startup changed local auth: %q, %v", auth, err)
	}
}

func TestLearningCredentialsDoNotOverwriteLocalOrRetryMissingSource(t *testing.T) {
	root := t.TempDir()
	targetCodex := filepath.Join(root, "codex")
	targetSecretDir := filepath.Join(root, "learning")
	if err := os.MkdirAll(targetCodex, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetCodex, "auth.json"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALBILIBILI_CODEX_HOME", targetCodex)
	t.Setenv("LOCALBILIBILI_LEARNING_SECRET_DIR", targetSecretDir)
	t.Setenv("LOCALBILIBILI_LEARNING_MIGRATION_MARKER", filepath.Join(targetSecretDir, "migration.json"))
	t.Setenv("LOCALBILIBILI_LEARNING_SOURCE_CODEX_HOME", filepath.Join(root, "missing"))
	t.Setenv("LOCALBILIBILI_LEARNING_SOURCE_CONFIG", filepath.Join(root, "missing.json"))
	t.Setenv("LOCALBILIBILI_LEARNING_SOURCE_WISEMAPPING_SECRET_FILE", filepath.Join(root, "missing.secret"))
	cfg := AppConfig{DatabaseDir: root, Learning: DefaultLearningRuntimeConfig()}
	if err := migrateLearningCredentialsOnce(filepath.Join(root, "config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	status := learningCredentialMigrationStatus()
	if status == nil || status.Codex != "already_local" || status.WiseMapping != "source_missing" {
		t.Fatalf("migration status = %#v", status)
	}
}
