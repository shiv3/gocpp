package cp

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v16"
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

func TestWithLenientSchemaWiresClosure(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	client := NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithSchemaRegistry(reg),
		WithLenientSchema(),
	)
	require.Equal(t, dispatcher.SchemaModeLenient, client.cfg.dispatcher.SchemaMode)
	require.NotNil(t, client.cfg.dispatcher.SchemaValidateLenient)
	require.Nil(t, client.cfg.dispatcher.SchemaValidate)

	out, soft, err := client.cfg.dispatcher.SchemaValidateLenient("1.6", "BootNotification", "request",
		[]byte(`{"chargePointVendor":"v","chargePointModel":"m","extra":1}`))
	require.NoError(t, err)
	require.Contains(t, soft, "additionalProperties")
	require.NotNil(t, out)
}

func TestWebSocketPingOptionsWireDispatcher(t *testing.T) {
	client := NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithWebSocketPingInterval(15*time.Second),
		WithWebSocketPongWait(3*time.Second),
	)

	require.Equal(t, 15*time.Second, client.cfg.dispatcher.PingInterval)
	require.Equal(t, 3*time.Second, client.cfg.dispatcher.PongWait)
}

func TestWithSerializedCalls(t *testing.T) {
	client := NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithSerializedCalls(),
	)

	require.True(t, client.cfg.dispatcher.SerializeOutboundCalls)
}

func TestClientConfigDialOptions(t *testing.T) {
	tlsCfg := &tls.Config{ServerName: "csms.example"}
	cfg := defaultClientConfig()
	WithSubProtocols("ocpp2.0.1", "ocpp1.6").apply(&cfg)
	WithHTTPHeader("X-Trace-ID", "trace-1").apply(&cfg)
	WithHTTPHeader("X-Trace-ID", "trace-2").apply(&cfg)
	WithHTTPHeader("X-Client", "cp").apply(&cfg)
	WithBasicAuth("alice", "secret").apply(&cfg)
	WithTLSConfig(tlsCfg).apply(&cfg)

	opts := cfg.dialOptions()

	require.Equal(t, []string{"ocpp2.0.1", "ocpp1.6"}, opts.Subprotocols)
	require.Equal(t, []string{"trace-1", "trace-2"}, opts.HTTPHeader.Values("X-Trace-ID"))
	require.Equal(t, "cp", opts.HTTPHeader.Get("X-Client"))
	require.Equal(t,
		"Basic "+base64.StdEncoding.EncodeToString([]byte("alice:secret")),
		opts.HTTPHeader.Get("Authorization"),
	)
	require.NotNil(t, opts.HTTPClient)
	require.Same(t, tlsCfg, opts.HTTPClient.Transport.(*http.Transport).TLSClientConfig)
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

func TestRuntimeSafetyOptions(t *testing.T) {
	def := NewClient("CP_1", "ws://example.invalid")
	require.True(t, def.cfg.dispatcher.SerializeOutboundCalls, "serialized by default")
	require.Equal(t, 60*time.Second, def.cfg.dispatcher.HandlerTimeout)
	require.EqualValues(t, 1<<20, def.cfg.dispatcher.MaxMessageSize)

	c := NewClient("CP_1", "ws://example.invalid",
		WithConcurrentCalls(), WithHandlerTimeout(0), WithMaxMessageSize(4096))
	require.False(t, c.cfg.dispatcher.SerializeOutboundCalls)
	require.Zero(t, c.cfg.dispatcher.HandlerTimeout)
	require.EqualValues(t, 4096, c.cfg.dispatcher.MaxMessageSize)

	c = NewClient("CP_1", "ws://example.invalid", WithConcurrentCalls(), WithSerializedCalls(), WithMaxMessageSize(-1))
	require.True(t, c.cfg.dispatcher.SerializeOutboundCalls)
	require.EqualValues(t, 1<<20, c.cfg.dispatcher.MaxMessageSize)
}

func TestWithUnlimitedMessageSize(t *testing.T) {
	c := NewClient("CP_1", "ws://example.invalid", WithUnlimitedMessageSize())
	require.Negative(t, c.cfg.dispatcher.MaxMessageSize)
}

func TestConnDoneOutsideHandler(t *testing.T) {
	require.Nil(t, ConnDone(context.Background()))
}

func TestQueueSizeOptions(t *testing.T) {
	c := NewClient("CP_1", "ws://example.invalid/ocpp/CP_1", WithInboundQueueSize(9), WithOutboundQueueSize(7))
	require.Equal(t, 9, c.cfg.dispatcher.InboundQueueSize)
	require.Equal(t, 7, c.cfg.dispatcher.OutboundQueueSize)
	def := NewClient("CP_1", "ws://example.invalid/ocpp/CP_1", WithInboundQueueSize(0), WithOutboundQueueSize(-1))
	require.Equal(t, 4, def.cfg.dispatcher.InboundQueueSize)
	require.Equal(t, 4, def.cfg.dispatcher.OutboundQueueSize)
}
