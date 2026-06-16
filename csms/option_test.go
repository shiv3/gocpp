package csms

import (
	"log/slog"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v16"
	"github.com/stretchr/testify/require"
)

func TestOptions_Apply(t *testing.T) {
	cfg := defaultServerConfig()
	WithCallTimeout(5 * time.Second).apply(&cfg)
	WithSubProtocols("ocpp1.6", "ocpp2.0.1").apply(&cfg)
	WithLogger(slog.Default()).apply(&cfg)
	WithDuplicatePolicy(DuplicatePolicyRejectNew).apply(&cfg)
	WithCPIDExtractor(func(r *http.Request) (string, bool) { return "CP_1", true }).apply(&cfg)

	require.Equal(t, 5*time.Second, cfg.dispatcher.CallTimeout)
	require.Equal(t, []string{"ocpp1.6", "ocpp2.0.1"}, cfg.subProtocols)
	require.NotNil(t, cfg.dispatcher.Logger)
	require.Equal(t, DuplicatePolicyRejectNew, cfg.duplicatePolicy)
	require.NotNil(t, cfg.cpIDExtractor)
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

func TestWithLenientSchemaWiresClosure(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	srv := NewServer(WithSchemaRegistry(reg), WithLenientSchema())
	require.Equal(t, dispatcher.SchemaModeLenient, srv.cfg.dispatcher.SchemaMode)
	require.NotNil(t, srv.cfg.dispatcher.SchemaValidateLenient)
	require.Nil(t, srv.cfg.dispatcher.SchemaValidate)

	out, soft, err := srv.cfg.dispatcher.SchemaValidateLenient("1.6", "BootNotification", "request",
		[]byte(`{"chargePointVendor":"v","chargePointModel":"m","extra":1}`))
	require.NoError(t, err)
	require.Contains(t, soft, "additionalProperties")
	require.NotNil(t, out)
}

func TestWithGlobalConcurrencyLimit(t *testing.T) {
	// Disabled by default: server-wide cap is opt-in.
	srv := NewServer()
	require.Nil(t, srv.cfg.dispatcher.GlobalHandlerLimiter)

	// A positive limit installs a shared limiter on the dispatcher config.
	srv = NewServer(WithGlobalConcurrencyLimit(8))
	require.NotNil(t, srv.cfg.dispatcher.GlobalHandlerLimiter)

	// Non-positive values are treated as "disabled".
	srv = NewServer(WithGlobalConcurrencyLimit(0))
	require.Nil(t, srv.cfg.dispatcher.GlobalHandlerLimiter)
}

func TestWebSocketPingOptionsWireDispatcher(t *testing.T) {
	srv := NewServer(
		WithWebSocketPingInterval(15*time.Second),
		WithWebSocketPongWait(3*time.Second),
	)

	require.Equal(t, 15*time.Second, srv.cfg.dispatcher.PingInterval)
	require.Equal(t, 3*time.Second, srv.cfg.dispatcher.PongWait)
}

func TestWithSerializedCalls(t *testing.T) {
	srv := NewServer(WithSerializedCalls())

	require.True(t, srv.cfg.dispatcher.SerializeOutboundCalls)
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
