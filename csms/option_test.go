package csms

import (
	"log/slog"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/stretchr/testify/require"
)

func TestOptions_Apply(t *testing.T) {
	cfg := defaultServerConfig()
	WithCallTimeout(5 * time.Second).apply(&cfg)
	WithSubProtocols("ocpp1.6", "ocpp2.0.1").apply(&cfg)
	WithLogger(slog.Default()).apply(&cfg)

	require.Equal(t, 5*time.Second, cfg.dispatcher.CallTimeout)
	require.Equal(t, []string{"ocpp1.6", "ocpp2.0.1"}, cfg.subProtocols)
	require.NotNil(t, cfg.dispatcher.Logger)
}

func TestSchemaOptions_LastWinsAndWireDispatcher(t *testing.T) {
	reg := testSchemaRegistry(t, "BootNotification")

	srv := NewServer(
		WithSchemaRegistry(reg),
		WithStrictSchema(true),
		WithTolerantSchema(),
	)
	require.Equal(t, dispatcher.SchemaModeTolerant, srv.cfg.dispatcher.SchemaMode)
	require.NotNil(t, srv.cfg.dispatcher.SchemaValidate)
	require.Error(t, srv.cfg.dispatcher.SchemaValidate("1.6", "BootNotification", "request", []byte(`{}`)))

	srv = NewServer(
		WithSchemaRegistry(reg),
		WithTolerantSchema(),
		WithStrictSchema(true),
	)
	require.Equal(t, dispatcher.SchemaModeStrict, srv.cfg.dispatcher.SchemaMode)
	require.NotNil(t, srv.cfg.dispatcher.SchemaValidate)

	srv = NewServer(
		WithSchemaRegistry(reg),
		WithTolerantSchema(),
		WithStrictSchema(false),
	)
	require.Equal(t, dispatcher.SchemaModeOff, srv.cfg.dispatcher.SchemaMode)
	require.Nil(t, srv.cfg.dispatcher.SchemaValidate)
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
