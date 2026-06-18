package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate_WritesBootNotification(t *testing.T) {
	tmp := t.TempDir()
	cfg := genConfig{
		version:     "v16",
		profileYAML: "internal/codegen/profiles/v16.yaml",
		schemaDir:   "schemas/v16",
		outRoot:     tmp,
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot() error = %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("Chdir(repoRoot) error = %v", err)
	}

	if err := generate(cfg); err != nil {
		t.Fatalf("generate() error = %v", err)
	}

	msgFile := filepath.Join(tmp, "v16", "messages", "boot_notification.go")
	b, err := os.ReadFile(msgFile)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", msgFile, err)
	}
	assertFileContains(t, string(b), "type BootNotificationRequest struct")
	assertFileContains(t, string(b), "type BootNotificationResponse struct")

	enumFile := filepath.Join(tmp, "v16", "messages", "enums.go")
	eb, err := os.ReadFile(enumFile)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", enumFile, err)
	}
	assertFileContains(t, string(eb), "type RegistrationStatus string")

	profFile := filepath.Join(tmp, "v16", "profiles", "core.go")
	pb, err := os.ReadFile(profFile)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", profFile, err)
	}
	assertFileContains(t, string(pb), "var BootNotification = ocppj.Message[")
}

func TestGenerate_WritesSendMessage(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot() error = %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("Chdir(repoRoot) error = %v", err)
	}

	tmp := t.TempDir()

	// Write a minimal profile YAML with a single SEND message.
	profileYAML := filepath.Join(tmp, "v21.yaml")
	profileContent := `version: v21
profiles:
  Monitoring:
    messages:
      - { name: NotifyPeriodicEventStream, send: NotifyPeriodicEventStream.json, dir: SentByCP }
`
	if err := os.WriteFile(profileYAML, []byte(profileContent), 0o600); err != nil {
		t.Fatalf("WriteFile profile: %v", err)
	}

	cfg := genConfig{
		version:     "v21",
		profileYAML: profileYAML,
		schemaDir:   "schemas/v21",
		outRoot:     tmp,
	}

	if err := generate(cfg); err != nil {
		t.Fatalf("generate() error = %v", err)
	}

	msgFile := filepath.Join(tmp, "v21", "messages", "notify_periodic_event_stream.go")
	b, err := os.ReadFile(msgFile)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", msgFile, err)
	}
	assertFileContains(t, string(b), "type NotifyPeriodicEventStream struct")
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

func assertFileContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Fatalf("file does not contain %q:\n%s", substr, s)
	}
}
