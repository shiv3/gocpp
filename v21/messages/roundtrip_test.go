package messages_test

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	v21schemas "github.com/shiv3/gocpp/v21/schemas"
	"github.com/stretchr/testify/require"
)

func TestAllSchemas_Compile(t *testing.T) {
	entries, err := v21schemas.FS.ReadDir(".")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		if e.IsDir() || e.Name() == "embed.go" {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			_, err := schema.New(v21schemas.FS, e.Name())
			require.NoError(t, err, "schema %s must compile", e.Name())
		})
	}
}
