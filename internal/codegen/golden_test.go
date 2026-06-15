package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var update = false // set true locally to regenerate goldens: go test -run Golden -args -update via flag below

func TestCodegen_GoldenV16(t *testing.T) {
	root, err := findRepoRoot()
	require.NoError(t, err)
	wd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(wd))
	})
	require.NoError(t, os.Chdir(root))

	tmp := t.TempDir()
	cfg := genConfig{version: "v16", profileYAML: "internal/codegen/profiles/v16.yaml", schemaDir: "schemas/v16", outRoot: tmp}
	require.NoError(t, generate(cfg))

	// Compare a stable subset (boot_notification.go) against the committed v16 output.
	got, err := os.ReadFile(filepath.Join(tmp, "v16", "messages", "boot_notification.go"))
	require.NoError(t, err)
	want, err := os.ReadFile(filepath.Join(root, "v16", "messages", "boot_notification.go"))
	require.NoError(t, err)
	require.Equal(t, string(want), string(got), "generated boot_notification.go drifted; run make codegen")
}
