package cp

import (
	"testing"
	"testing/fstest"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/stretchr/testify/require"
)

func TestSchemaOptions_LastWinsAndWireDispatcher(t *testing.T) {
	reg := testSchemaRegistry(t, "ChangeConfiguration")

	client := NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithSchemaRegistry(reg),
		WithStrictSchema(true),
		WithTolerantSchema(),
	)
	require.Equal(t, dispatcher.SchemaModeTolerant, client.cfg.dispatcher.SchemaMode)
	require.NotNil(t, client.cfg.dispatcher.SchemaValidate)
	require.Error(t, client.cfg.dispatcher.SchemaValidate("1.6", "ChangeConfiguration", "request", []byte(`{}`)))

	client = NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithSchemaRegistry(reg),
		WithTolerantSchema(),
		WithStrictSchema(true),
	)
	require.Equal(t, dispatcher.SchemaModeStrict, client.cfg.dispatcher.SchemaMode)
	require.NotNil(t, client.cfg.dispatcher.SchemaValidate)

	client = NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithSchemaRegistry(reg),
		WithTolerantSchema(),
		WithStrictSchema(false),
	)
	require.Equal(t, dispatcher.SchemaModeOff, client.cfg.dispatcher.SchemaMode)
	require.Nil(t, client.cfg.dispatcher.SchemaValidate)
}

func testSchemaRegistry(t *testing.T, action string) *schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	err := reg.Register("1.6", action, "request", fstest.MapFS{
		"request.json": {
			Data: []byte(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
		},
	}, "request.json")
	require.NoError(t, err)
	return reg
}
