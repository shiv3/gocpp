package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate_OneFilePerMessage(t *testing.T) {
	tmp := t.TempDir()
	root, err := findRepoRoot()
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
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir(%s) error = %v", root, err)
	}

	cfg := genConfig{
		version:     "v16",
		profileYAML: "internal/codegen/profiles/v16.yaml",
		schemaDir:   "schemas/v16",
		outRoot:     tmp,
	}
	if err := generate(cfg); err != nil {
		t.Fatalf("generate() error = %v", err)
	}

	bootFile := filepath.Join(tmp, "v16", "messages", "boot_notification.go")
	b, err := os.ReadFile(bootFile)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", bootFile, err)
	}
	assertFileContains(t, string(b), "type BootNotificationRequest struct")
	assertFileNotContains(t, string(b), "type AuthorizeRequest struct")

	for _, path := range []string{
		filepath.Join(tmp, "v16", "messages", "enums.go"),
		filepath.Join(tmp, "v16", "schemas", "BootNotification.json"),
		filepath.Join(tmp, "v16", "schemas", "BootNotificationResponse.json"),
		filepath.Join(tmp, "v16", "schemas", "embed.go"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat(%s) error = %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(tmp, "v16", "messages", "messages.go")); !os.IsNotExist(err) {
		t.Fatalf("messages.go exists or stat failed unexpectedly: %v", err)
	}
}

func assertFileNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Fatalf("file unexpectedly contains %q:\n%s", substr, s)
	}
}
