package messages_test

import (
	"encoding/json"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v16/messages"
	v16schemas "github.com/shiv3/gocpp/v16/schemas"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestBootNotificationRequest_SchemaConformance(t *testing.T) {
	v, err := schema.New(v16schemas.FS, "BootNotification.json")
	require.NoError(t, err)

	rapid.Check(t, func(t *rapid.T) {
		req := messages.BootNotificationRequest{
			ChargePointVendor: rapid.StringN(1, 20, 20).Draw(t, "vendor"),
			ChargePointModel:  rapid.StringN(1, 20, 20).Draw(t, "model"),
		}
		b, err := json.Marshal(req)
		require.NoError(t, err)
		require.NoError(t, v.Validate(b))
	})
}

func TestAllSchemas_Compile(t *testing.T) {
	entries, err := v16schemas.FS.ReadDir(".")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		if e.IsDir() || e.Name() == "embed.go" {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			_, err := schema.New(v16schemas.FS, e.Name())
			require.NoError(t, err, "schema %s must compile", e.Name())
		})
	}
}
