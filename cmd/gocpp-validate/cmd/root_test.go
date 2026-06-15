package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidate_ValidAndInvalid(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.json")
	require.NoError(t, os.WriteFile(valid, []byte(`{"chargePointVendor":"X","chargePointModel":"Y"}`), 0o644))
	invalid := filepath.Join(dir, "invalid.json")
	require.NoError(t, os.WriteFile(invalid, []byte(`{"chargePointVendor":"X"}`), 0o644))

	var out bytes.Buffer
	err := RunValidate(&out, "1.6", "BootNotification", "request", valid)
	require.NoError(t, err)
	require.Contains(t, out.String(), "valid")

	out.Reset()
	err = RunValidate(&out, "1.6", "BootNotification", "request", invalid)
	require.Error(t, err)
}
